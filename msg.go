package gofastemail

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/AOShei/go-fast-core"
	"github.com/AOShei/go-fast-email/internal/cfb"
	"github.com/AOShei/go-fast-email/internal/charset"
	"github.com/AOShei/go-fast-email/internal/clean"
	"github.com/AOShei/go-fast-email/internal/rfc822"
	"github.com/AOShei/go-fast-email/internal/rtfcp"
	"github.com/AOShei/go-fast-email/internal/textbody"
)

// An Outlook message ([MS-OXMSG]) is a compound file. The message's
// properties are in the stream "__properties_version1.0": fixed-length
// values there, variable-length ones each in a stream
// "__substg1.0_IIIITTTT" (property id and type in hexadecimal). Every
// recipient and every attachment is a storage with properties of its own;
// an attached message is a storage within its attachment's, laid out as
// a message is.

// Property types.
const (
	ptInt32   = 0x0003
	ptBool    = 0x000B
	ptObject  = 0x000D
	ptString8 = 0x001E
	ptUnicode = 0x001F
	ptTime    = 0x0040
	ptBinary  = 0x0102
)

// Property ids ([MS-OXPROPS]).
const (
	pidMessageClass       = 0x001A
	pidSubject            = 0x0037
	pidClientSubmitTime   = 0x0039
	pidSentReprName       = 0x0042
	pidSentReprEmail      = 0x0065
	pidTransportHeaders   = 0x007D
	pidSenderEmail        = 0x0C1F
	pidSenderName         = 0x0C1A
	pidRecipientType      = 0x0C15
	pidDisplayBcc         = 0x0E02
	pidDisplayCc          = 0x0E03
	pidDisplayTo          = 0x0E04
	pidDeliveryTime       = 0x0E06
	pidBody               = 0x1000
	pidRTFCompressed      = 0x1009
	pidHTML               = 0x1013
	pidInternetMessageID  = 0x1035
	pidDisplayName        = 0x3001
	pidEmailAddress       = 0x3003
	pidCreationTime       = 0x3007
	pidModificationTime   = 0x3008
	pidAttachData         = 0x3701
	pidAttachFilename     = 0x3704
	pidAttachMethod       = 0x3705
	pidAttachLongFilename = 0x3707
	pidAttachPathname     = 0x3708
	pidAttachLongPathname = 0x370D
	pidAttachMimeTag      = 0x370E
	pidSMTPAddress        = 0x39FE
	pidInternetCPID       = 0x3FDE
	pidMessageCodepage    = 0x3FFD
	pidMessageLocaleID    = 0x3FF1
	pidSenderSMTPAddress  = 0x5D01
	pidAttachmentHidden   = 0x7FFE
)

// Attachment methods (PidTagAttachMethod).
const (
	attachByValue      = 1
	attachByReference  = 2
	attachByRefResolve = 3
	attachByRefOnly    = 4
	attachEmbeddedMsg  = 5
	attachOLE          = 6
	attachByWebRef     = 7
)

// maxStorages bounds the recipients and the attachments read from one
// message.
const maxStorages = 10_000

// maxFixedEntries bounds the entries read from the property stream of one
// object (a message, a recipient, an attachment).
const maxFixedEntries = 65536

// props reads the properties of one object: a message, a recipient or an
// attachment.
type props struct {
	f      *cfb.File
	prefix []string
	// values holds the variable-length properties by tag when they come
	// from a TNEF stream, not from a compound file (f is nil).
	values map[uint32][]byte
	fixed  map[uint32]uint64
	label  string // the character set of PT_STRING8 values
	// alt, when label is UTF-8 taken from PR_INTERNET_CPID, is the ANSI
	// code page of the message's locale: a PT_STRING8 value that is not
	// valid UTF-8 is read in it.
	alt   string
	limit int64
	m     *message
	// serial tells this object's properties from another's of the same
	// message: the number of property readers made for it so far.
	serial int
	// r, for a compound file: every byte read from it is taken from the
	// decode budget, since a file can give many streams the same sectors
	// and so be read many times over.
	r *reader
}

