package gofastemail

import (
	"bytes"
	"context"
	"encoding/binary"
	"fmt"
	"io"
	"os"

	"github.com/AOShei/go-fast-core"
	"github.com/AOShei/go-fast-email/internal/tnef"
)

// Defaults for the limits of gofastcore.Options.Limits, used where a limit
// is zero. A negative limit means no limit.
const (
	// DefaultMaxInputBytes bounds the size of the file. A mailbox is read
	// one message at a time, so the bound is high.
	DefaultMaxInputBytes = 16 << 30
	// DefaultMaxDecodedBytes bounds the bytes produced by decoding base64
	// and quoted-printable, over all the messages and parts of a file and
	// the documents embedded in it.
	DefaultMaxDecodedBytes = 2 << 30
)

// Defaults for the settings of Extractor.
const (
	// DefaultMaxMessageBytes bounds the bytes of one message held in
	// memory at a time: an .eml file, or one message of a mailbox.
	DefaultMaxMessageBytes = 256 << 20
	// DefaultMaxHeaderBytes bounds the header section of a message or of
	// a MIME part.
	DefaultMaxHeaderBytes = 1 << 20
	// DefaultMaxMIMEDepth bounds how deeply multipart bodies nest.
	DefaultMaxMIMEDepth = 32
	// DefaultMaxMIMEParts bounds the MIME parts read from one message.
	DefaultMaxMIMEParts = 10_000
)

// Warning codes specific to this library. Each states a fact about the
// message. The codes shared by all go-fast libraries are in gofastcore; this
// library raises these of them besides the ones about limits and embedded
// documents:
//
//	text.unmapped_glyphs  text in a character set this library does not
//	                      decode, or bytes not valid in the one declared;
//	                      the message names the character set and the
//	                      bytes appear as U+FFFD
//	input.repaired        a header section ended by a line that is not a
//	                      header, a multipart body with no closing
//	                      boundary or no boundary at all, base64 with
//	                      characters outside its alphabet, a compressed
//	                      RTF body that cannot be read, a damaged TNEF
//	                      stream (cut short, a wrong checksum, an
//	                      unreadable property list), an Outlook compound
//	                      file whose short streams cannot be read
//	                      (severity error: the properties in them are
//	                      missing)
//	input.external_reference
//	                      an Outlook attachment by reference names a file
//	                      outside the message, or a message/external-body
//	                      part names content elsewhere; nothing is fetched
//	part.no_text          a message with no text at all
const (
	// WarnAlternatives: the message has alternative bodies
	// (multipart/alternative). The blocks are the text/plain alternative;
	// every other alternative, which a mail program may show instead and
	// which can say something else, is extracted as an embedded document.
	WarnAlternatives = "email.alternatives"
	// WarnUnknownTransferEncoding: a part declares a transfer encoding
	// other than base64, quoted-printable, 7bit, 8bit or binary. Its bytes
	// are taken as they are.
	WarnUnknownTransferEncoding = "email.unknown_transfer_encoding"
)

// Extractor extracts email: a single message (.eml), a mailbox (.mbox), an
// Outlook message (.msg) and a TNEF stream (winmail.dat); and calendars
// (.ics) and contact lists (.vcf). The zero value
// is ready to use and safe for concurrent use. Its fields are the settings
// only this library has; everything else is in gofastcore.Options. For
// each, zero means the default and a negative value no limit.
type Extractor struct {
	// MaxMessageBytes bounds the bytes of one message held in memory: an
	// .eml file, or one message of a mailbox. A longer message is read to
	// that length, with a limit.exceeded warning on its part.
	MaxMessageBytes int64
	// MaxHeaderBytes bounds the header section of a message or MIME part.
	MaxHeaderBytes int
	// MaxMIMEDepth bounds how deeply multipart bodies nest.
	MaxMIMEDepth int
	// MaxMIMEParts bounds the MIME parts read from one message.
	MaxMIMEParts int
	// Charset is the character set of text that declares none (a body
	// with no charset parameter, header text outside encoded words, an
	// Outlook string with no code page) and is not valid UTF-8, given as a
	// MIME charset label ("shift_jis", "windows-1251"). It is used instead
	// of the set recognised from the bytes. Empty means none: such text is
	// read in the set its bytes are certainly in, with the warning
	// text.encoding_detected, and where none is certain its bytes outside
	// ASCII are U+FFFD (text.unmapped_glyphs). Text labelled US-ASCII that
	// is not ASCII counts as declaring none.
	Charset string
}

var _ gofastcore.Extractor = (*Extractor)(nil)

// Formats lists the formats this library extracts, as they appear in
// gofastcore.Source.Format.
func (e *Extractor) Formats() []string { return []string{"eml", "mbox", "msg", "tnef", "ics", "vcf"} }

