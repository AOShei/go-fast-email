package gofastemail

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"

	"github.com/AOShei/go-fast-core"
	"github.com/AOShei/go-fast-email/internal/clean"
	"github.com/AOShei/go-fast-email/internal/rfc822"
)

// DefaultMaxParts bounds the messages read from a mailbox when
// Limits.MaxParts is zero.
const DefaultMaxParts = 1_000_000

// maxEnvelope bounds the bytes of the "From " line kept for a message
// (as text, bytes not valid UTF-8 become U+FFFD).
const maxEnvelope = 1024

// span is one message of a mailbox: the bytes after its "From " line.
type span struct {
	start, end int64
	envelope   string
	// long: the bytes of the "From " line, where it is cut to its first
	// maxEnvelope bytes in the envelope; 0 otherwise.
	long int
	// counted: the end was found from a Content-Length header (mboxcl2),
	// so lines in the body that begin ">From " are as the sender wrote
	// them and are not unquoted.
	counted bool
}

// mbox reads a mailbox: messages each introduced by a "From " line.
//
// A "From " line separates messages where it begins the file or follows a
// blank line. Where a message has a Content-Length header and the byte it
// points to is the end of the file or the next "From " line, the message
// is taken to end there (mboxcl2), and "From " lines inside it are text.
// Otherwise lines that begin ">From ", ">>From ", ... lose one ">"
// (mboxrd). A mailbox in the older mboxo form, which quotes only "From ",
// reads the same way except for lines that began ">From " in the
// original, which lose their ">".
func (r *reader) mbox(src io.ReaderAt, size int64) (*gofastcore.Document, error) {
	r.doc = gofastcore.New("mbox")
	r.doc.Source = gofastcore.Source{Format: "mbox", Name: r.opts.Name, MIME: "application/mbox", Size: size}
	maxParts := int(min(gofastcore.ResolveLimit(int64(r.opts.Limits.MaxParts), DefaultMaxParts), 1<<40))
	number := 0
	var stop error
	emit := func(s span) bool {
		number++
		if err := r.ctx.Err(); err != nil {
			stop = err
			return false
		}
		if !r.opts.Parts.Contains(number) {
			return r.opts.Parts.Last == 0 || number < r.opts.Parts.Last
		}
		if len(r.doc.Parts) >= maxParts {
			r.doc.Warn(gofastcore.SeverityError, gofastcore.WarnLimitExceeded, fmt.Sprintf("the mailbox has more than %d messages; the rest are not read", maxParts))
			return false
		}
		if r.text.Spent() {
			r.doc.Warn(gofastcore.SeverityError, gofastcore.WarnLimitExceeded, fmt.Sprintf("text limit reached; messages from %d on are not read", number))
			return false
		}
		r.doc.Parts = append(r.doc.Parts, r.mboxMessage(src, s, number))
		return true
	}
	if err := scanMbox(src, size, r.e.maxHeader(), emit); err != nil {
		return nil, fmt.Errorf("gofastemail: reading: %v: %w", err, gofastcore.ErrMalformed)
	}
	if stop != nil {
		return nil, stop
	}
	if len(r.doc.Parts) == 0 {
		return nil, fmt.Errorf("gofastemail: the mailbox has %d messages: %w", number, gofastcore.ErrNoPartsSelected)
	}
	return r.finish(), nil
}

// mboxMessage reads one message of a mailbox into a part.
func (r *reader) mboxMessage(src io.ReaderAt, s span, number int) gofastcore.Part {
	m := &message{def: r.e.Charset}
	n := s.end - s.start
	if limit := r.e.maxMessage(); n > limit {
		m.warn(gofastcore.SeverityError, gofastcore.WarnLimitExceeded, fmt.Sprintf("message of %d bytes read to %d", n, limit))
		n = limit
	}
	buf := getBuffer(n)
	defer putBuffer(buf)
	got, _ := src.ReadAt(buf, s.start)
	raw := buf[:got]
	if !s.counted {
		raw = unquote(raw)
	}
	// A part takes from the text budget what a block does, so that a
	// mailbox of a great many empty messages is bounded too.
	if !r.take(m, gofastcore.BlockCost) {
		m.part.Kind, m.part.Number = gofastcore.PartMessage, number
		m.finish()
		return m.part
	}
	if s.long > 0 {
		m.warn(gofastcore.SeverityError, gofastcore.WarnLimitExceeded, fmt.Sprintf("the mailbox \"From \" line of this message (%d bytes) is cut to its first %d bytes in the headers note", s.long, maxEnvelope))
	}
	r.message(m, raw, number, s.envelope)
	return m.part
}

// unquote removes one ">" from every line that begins with one or more
// ">" and then "From " (mboxrd).
//
// The lines are moved down in b, which is returned shortened: a line is
// never written past where it was read.
func unquote(b []byte) []byte {
	if !bytes.Contains(b, []byte(">From ")) {
		return b
	}
	out := b[:0]
	for len(b) > 0 {
		end := bytes.IndexByte(b, '\n') + 1
		if end == 0 {
			end = len(b)
		}
		line := b[:end]
		if i := bytes.IndexFunc(line, func(r rune) bool { return r != '>' }); i > 0 && bytes.HasPrefix(line[i:], []byte("From ")) {
			line = line[1:]
		}
		out = append(out, line...)
		b = b[end:]
	}
	return out
}