func (r *reader) readProps(m *message, f *cfb.File, prefix []string, headerSize int) *props {
	m.props++
	p := &props{f: f, prefix: prefix, fixed: map[uint32]uint64{}, limit: r.e.maxMessage(), m: m, r: r, serial: m.props}
	s, err := f.Open(p.path("__properties_version1.0")...)
	if err != nil {
		return p
	}
	window := int64(headerSize) + 16*maxFixedEntries
	n := min(s.Size(), window)
	if !r.takeRead(m, n) {
		return p
	}
	if lost := (s.Size() - window) / 16; lost >= 1 { // whole entries only: a part of one is not read in any case
		m.warnCount(gofastcore.WarnLimitExceeded, "property entries", int(lost), func(n int) string {
			return fmt.Sprintf("property streams with more than %d entries: the fixed-length values (dates, numbers, flags) in the entries past the %dth of each are not read (%s in all)", maxFixedEntries, maxFixedEntries, things(n, "entry"))
		})
	}
	buf := make([]byte, n)
	got, _ := s.ReadAt(buf, 0)
	buf = buf[:got]
	le := binary.LittleEndian
	for at := headerSize; at+16 <= len(buf); at += 16 {
		tag := le.Uint32(buf[at:])
		switch tag & 0xFFFF {
		case ptInt32, ptBool, ptTime:
			p.fixed[tag] = le.Uint64(buf[at+8:])
		}
	}
	return p
}

func (p *props) path(name string) []string {
	return append(append([]string(nil), p.prefix...), name)
}

// read returns the content of the stream of a variable-length property,
// to at most the message limit.
func (p *props) read(id, typ uint16) ([]byte, bool) {
	if p.f == nil {
		v, ok := p.values[uint32(id)<<16|uint32(typ)]
		if int64(len(v)) > p.limit {
			p.long(id, typ, int64(len(v)))
			v = v[:p.limit]
		}
		return v, ok
	}
	s, err := p.f.Open(p.path(fmt.Sprintf("__substg1.0_%04X%04X", id, typ))...)
	if err != nil {
		return nil, false
	}
	n := s.Size()
	if n > p.limit {
		p.long(id, typ, n)
		n = p.limit
	}
	if p.r != nil && !p.r.takeRead(p.m, n) {
		return nil, false
	}
	buf := make([]byte, n)
	got, _ := s.ReadAt(buf, 0)
	return buf[:got], true
}

// long warns, once per part, of the properties longer than the message
// limit, which are read to that length.
func (p *props) long(id, typ uint16, size int64) {
	p.m.warnEach(gofastcore.SeverityError, gofastcore.WarnLimitExceeded, "long property", fmt.Sprintf("%d %04X%04X", p.serial, id, typ),
		fmt.Sprintf("%d bytes", size), fmt.Sprintf("a property of %d bytes read to %d", size, p.limit), true,
		func(count, list string) string {
			return fmt.Sprintf("%s properties longer than %d bytes are read to that length (%s)", count, p.limit, list)
		})
}

// takeRead takes n bytes read from a compound file from the decode
// budget, and warns once when it is spent.
func (r *reader) takeRead(m *message, n int64) bool {
	if m.undecode || !r.dec.Take(n) {
		if !m.undecode {
			m.undecode = true
			m.warn(gofastcore.SeverityError, gofastcore.WarnLimitExceeded, "decode limit reached; the rest of the message's parts are listed, not read")
		}
		return false
	}
	return true
}

