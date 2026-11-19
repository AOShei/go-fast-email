// Package charset turns the bytes of a message, in the character set it
// declares, into UTF-8.
//
// It knows UTF-8, US-ASCII, ISO-8859-1, ISO-8859-15, windows-1252 and
// UTF-16. Any other character set is not guessed at: bytes outside ASCII
// become U+FFFD, and the caller says so in a warning that names the set.
// Decoding a multi-byte set as a single-byte one would produce text that
// looks like text and is not.
package charset

import (
	"strings"
	"unicode/utf16"
	"unicode/utf8"
)

// Result says how a decoding went.
type Result struct {
	// Known is false when the character set is not one this package
	// decodes: bytes outside ASCII were replaced with U+FFFD.
	Known bool
	// Replaced is true when some bytes could not be decoded and were
	// replaced with U+FFFD.
	Replaced bool
}

// Normalize returns the lowercase name of a charset label, without
// quotes, spaces or an RFC 2231 language suffix.
func Normalize(label string) string {
	label = strings.ToLower(strings.Trim(strings.TrimSpace(label), `"'`))
	if i := strings.IndexByte(label, '*'); i >= 0 { // RFC 2231 "charset*language"
		label = label[:i]
	}
	return label
}

type kind uint8

const (
	kindUnknown kind = iota
	kindUTF8
	kindASCII // and no label at all: UTF-8 is accepted where the bytes are valid
	kindCP1252
	kindLatin9
	kindUTF16LE
	kindUTF16BE
	kindUTF16 // byte order from a byte order mark, big-endian without one
)

var labels = map[string]kind{
	"utf-8": kindUTF8, "utf8": kindUTF8, "unicode-1-1-utf-8": kindUTF8,
	"": kindASCII, "us-ascii": kindASCII, "ascii": kindASCII, "ansi_x3.4-1968": kindASCII, "iso646-us": kindASCII, "us": kindASCII,
	// ISO-8859-1 is decoded as windows-1252, as the WHATWG Encoding
	// Standard does for every label of it: the two differ only in 0x80 to
	// 0x9F, which in ISO-8859-1 are control characters no text uses and
	// which in mail labelled ISO-8859-1 are nearly always windows-1252.
	"iso-8859-1": kindCP1252, "iso8859-1": kindCP1252, "iso_8859-1": kindCP1252, "latin1": kindCP1252, "l1": kindCP1252,
	"cp819": kindCP1252, "ibm819": kindCP1252, "iso-ir-100": kindCP1252, "iso_8859-1:1987": kindCP1252, "csisolatin1": kindCP1252,
	"windows-1252": kindCP1252, "cp1252": kindCP1252, "x-cp1252": kindCP1252,
	"iso-8859-15": kindLatin9, "iso8859-15": kindLatin9, "iso_8859-15": kindLatin9, "latin9": kindLatin9, "latin-9": kindLatin9, "l9": kindLatin9, "csisolatin9": kindLatin9,
	"utf-16le": kindUTF16LE, "utf-16be": kindUTF16BE, "utf-16": kindUTF16, "ucs-2": kindUTF16,
}

// Known reports whether label names a character set this package decodes.
func Known(label string) bool {
	_, ok := labels[Normalize(label)]
	return ok
}

// Decode returns b, in the character set label names, as UTF-8. It never
// fails: what cannot be decoded is U+FFFD, and Result says so.
func Decode(label string, b []byte) (string, Result) {
	k := labels[Normalize(label)]
	switch k {
	case kindUTF8, kindASCII:
		// Text labelled US-ASCII, or not labelled, that is valid UTF-8 is
		// taken as UTF-8: mail programs write it so.
		if utf8.Valid(b) {
			return string(b), Result{Known: true}
		}
		return strings.ToValidUTF8(string(b), "�"), Result{Known: true, Replaced: true}
	case kindCP1252, kindLatin9:
		return singleByte(b, k), Result{Known: true, Replaced: k == kindCP1252 && hasUndefined1252(b)}
	case kindUTF16LE, kindUTF16BE, kindUTF16:
		return DecodeUTF16(b, k == kindUTF16LE)
	}
	var sb strings.Builder
	sb.Grow(len(b))
	replaced := false
	for _, c := range b {
		if c < 0x80 {
			sb.WriteByte(c)
		} else {
			sb.WriteString("�")
			replaced = true
		}
	}
	return sb.String(), Result{Known: false, Replaced: replaced}
}

// DecodeUTF16 decodes UTF-16 in the byte order a byte order mark gives, or
// in little-endian order if le and there is no mark, else big-endian.
// Unpaired surrogates and an odd last byte become U+FFFD.
func DecodeUTF16(b []byte, le bool) (string, Result) {
	if len(b) >= 2 {
		switch {
		case b[0] == 0xFF && b[1] == 0xFE:
			le, b = true, b[2:]
		case b[0] == 0xFE && b[1] == 0xFF:
			le, b = false, b[2:]
		}
	}
	units := make([]uint16, len(b)/2)
	for i := range units {
		if le {
			units[i] = uint16(b[2*i]) | uint16(b[2*i+1])<<8
		} else {
			units[i] = uint16(b[2*i])<<8 | uint16(b[2*i+1])
		}
	}
	res := Result{Known: true}
	runes := utf16.Decode(units)
	var sb strings.Builder
	sb.Grow(len(runes))
	for i, r := range runes {
		if r == utf8.RuneError {
			// utf16.Decode gives U+FFFD for an unpaired surrogate; a
			// U+FFFD in the source is decoded the same way.
			if units[i] != 0xFFFD {
				res.Replaced = true
			}
		}
		sb.WriteRune(r)
	}
	if len(b)%2 == 1 {
		sb.WriteString("�")
		res.Replaced = true
	}
	return sb.String(), res
}

func hasUndefined1252(b []byte) bool {
	for _, c := range b {
		if c >= 0x80 && c < 0xA0 && cp1252[c-0x80] == 0xFFFD {
			return true
		}
	}
	return false
}

func singleByte(b []byte, k kind) string {
	var sb strings.Builder
	sb.Grow(len(b) + len(b)/4)
	for _, c := range b {
		switch {
		case c < 0x80:
			sb.WriteByte(c)
		case k == kindCP1252 && c < 0xA0:
			sb.WriteRune(cp1252[c-0x80])
		case k == kindLatin9:
			if r, ok := latin9[c]; ok {
				sb.WriteRune(r)
			} else {
				sb.WriteRune(rune(c))
			}
		default:
			sb.WriteRune(rune(c))
		}
	}
	return sb.String()
}
