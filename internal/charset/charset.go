// Package charset turns the bytes of a message, in the character set it
// declares, into UTF-8.
//
// It knows UTF-8, US-ASCII, ISO-8859-1, ISO-8859-9, ISO-8859-11 (TIS-620),
// ISO-8859-15, windows-874 and windows-1250 to 1258, Mac OS Roman and
// UTF-16, the double-byte code pages 932 (Shift_JIS), 936 (GBK and
// GB2312), 949 (EUC-KR) and 950 (Big5) (dbcs.go), and GB18030, EUC-JP and
// ISO-2022-JP (cjk.go), from the tables in tables/, which are embedded in
// the binary. KOI8 is not decoded. Any other character set is not guessed
// at: bytes outside ASCII become U+FFFD, and the caller says so in a
// warning that names the set.
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

// tableLabels names the code pages decoded with a table of codepages.go,
// by their labels in the WHATWG Encoding Standard and the names Windows
// and mail programs use. ISO-8859-9 and ISO-8859-11 (TIS-620) are decoded
// as windows-1254 and windows-874, as the WHATWG Encoding Standard does:
// each Windows page is the ISO part with characters added in 0x80 to 0x9F,
// where the ISO part has only control characters, as for ISO-8859-1.
var tableLabels = map[string]*[128]uint16{
	"windows-1250": &cp1250, "cp1250": &cp1250, "x-cp1250": &cp1250,
	"windows-1251": &cp1251, "cp1251": &cp1251, "x-cp1251": &cp1251,
	"windows-1253": &cp1253, "cp1253": &cp1253, "x-cp1253": &cp1253,
	"windows-1254": &cp1254, "cp1254": &cp1254, "x-cp1254": &cp1254,
	"iso-8859-9": &cp1254, "iso8859-9": &cp1254, "iso_8859-9": &cp1254, "latin5": &cp1254, "l5": &cp1254, "iso-ir-148": &cp1254, "csisolatin5": &cp1254,
	"windows-1255": &cp1255, "cp1255": &cp1255, "x-cp1255": &cp1255,
	"windows-1256": &cp1256, "cp1256": &cp1256, "x-cp1256": &cp1256,
	"windows-1257": &cp1257, "cp1257": &cp1257, "x-cp1257": &cp1257,
	"windows-1258": &cp1258, "cp1258": &cp1258, "x-cp1258": &cp1258,
	"windows-874": &cp874, "cp874": &cp874, "dos-874": &cp874, "x-cp874": &cp874,
	"tis-620": &cp874, "iso-8859-11": &cp874, "iso8859-11": &cp874,
	"macintosh": &macRoman, "mac": &macRoman, "x-mac-roman": &macRoman, "csmacintosh": &macRoman, "cp10000": &macRoman,
}

// Known reports whether label names a character set this package decodes.
func Known(label string) bool {
	l := Normalize(label)
	_, ok := labels[l]
	if cp := dbcsLabels[l]; cp != 0 {
		return dbcsTable(cp) != nil
	}
	if set := cjkLabels[l]; set != 0 {
		return cjkReady(set)
	}
	return ok || tableLabels[l] != nil
}

// decodeTable decodes a single-byte code page whose upper half is t; a
// byte t leaves undefined is U+FFFD.
func decodeTable(t *[128]uint16, b []byte) (string, Result) {
	res := Result{Known: true}
	var sb strings.Builder
	sb.Grow(len(b) + len(b)/2)
	for _, c := range b {
		switch {
		case c < 0x80:
			sb.WriteByte(c)
		case t[c-0x80] == 0:
			sb.WriteRune(utf8.RuneError)
			res.Replaced = true
		default:
			sb.WriteRune(rune(t[c-0x80]))
		}
	}
	return sb.String(), res
}

// Decode returns b, in the character set label names, as UTF-8. It never
// fails: what cannot be decoded is U+FFFD, and Result says so.
func Decode(label string, b []byte) (string, Result) {
	if set := cjkLabels[Normalize(label)]; set != 0 && cjkReady(set) {
		return decodeCJK(set, b)
	}
	if cp := dbcsLabels[Normalize(label)]; cp != 0 {
		if t := dbcsTable(cp); t != nil {
			return decodeDBCS(t, cp, b)
		}
	}
	if t := tableLabels[Normalize(label)]; t != nil {
		return decodeTable(t, b)
	}
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
		if c >= 0x80 && c < 0xA0 && cp1252C1[c-0x80] == 0xFFFD {
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
			sb.WriteRune(cp1252C1[c-0x80])
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
