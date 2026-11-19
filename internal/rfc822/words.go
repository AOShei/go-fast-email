package rfc822

import (
	"strings"
	"unicode/utf8"

	"github.com/AOShei/go-fast-email/internal/charset"
)

// CharsetFunc is told of every decoding that did not go cleanly: a
// character set this package does not know, or bytes that are not valid
// in the one declared. An empty label stands for bytes outside ASCII in a
// header that declares no character set.
type CharsetFunc func(label string, res charset.Result)

// DecodeText returns a header value as text: its bytes as UTF-8, with the
// encoded words of RFC 2047 decoded, and the whitespace between two
// adjacent encoded words removed as that RFC has it. Bytes outside ASCII
// outside an encoded word are taken as UTF-8 (RFC 6532); where they are
// not valid UTF-8 they become U+FFFD and report is told.
func DecodeText(raw []byte, report CharsetFunc) string {
	var s string
	if utf8.Valid(raw) {
		s = string(raw)
	} else {
		var res charset.Result
		s, res = charset.Decode("", raw)
		if report != nil {
			report("", res)
		}
	}
	if !strings.Contains(s, "=?") {
		return s
	}
	var sb strings.Builder
	sb.Grow(len(s))
	pending := "" // whitespace after an encoded word, written only if text follows
	afterWord := false
	for len(s) > 0 {
		i := strings.Index(s, "=?")
		if i < 0 {
			sb.WriteString(pending)
			sb.WriteString(s)
			break
		}
		text, rest, ok := encodedWord(s[i:], report)
		if !ok {
			sb.WriteString(pending)
			pending = ""
			sb.WriteString(s[:i+2])
			s = s[i+2:]
			afterWord = false
			continue
		}
		gap := s[:i]
		if afterWord && strings.Trim(gap, " \t\r\n") == "" {
			gap = "" // between two encoded words
		} else {
			sb.WriteString(pending)
		}
		pending = ""
		sb.WriteString(gap)
		sb.WriteString(text)
		s = rest
		afterWord = true
		// Whitespace that follows is held back until it is known whether
		// another encoded word follows it.
		j := 0
		for j < len(s) && (s[j] == ' ' || s[j] == '\t' || s[j] == '\r' || s[j] == '\n') {
			j++
		}
		pending, s = s[:j], s[j:]
	}
	return sb.String()
}

// encodedWord decodes the encoded word "=?charset?enc?text?=" at the start
// of s, and returns its text and what follows it.
func encodedWord(s string, report CharsetFunc) (string, string, bool) {
	// The longest word RFC 2047 allows is 75 characters; longer ones are
	// written by some programs, so the bound here is looser.
	const maxWord = 4096
	body := s[2:]
	q1 := strings.IndexByte(body, '?')
	if q1 <= 0 || q1 > 100 || len(body) < q1+3 || body[q1+2] != '?' {
		return "", "", false
	}
	label, enc := body[:q1], body[q1+1]
	text := body[q1+3:]
	end := strings.Index(text, "?=")
	if end < 0 || end > maxWord || strings.ContainsAny(text[:end], " \t\r\n") {
		return "", "", false
	}
	var raw []byte
	switch enc {
	case 'B', 'b':
		raw = Base64([]byte(text[:end]))
	case 'Q', 'q':
		raw = qword(text[:end])
	default:
		return "", "", false
	}
	if strings.ContainsAny(label, " \t\"") {
		return "", "", false
	}
	out, res := charset.Decode(label, raw)
	if (!res.Known || res.Replaced) && report != nil {
		report(charset.Normalize(label), res)
	}
	return out, text[end+2:], true
}

// qword decodes the Q encoding of an encoded word: "_" is a space and
// "=XX" a byte.
func qword(s string) []byte {
	out := make([]byte, 0, len(s))
	for i := 0; i < len(s); i++ {
		switch c := s[i]; {
		case c == '_':
			out = append(out, ' ')
		case c == '=' && i+2 < len(s) && isHex(s[i+1]) && isHex(s[i+2]):
			out = append(out, unhex(s[i+1])<<4|unhex(s[i+2]))
			i += 2
		default:
			out = append(out, c)
		}
	}
	return out
}

func isHex(c byte) bool {
	return c >= '0' && c <= '9' || c >= 'a' && c <= 'f' || c >= 'A' && c <= 'F'
}

func unhex(c byte) byte {
	switch {
	case c >= '0' && c <= '9':
		return c - '0'
	case c >= 'a' && c <= 'f':
		return c - 'a' + 10
	}
	return c - 'A' + 10
}