// scanMbox finds the messages of a mailbox and calls emit with each, in
// order, until it returns false. It reads the file a line at a time, and
// holds no more than a buffer and one header section.
func scanMbox(src io.ReaderAt, size int64, maxHeader int, emit func(span) bool) error {
	// One reader for the whole scan, with a buffer the size of the file
	// rounded up to a power of two, from 4 KiB to 64 KiB, and reused from
	// one file to the next: a file of one message of a few KB (an .eml
	// saved with its "From " line) does not take 64 KiB, nor a mailbox a
	// new buffer after every counted message.
	const bufSize = 64 << 10
	var off int64
	br := getReader(io.NewSectionReader(src, 0, size), min(size, bufSize))
	defer putReader(br)
	newReader := func(at int64) *bufio.Reader {
		br.Reset(io.NewSectionReader(src, at, size-at))
		return br
	}
	prevBlank, atLineStart := true, true
	var cur *span
	for off < size {
		lineStart := off
		chunk, err := br.ReadSlice('\n')
		off += int64(len(chunk))
		whole := !errors.Is(err, bufio.ErrBufferFull)
		if err != nil && !errors.Is(err, bufio.ErrBufferFull) && !errors.Is(err, io.EOF) {
			return err
		}
		if atLineStart && prevBlank && bytes.HasPrefix(chunk, []byte("From ")) {
			// The line is cut in its bytes, before it is made text.
			line := lineBytes(chunk[5:], whole)
			env := clean.Line(strings.ToValidUTF8(clean.Cut(string(line), maxEnvelope), "�"))
			var n lineLength
			n.add(chunk[5:], whole)
			for !whole { // the rest of a very long line
				chunk, err = br.ReadSlice('\n')
				off += int64(len(chunk))
				whole = !errors.Is(err, bufio.ErrBufferFull)
				n.add(chunk, whole)
			}
			long := n.n
			if cur != nil {
				cur.end = lineStart
				if !emit(*cur) {
					return nil
				}
			}
			if long <= maxEnvelope {
				long = 0
			}
			cur = &span{start: off, envelope: env, long: long}
			if end, ok := countedEnd(src, size, off, maxHeader); ok {
				cur.end, cur.counted = end, true
				if !emit(*cur) {
					return nil
				}
				cur = nil
				off = end
				br = newReader(off)
				prevBlank, atLineStart = true, true
				continue
			}
		}
		prevBlank = atLineStart && whole && len(bytes.TrimRight(chunk, "\r\n")) == 0
		atLineStart = whole
		if errors.Is(err, io.EOF) {
			break
		}
	}
	if cur != nil {
		cur.end = size
		emit(*cur)
	}
	return nil
}

// lineBytes returns the bytes of a line read up to a buffer's size: its
// line break removed where the line ends there (whole), and as they are
// where it goes on.
func lineBytes(chunk []byte, whole bool) []byte {
	if whole {
		return bytes.TrimRight(chunk, "\r\n")
	}
	return chunk
}

// lineLength counts the bytes of a line read a buffer's size at a time,
// without its line break: "\r" bytes that end a buffer are held back
// until the next shows whether they are the line's own or begin its
// break.
type lineLength struct{ n, held int }

func (l *lineLength) add(chunk []byte, whole bool) {
	cut := "\r"
	if whole {
		cut = "\r\n"
	}
	t := bytes.TrimRight(chunk, cut)
	if len(t) > 0 {
		l.n += l.held + len(t)
		l.held = 0
	}
	l.held += len(chunk) - len(t)
	if whole {
		l.held = 0
	}
}

// countedEnd returns the end of the message whose header begins at start,
// as its Content-Length header gives it, if it has one and the byte it
// points to is the end of the file or a "From " line after at most one
// blank line.
func countedEnd(src io.ReaderAt, size, start int64, maxHeader int) (int64, bool) {
	// The header is read a little at a time, growing until a blank line
	// ends it within what was read: a mailbox of a million tiny messages
	// must not cost a million times the largest header.
	avail := min(int64(maxHeader), size-start)
	var head []byte
	var h rfc822.Header
	var bodyAt int
	var p rfc822.Problems
	s := getScratch()
	defer putScratch(s)
	defer func() { putBuffer(head) }()
	for want := min(avail, 256); ; want = min(avail, want*4) {
		if head != nil {
			putBuffer(head)
		}
		head = getBuffer(want)
		n, _ := src.ReadAt(head, start)
		head = head[:n]
		h, bodyAt, p = s.ParseHeader(head, len(head))
		ended := bodyAt < len(head) && !p.Malformed || // a blank line ended it
			p.Malformed && bytes.IndexByte(head[bodyAt:], '\n') >= 0 // a whole line that is no field
		if ended || want >= avail || int64(n) < want {
			break
		}
	}
	if p.Truncated {
		return 0, false
	}
	v, ok := h.Get("Content-Length")
	if !ok {
		return 0, false
	}
	length, err := strconv.ParseInt(strings.TrimSpace(string(v)), 10, 64)
	if err != nil || length < 0 || length > size {
		return 0, false
	}
	end := start + int64(bodyAt) + length
	if end == size {
		return end, true
	}
	if end > size {
		return 0, false
	}
	var next [16]byte
	k, _ := src.ReadAt(next[:], end)
	rest := next[:k]
	for range 2 { // up to one blank line, then the "From " line
		if bytes.HasPrefix(rest, []byte("From ")) {
			return end, true
		}
		switch {
		case bytes.HasPrefix(rest, []byte("\r\n")):
			end, rest = end+2, rest[2:]
		case bytes.HasPrefix(rest, []byte("\n")):
			end, rest = end+1, rest[1:]
		default:
			return 0, false
		}
		if end == size {
			return end, true
		}
	}
	return 0, false
}