// str returns a string property, Unicode or 8-bit.
func (p *props) str(id uint16) string {
	if b, ok := p.read(id, ptUnicode); ok {
		s, res := charset.DecodeUTF16(b, true)
		if res.Replaced {
			p.m.charsetReport()("utf-16le", res)
		}
		return strings.TrimRight(s, "\x00")
	}
	if b, ok := p.read(id, ptString8); ok {
		b = bytes.TrimRight(b, "\x00")
		label := p.label
		if p.alt != "" && !utf8.Valid(b) {
			label = p.alt
		}
		s, res := charset.Decode(label, b)
		if !res.Known || res.Replaced {
			p.m.charsetReport()(label, res)
		}
		return s
	}
	return ""
}

func (p *props) int32(id uint16) (int32, bool) {
	v, ok := p.fixed[uint32(id)<<16|ptInt32]
	return int32(uint32(v)), ok
}

func (p *props) bool(id uint16) bool {
	v, ok := p.fixed[uint32(id)<<16|ptBool]
	return ok && v&0xFF != 0
}

// time returns a time property (a FILETIME: hundreds of nanoseconds since
// 1601), if it is set to a time between 1601 and 9999.
func (p *props) time(id uint16) (time.Time, bool) {
	v, ok := p.fixed[uint32(id)<<16|ptTime]
	if !ok || v == 0 || v >= 2650467744000000000 { // 9999-12-31
		return time.Time{}, false
	}
	const epochDiff = 116444736000000000 // 1601 to 1970, in hundreds of nanoseconds
	t := int64(v) - epochDiff
	return time.Unix(t/10_000_000, t%10_000_000*100).UTC(), true
}

// codepageLabel names the character set of a Windows code page.
func codepageLabel(cp int32) string {
	switch cp {
	case 1252:
		return "windows-1252"
	case 65001:
		return "utf-8"
	case 28591:
		return "iso-8859-1"
	case 28605:
		return "iso-8859-15"
	case 28599:
		return "iso-8859-9"
	case 10000:
		return "macintosh"
	case 20127:
		return "us-ascii"
	case 1200:
		return "utf-16le"
	case 1201:
		return "utf-16be"
	case 874, 1250, 1251, 1253, 1254, 1255, 1256, 1257, 1258:
		return fmt.Sprintf("windows-%d", cp)
	case 932:
		return "shift_jis"
	case 936:
		return "gbk"
	case 949:
		return "euc-kr"
	case 950:
		return "big5"
	case 54936:
		return "gb18030"
	case 20866:
		return "koi8-r"
	case 21866:
		return "koi8-u"
	case 51932:
		return "euc-jp"
	case 50220, 50221, 50222:
		return "iso-2022-jp"
	}
	if cp >= 28592 && cp <= 28606 {
		return fmt.Sprintf("iso-8859-%d", cp-28590)
	}
	return fmt.Sprintf("cp%d", cp)
}

// localeCodepage names the Windows ANSI code page of a locale identifier
// (PR_MESSAGE_LOCALE_ID, [MS-LCID]) by its primary language, for the
// languages whose code page does not depend on the sublanguage and is one
// this library decodes; "" for the others.
func localeCodepage(lcid int32) string {
	switch lcid & 0x3FF {
	case 0x03, 0x06, 0x07, 0x09, 0x0A, 0x0B, 0x0C, 0x0F, 0x10, 0x13, 0x14, 0x16, 0x1D, 0x21, 0x2D, 0x36, 0x38, 0x3E, 0x41, 0x56:
		return "windows-1252" // ca, da, de, en, es, fi, fr, is, it, nl, no, pt, sv, id, eu, af, fo, ms, sw, gl
	case 0x05, 0x0E, 0x15, 0x18, 0x1B, 0x1C, 0x24:
		return "windows-1250" // cs, hu, pl, ro, sk, sq, sl
	case 0x02, 0x19, 0x22, 0x23, 0x2F, 0x3F:
		return "windows-1251" // bg, ru, uk, be, mk, kk
	case 0x08:
		return "windows-1253" // el
	case 0x1F:
		return "windows-1254" // tr
	case 0x0D:
		return "windows-1255" // he
	case 0x01, 0x20, 0x29:
		return "windows-1256" // ar, ur, fa
	case 0x25, 0x26, 0x27:
		return "windows-1257" // et, lv, lt
	case 0x2A:
		return "windows-1258" // vi
	case 0x1E:
		return "windows-874" // th
	}
	return ""
}