// Extract reads an email file with default settings. It is
// (&Extractor{}).Extract.
func Extract(ctx context.Context, r io.ReaderAt, size int64, opts *gofastcore.Options) (*gofastcore.Document, error) {
	return (&Extractor{}).Extract(ctx, r, size, opts)
}

// ExtractFile opens the named file and extracts it with default settings.
func ExtractFile(ctx context.Context, path string, opts *gofastcore.Options) (*gofastcore.Document, error) {
	return (&Extractor{}).ExtractFile(ctx, path, opts)
}

// ExtractFile opens the named file and extracts it. The file's base name
// is recorded as the source name unless opts.Name is set.
func (e *Extractor) ExtractFile(ctx context.Context, path string, opts *gofastcore.Options) (*gofastcore.Document, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return nil, err
	}
	o := gofastcore.Options{}
	if opts != nil {
		o = *opts
	}
	if o.Name == "" {
		o.Name = info.Name()
	}
	return e.Extract(ctx, f, info.Size(), &o)
}

var cfbSignature = []byte{0xD0, 0xCF, 0x11, 0xE0, 0xA1, 0xB1, 0x1A, 0xE1}

// lookVobj is how far into a file Extract looks for the BEGIN line of a
// calendar or contact list, past a byte order mark and white space.
const lookVobj = 4096

// Extract reads size bytes from r and returns the document.
//
// The format is decided from the bytes, never from a name: a file that
// begins, after a byte order mark and white space, with BEGIN:VCALENDAR or
// BEGIN: and a component a calendar holds (VEVENT, VTODO, ...) is a
// calendar (ics), and one with BEGIN:VCARD, perhaps after a group, a
// contact list (vcf); a
// compound file is an Outlook message, a file that begins with an mbox
// "From " line
// is a mailbox, a file that begins with the TNEF signature is a
// winmail.dat, and a file that begins with a header field is a message.
// An error wraps one of the gofastcore errors:
//
//   - ErrMalformed: the input is none of these.
//   - ErrUnsupported: a compound file that is not an Outlook message.
//   - ErrLimitExceeded: the input is larger than Limits.MaxInputBytes.
//   - ErrNoPartsSelected: Options.Parts selects no message.
func (e *Extractor) Extract(ctx context.Context, r io.ReaderAt, size int64, opts *gofastcore.Options) (*gofastcore.Document, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if opts == nil {
		opts = &gofastcore.Options{}
	}
	if size < 0 {
		return nil, fmt.Errorf("gofastemail: negative size: %w", gofastcore.ErrMalformed)
	}
	if size > gofastcore.ResolveLimit(opts.Limits.MaxInputBytes, DefaultMaxInputBytes) {
		return nil, fmt.Errorf("gofastemail: input of %d bytes: %w", size, gofastcore.ErrLimitExceeded)
	}
	var head [lookVobj]byte
	n, _ := r.ReadAt(head[:], 0)
	ctx, text := gofastcore.WithTextBudget(ctx, opts.Limits)
	defer text.Close()
	ctx, dec := gofastcore.WithDecodeBudget(ctx, opts.Limits, DefaultMaxDecodedBytes)
	defer dec.Close()
	rd := &reader{e: e, ctx: ctx, opts: opts, text: text, dec: dec}
	if f := vobjFormat(head[:n]); f != "" {
		// Before the test for a message: a content line looks like a
		// header field.
		return rd.vobject(r, size, f)
	}
	switch {
	case bytes.HasPrefix(head[:n], cfbSignature):
		return rd.msgFile(r, size)
	case bytes.HasPrefix(head[:n], []byte("From ")):
		return rd.mbox(r, size)
	case n >= 4 && binary.LittleEndian.Uint32(head[:]) == tnef.Signature:
		return rd.tnefFile(r, size)
	}
	return rd.eml(r, size)
}

func (e *Extractor) maxMessage() int64 {
	return gofastcore.ResolveLimit(e.MaxMessageBytes, DefaultMaxMessageBytes)
}

func (e *Extractor) maxHeader() int {
	return int(min(gofastcore.ResolveLimit(int64(e.MaxHeaderBytes), DefaultMaxHeaderBytes), 1<<40))
}

func (e *Extractor) maxDepth() int {
	return int(min(gofastcore.ResolveLimit(int64(e.MaxMIMEDepth), DefaultMaxMIMEDepth), 1<<20))
}

func (e *Extractor) maxParts() int {
	return int(min(gofastcore.ResolveLimit(int64(e.MaxMIMEParts), DefaultMaxMIMEParts), 1<<40))
}
