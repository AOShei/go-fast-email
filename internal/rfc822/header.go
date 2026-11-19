// Package rfc822 reads the parts of an Internet message (RFC 5322) and of
// MIME (RFC 2045 to 2049, 2231): the header section, encoded words, the
// parameters of a header, the parts of a multipart body, and the transfer
// encodings. It works on bytes in memory, trusts nothing it reads, and
// never fails: what is damaged is read as far as it goes, and the caller
// is told.
package rfc822

import (
	"bytes"
	"strings"
)

// Field is one header field: its name as written and its value with the
// folding removed, still in the bytes of the message.
type Field struct {
	Name  string
	Value []byte
}

// Header is the fields of a header section, in order.
type Header []Field

// Get returns the value of the first field with the given name, compared
// without regard to case, and whether there is one.
func (h Header) Get(name string) ([]byte, bool) {
	for i := range h {
		if strings.EqualFold(h[i].Name, name) {
			return h[i].Value, true
		}
	}
	return nil, false
}

// Problems found while a header section was read.
type Problems struct {
	// Truncated: the section was longer than the limit and was cut there.
	Truncated bool
	// Malformed: a line that is not a field ended the section where no
	// blank line did; the body begins at that line.
	Malformed bool
	// TooMany: there were more fields than the limit; the rest were left
	// out.
	TooMany bool
}

// MaxFields bounds the fields kept from one header section.
const MaxFields = 10_000

// ParseHeader reads the header section at the start of msg and returns its
// fields and the offset at which the body begins. At most maxBytes of msg
// are read as header; a section longer than that is cut there and the body
// is taken to begin at the end of it.
//
// A line that is neither a field nor the continuation of one ends the
// section, as a blank line does, and the body begins with that line. A
// first line that is an mbox "From " line is skipped.
func ParseHeader(msg []byte, maxBytes int) (Header, int, Problems) {
	var h Header
	var p Problems
	pos := 0
	limit := len(msg)
	if maxBytes > 0 && limit > maxBytes {
		limit = maxBytes
	}
	if bytes.HasPrefix(msg, []byte("From ")) {
		pos = lineEnd(msg, 0)
	}
	for pos < limit {
		end := lineEnd(msg, pos)
		line := trimEOL(msg[pos:end])
		if len(line) == 0 {
			return h, end, p // the blank line that ends the section
		}
		if (line[0] == ' ' || line[0] == '\t') && len(h) > 0 {
			// The continuation of the field before: the line break is
			// removed and the whitespace kept (RFC 5322, 2.2.3).
			if !p.TooMany {
				last := &h[len(h)-1]
				last.Value = append(last.Value, line...)
			}
			pos = end
			continue
		}
		colon := bytes.IndexByte(line, ':')
		if colon <= 0 || !fieldName(line[:colon]) {
			if line[0] == ' ' || line[0] == '\t' {
				// Whitespace before the first field: skipped.
				pos = end
				continue
			}
			p.Malformed = true
			return h, pos, p
		}
		if len(h) >= MaxFields {
			p.TooMany = true
		} else {
			value := bytes.TrimLeft(line[colon+1:], " \t")
			h = append(h, Field{Name: string(bytes.TrimRight(line[:colon], " \t")), Value: append([]byte(nil), value...)})
		}
		pos = end
	}
	if limit < len(msg) {
		p.Truncated = true
		return h, limit, p
	}
	return h, len(msg), p
}

// fieldName reports whether b is a field name: printable ASCII other than
// the colon (RFC 5322, 3.6.8). Whitespace before the colon is tolerated,
// as old mail has it.
func fieldName(b []byte) bool {
	b = bytes.TrimRight(b, " \t")
	if len(b) == 0 {
		return false
	}
	for _, c := range b {
		if c <= ' ' || c >= 0x7F {
			return false
		}
	}
	return true
}

// lineEnd returns the offset just past the line that begins at pos: past
// its "\n", or the end of b.
func lineEnd(b []byte, pos int) int {
	if i := bytes.IndexByte(b[pos:], '\n'); i >= 0 {
		return pos + i + 1
	}
	return len(b)
}

func trimEOL(line []byte) []byte {
	line = bytes.TrimSuffix(line, []byte("\n"))
	return bytes.TrimSuffix(line, []byte("\r"))
}