// msgFile reads an Outlook message.
func (r *reader) msgFile(src io.ReaderAt, size int64) (*gofastcore.Document, error) {
	f, err := cfb.Open(src, size)
	if errors.Is(err, cfb.ErrLimit) {
		return nil, fmt.Errorf("gofastemail: %s: %w", clean.Line(err.Error()), gofastcore.ErrLimitExceeded)
	}
	if err != nil {
		return nil, fmt.Errorf("gofastemail: %s: %w", clean.Line(err.Error()), gofastcore.ErrMalformed)
	}
	if !f.Has("__properties_version1.0") {
		return nil, fmt.Errorf("gofastemail: a compound file that is not an Outlook message: %w", gofastcore.ErrUnsupported)
	}
	if !r.opts.Parts.Contains(1) {
		return nil, fmt.Errorf("gofastemail: %w", gofastcore.ErrNoPartsSelected)
	}
	r.doc = gofastcore.New("msg")
	r.doc.Source = gofastcore.Source{Format: "msg", Name: r.opts.Name, MIME: "application/vnd.ms-outlook", Size: size}
	if f.Damaged != "" {
		r.doc.Warn(gofastcore.SeverityError, gofastcore.WarnInputRepaired, "the compound file's short streams cannot be read ("+clean.Line(f.Damaged)+"); the properties stored in them are missing")
	}
	r.outlook(f, nil, 32)
	if err := r.ctx.Err(); err != nil {
		return nil, err
	}
	return r.finish(), nil
}

// msgStorage extracts the message in the given storage of f, which is
// attached to another, as a document of its own.
func (e *Extractor) msgStorage(ctx context.Context, f *cfb.File, storage []string, opts *gofastcore.Options) (*gofastcore.Document, error) {
	ctx, text := gofastcore.WithTextBudget(ctx, opts.Limits)
	defer text.Close()
	ctx, dec := gofastcore.WithDecodeBudget(ctx, opts.Limits, DefaultMaxDecodedBytes)
	defer dec.Close()
	r := &reader{e: e, ctx: ctx, opts: opts, text: text, dec: dec}
	r.doc = gofastcore.New("msg")
	r.doc.Source = gofastcore.Source{Format: "msg", Name: opts.Name, MIME: "application/vnd.ms-outlook"}
	r.outlook(f, storage, 24)
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return r.finish(), nil
}

// storages returns the names of the storages in prefix that begin with
// the given text, in order, at most maxStorages.
func storages(f *cfb.File, prefix []string, begin string) ([]string, bool) {
	var out []string
	for _, name := range f.List(prefix...) {
		if strings.HasPrefix(strings.ToLower(name), strings.ToLower(begin)) {
			out = append(out, name)
		}
	}
	sort.Strings(out)
	if len(out) > maxStorages {
		return out[:maxStorages], true
	}
	return out, false
}

