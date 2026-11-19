package charset

import "testing"

func TestDecode(t *testing.T) {
	for _, c := range []struct {
		label    string
		in       string
		want     string
		known    bool
		replaced bool
	}{
		{"utf-8", "caf\xc3\xa9", "café", true, false},
		{"UTF-8", "bad\xff", "bad�", true, true},
		{"", "plain", "plain", true, false},
		{"us-ascii", "caf\xc3\xa9", "café", true, false}, // valid UTF-8 mislabelled
		{"us-ascii", "caf\xe9", "caf�", true, true},
		{"ISO-8859-1", "caf\xe9 \x80 \x93x\x94", "café € “x”", true, false},
		{"\"windows-1252\"", "\x81", "�", true, true},
		{"iso-8859-15", "\xa4\xbd\xe9", "€œé", true, false},
		{"utf-16le", "h\x00i\x00", "hi", true, false},
		{"utf-16", "\xfe\xff\x00h\x00i", "hi", true, false},
		{"utf-16be", "\xd8\x00", "�", true, true},
		{"koi8-r", "a\xc1b", "a�b", false, true},
		{"utf-8*en", "x", "x", true, false},
	} {
		got, res := Decode(c.label, []byte(c.in))
		if got != c.want || res.Known != c.known || res.Replaced != c.replaced {
			t.Errorf("Decode(%q, %q) = %q %+v; want %q known=%v replaced=%v", c.label, c.in, got, res, c.want, c.known, c.replaced)
		}
	}
}

// TestCP1252 checks the table against the code points of the WHATWG
// index, written out a second time in a different form.
func TestCP1252(t *testing.T) {
	want := map[byte]rune{
		0x80: '€', 0x82: '‚', 0x83: 'ƒ', 0x84: '„', 0x85: '…', 0x86: '†', 0x87: '‡',
		0x88: 'ˆ', 0x89: '‰', 0x8A: 'Š', 0x8B: '‹', 0x8C: 'Œ', 0x8E: 'Ž',
		0x91: '‘', 0x92: '’', 0x93: '“', 0x94: '”', 0x95: '•', 0x96: '–', 0x97: '—',
		0x98: '˜', 0x99: '™', 0x9A: 'š', 0x9B: '›', 0x9C: 'œ', 0x9E: 'ž', 0x9F: 'Ÿ',
	}
	for b := 0x80; b < 0xA0; b++ {
		r, ok := want[byte(b)]
		if !ok {
			r = 0xFFFD
		}
		if cp1252[b-0x80] != r {
			t.Errorf("0x%02X = U+%04X, want U+%04X", b, cp1252[b-0x80], r)
		}
	}
}
