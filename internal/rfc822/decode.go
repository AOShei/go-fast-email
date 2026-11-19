package rfc822

import (
	"bytes"
	"encoding/base64"
)

// Base64 decodes base64 leniently: characters outside the alphabet (line
// breaks, spaces, stray punctuation) are skipped, decoding stops at the
// first "=", and a last group that is short is decoded as far as it goes.
// The result is never longer than three quarters of the input.
func Base64(in []byte) []byte {
	clean := make([]byte, 0, len(in))
	for _, c := range in {
		switch {
		case c >= 'A' && c <= 'Z', c >= 'a' && c <= 'z', c >= '0' && c <= '9', c == '+', c == '/':
			clean = append(clean, c)
		case c == '=':
			goto done
		}
	}
done:
	if len(clean)%4 == 1 { // one character is no byte
		clean = clean[:len(clean)-1]
	}
	out := make([]byte, base64.RawStdEncoding.DecodedLen(len(clean)))
	n, _ := base64.RawStdEncoding.Decode(out, clean)
	return out[:n]
}

// Base64Bad reports whether in holds anything other than the base64
// alphabet, padding and whitespace: a sign that it was damaged.
func Base64Bad(in []byte) bool {
	for _, c := range in {
		switch {
		case c >= 'A' && c <= 'Z', c >= 'a' && c <= 'z', c >= '0' && c <= '9', c == '+', c == '/', c == '=',
			c == '\r', c == '\n', c == ' ', c == '\t':
		default:
			return true
		}
	}
	return false
}

// QuotedPrintable decodes the quoted-printable encoding (RFC 2045, 6.7)
// leniently: "=XX" is a byte, "=" at the end of a line joins it to the
// next, whitespace at the end of a line is removed, and an "=" followed by
// anything else is kept as it is. Line breaks become "\n" (canonical CRLF
// becomes the line break of the text).
func QuotedPrintable(in []byte) []byte {
	out := make([]byte, 0, len(in))
	for len(in) > 0 {
		var line []byte
		eol := false
		if i := bytes.IndexByte(in, '\n'); i >= 0 {
			line, in, eol = in[:i], in[i+1:], true
		} else {
			line, in = in, nil
		}
		line = bytes.TrimSuffix(line, []byte("\r"))
		line = bytes.TrimRight(line, " \t")
		soft := false
		if bytes.HasSuffix(line, []byte("=")) {
			soft, line = true, line[:len(line)-1]
		}
		for i := 0; i < len(line); i++ {
			c := line[i]
			if c == '=' && i+2 < len(line) && isHex(line[i+1]) && isHex(line[i+2]) {
				out = append(out, unhex(line[i+1])<<4|unhex(line[i+2]))
				i += 2
				continue
			}
			out = append(out, c)
		}
		if eol && !soft {
			out = append(out, '\n')
		}
	}
	return out
}