// outlook reads the message whose storage is prefix (the root if empty)
// into one part of r.doc.
func (r *reader) outlook(f *cfb.File, prefix []string, headerSize int) {
	m := &message{inline: true}
	m.part.Kind, m.part.Number = gofastcore.PartMessage, 1
	p := r.readProps(m, f, prefix, headerSize)
	if cp, ok := p.int32(pidMessageCodepage); ok {
		p.label = codepageLabel(cp)
	} else if cp, ok := p.int32(pidInternetCPID); ok {
		p.label = codepageLabel(cp)
		if lcid, ok := p.int32(pidMessageLocaleID); ok && cp == 65001 {
			p.alt = localeCodepage(lcid)
		}
	}
	line := func(s string) string { return clean.Line(s) }

	subject := line(p.str(pidSubject))
	from := address(line(p.str(pidSenderName)), line(firstOf(p.str(pidSenderSMTPAddress), p.str(pidSenderEmail))))
	if from == "" {
		from = address(line(p.str(pidSentReprName)), line(p.str(pidSentReprEmail)))
	}
	var to, cc, bcc []string
	names, more := storages(f, prefix, "__recip_version1.0_")
	if more {
		m.warn(gofastcore.SeverityError, gofastcore.WarnLimitExceeded, fmt.Sprintf("the message has more than %d recipients; the rest are not read", maxStorages))
	}
	held := int64(0) // bytes of recipients held, before the text budget sees them
	for _, name := range names {
		if held > r.text.Left() {
			m.warn(gofastcore.SeverityError, gofastcore.WarnLimitExceeded, "text limit reached; the rest of the recipients are not read")
			break
		}
		rp := r.readProps(m, f, append(append([]string(nil), prefix...), name), 8)
		rp.label, rp.alt = p.label, p.alt
		a := address(line(rp.str(pidDisplayName)), line(firstOf(rp.str(pidSMTPAddress), rp.str(pidEmailAddress))))
		if a == "" {
			continue
		}
		held += int64(len(a))
		switch t, _ := rp.int32(pidRecipientType); t &^ 0x10000000 { // the high bit marks a resent recipient
		case 2:
			cc = append(cc, a)
		case 3:
			bcc = append(bcc, a)
		default:
			to = append(to, a)
		}
	}
	if len(names) == 0 {
		// No recipient objects: the display lists stand for them.
		to, cc, bcc = nonEmpty(line(p.str(pidDisplayTo))), nonEmpty(line(p.str(pidDisplayCc))), nonEmpty(line(p.str(pidDisplayBcc)))
	}
	date, ok := p.time(pidClientSubmitTime)
	if !ok {
		if date, ok = p.time(pidDeliveryTime); !ok {
			date, ok = p.time(pidCreationTime)
		}
	}
	var lines []string
	add := func(label string, values []string) {
		if len(values) > 0 {
			lines = append(lines, label+": "+strings.Join(values, ", "))
		}
	}
	add("From", nonEmpty(from))
	add("To", to)
	add("Cc", cc)
	add("Bcc", bcc)
	if ok {
		lines = append(lines, "Date: "+date.Format(time.RFC1123Z))
	}
	headersAt := r.head(m, subject, lines)

	md := &r.doc.Metadata
	md.Title = gofastcore.ExpandLigatures(subject)
	if from != "" {
		md.Authors = []string{gofastcore.ExpandLigatures(from)}
	}
	if ok {
		md.Created = date
	}
	if t, ok := p.time(pidModificationTime); ok {
		md.Modified = t
	}
	extra := map[string]string{}
	// The headers the message had in transit, which Outlook keeps and
	// does not show.
	if h := p.str(pidTransportHeaders); h != "" {
		fields, _, prob := rfc822.ParseHeader([]byte(h), r.e.maxHeader())
		r.headerLimits(m, prob)
		extra = joinFields(extra, len(fields), func(yield func(k, v string)) {
			for _, fl := range fields {
				k := strings.ToLower(clean.Line(fl.Name))
				d, c := rfc822.DecodeText(fl.Value, m.charsetReport())
				m.cuts(c)
				v := line(d)
				if k == "" {
					continue
				}
				yield(k, v)
			}
		})
	}
	if v := line(p.str(pidMessageClass)); v != "" {
		extra["message-class"] = v
	}
	if v := line(p.str(pidInternetMessageID)); v != "" {
		extra["message-id"] = v
	}
	if len(extra) > 0 {
		md.Extra = extra
	}

	r.outlookBody(m, p)
	r.outlookAttachments(m, f, prefix, p.label, p.alt)
	r.tail(m, headersAt)
	r.doc.Parts = append(r.doc.Parts, m.part)
}

func firstOf(s ...string) string {
	for _, v := range s {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}

func nonEmpty(s string) []string {
	if s == "" {
		return nil
	}
	return []string{s}
}

// address writes a name and an address as a mail header does.
func address(name, addr string) string {
	switch {
	case name == "" || name == addr:
		return addr
	case addr == "":
		return name
	}
	return name + " <" + addr + ">"
}

// outlookBody reads the bodies: the plain text gives the blocks, and the
// HTML and the RTF are embedded documents.
func (r *reader) outlookBody(m *message, p *props) {
	plain := p.str(pidBody)
	html, hasHTML := p.read(pidHTML, ptBinary)
	if !hasHTML {
		if s := p.str(pidHTML); s != "" {
			html, hasHTML = []byte(s), true
		}
	}
	rtf, hasRTF := p.read(pidRTFCompressed, ptBinary)
	var kinds []string
	if plain != "" {
		kinds = append(kinds, "text/plain")
	}
	if hasHTML {
		kinds = append(kinds, "text/html")
	}
	if hasRTF {
		kinds = append(kinds, "application/rtf")
	}
	if len(kinds) > 1 {
		if plain != "" {
			m.warn(gofastcore.SeverityWarning, WarnAlternatives, fmt.Sprintf("%d alternative bodies (%s); the blocks are the text/plain one, the others are embedded documents", len(kinds), strings.Join(kinds, ", ")))
		} else {
			m.warn(gofastcore.SeverityWarning, WarnAlternatives, fmt.Sprintf("%d alternative bodies (%s), none of them text/plain; each is an embedded document", len(kinds), strings.Join(kinds, ", ")))
		}
	}
	if plain != "" {
		blocks, _ := textbody.Blocks(clean.Text(plain), textbody.Options{}, func(n int) bool { return r.take(m, n) })
		m.body = append(m.body, blocks...)
	}
	if hasHTML {
		mime := "text/html"
		if cp, ok := p.int32(pidInternetCPID); ok {
			mime = m.withCharset(mime, map[string]string{"charset": codepageLabel(cp)})
		}
		if p.f == nil {
			html = r.budget(m, html) // from a TNEF stream; read from a compound file, it is counted
		}
		r.child(m, "", mime, html, false)
	}
	if hasRTF {
		limit := min(r.e.maxMessage(), max(r.dec.Left(), 0))
		out, err := rtfcp.Decompress(rtf, limit)
		switch {
		case errors.Is(err, rtfcp.ErrLimit):
			m.warn(gofastcore.SeverityError, gofastcore.WarnLimitExceeded, "the compressed RTF body decompresses to more than the limit; it is cut there")
		case err != nil:
			m.warn(gofastcore.SeverityWarning, gofastcore.WarnInputRepaired, "the compressed RTF body cannot be read: "+clean.Line(err.Error()))
			out = nil
		}
		if out != nil {
			r.child(m, "", "application/rtf", r.budget(m, out), false)
		}
	}
}

// budget takes len(data) from the decode budget and returns data, or nil
// when the budget is spent.
func (r *reader) budget(m *message, data []byte) []byte {
	if m.undecode || !r.dec.Take(int64(len(data))) {
		if !m.undecode {
			m.undecode = true
			m.warn(gofastcore.SeverityError, gofastcore.WarnLimitExceeded, "decode limit reached; the rest of the message's parts are listed, not read")
		}
		return nil
	}
	if data == nil {
		return []byte{}
	}
	return data
}

// outlookAttachments reads the attachments.
func (r *reader) outlookAttachments(m *message, f *cfb.File, prefix []string, label, alt string) {
	names, more := storages(f, prefix, "__attach_version1.0_")
	if more {
		m.warn(gofastcore.SeverityError, gofastcore.WarnLimitExceeded, fmt.Sprintf("the message has more than %d attachments; the rest are not read", maxStorages))
	}
	for _, name := range names {
		if r.ctx.Err() != nil {
			return
		}
		storage := append(append([]string(nil), prefix...), name)
		ap := r.readProps(m, f, storage, 8)
		ap.label, ap.alt = label, alt
		r.outlookAttachment(m, ap, "", func(file string, listed bool) {
			r.embedded(m, f, append(storage, "__substg1.0_3701000D"), file, listed)
		})
	}
}

// outlookAttachment reads one attachment of an Outlook message or of a
// TNEF stream from its properties. title is a file name to use when the
// properties give none; embedded reads an attached message.
func (r *reader) outlookAttachment(m *message, ap *props, title string, embedded func(file string, listed bool)) {
	method, _ := ap.int32(pidAttachMethod)
	file := m.fileName(firstOf(ap.str(pidAttachLongFilename), ap.str(pidAttachFilename), ap.str(pidDisplayName), title))
	mime := strings.ToLower(clean.Line(ap.str(pidAttachMimeTag)))
	if !rfc822.Token(mime) || !strings.Contains(mime, "/") {
		mime = "application/octet-stream"
	}
	// An attachment Outlook does not list (a picture shown in the
	// body) is extracted all the same, and not named in the headers.
	listed := !ap.bool(pidAttachmentHidden)
	switch method {
	case attachEmbeddedMsg:
		embedded(file, listed)
	case attachOLE:
		// An OLE object is a storage of the application that made it;
		// it is listed, not read.
		r.child(m, file, "application/x-oleobject", nil, listed)
	case attachByReference, attachByRefResolve, attachByRefOnly, attachByWebRef:
		full := clean.Line(firstOf(ap.str(pidAttachLongPathname), ap.str(pidAttachPathname)))
		path := m.reference(full)
		m.warnEach(gofastcore.SeverityWarning, gofastcore.WarnInputExternalReference, "attachment path", full, path,
			"an attachment refers to a file outside the message: "+path, true,
			func(count, list string) string {
				return fmt.Sprintf("attachments refer to %s files outside the message: %s", count, list)
			})
		r.child(m, file, mime, nil, listed)
	default:
		data, ok := ap.read(pidAttachData, ptBinary)
		if !ok {
			r.child(m, file, mime, nil, listed)
			return
		}
		if ap.f == nil {
			data = r.budget(m, data) // from a TNEF stream; read from a compound file, it is counted
		} else if data == nil {
			data = []byte{}
		}
		if isTNEF(mime) || strings.EqualFold(file, "winmail.dat") {
			r.tnefChild(m, file, mime, data, listed)
			return
		}
		if !listed && strings.HasPrefix(mime, "image/") {
			// A picture shown in the body: a block, and an embedded
			// document as well.
			r.image(m, mime, file, "", data)
		}
		r.child(m, file, mime, data, listed)
	}
}

// embedded extracts a message attached to this one. It is an Outlook
// message too, but a storage of this file and not a file of its own, so
// it is read here; it goes through ExtractChild all the same, so that the
// depth limit and the budgets apply as to any embedded document.
func (r *reader) embedded(m *message, f *cfb.File, storage []string, name string, listed bool) {
	if !f.Has(storage...) {
		r.child(m, name, "application/vnd.ms-outlook", nil, listed)
		return
	}
	parent := r.opts.Child
	o := *r.opts
	o.Child = func(ctx context.Context, _, _ string, _ io.ReaderAt, _ int64, co *gofastcore.Options) (*gofastcore.Document, error) {
		co.Child = parent
		return r.e.msgStorage(ctx, f, storage, co)
	}
	if m.cut {
		r.child(m, name, "application/vnd.ms-outlook", nil, listed)
		return
	}
	d := o.ExtractChild(r.ctx, name, "application/vnd.ms-outlook", bytes.NewReader(nil), 0)
	m.children = append(m.children, len(r.doc.Children))
	r.doc.Children = append(r.doc.Children, d)
	if listed {
		label := name
		if label == "" {
			label = "(message)"
		}
		m.attached = append(m.attached, gofastcore.ExpandLigatures(label))
	}
}
