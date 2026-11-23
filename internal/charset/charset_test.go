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
		{"us-ascii", "caf\xe9", "café", true, false}, // recognised as windows-1252
		{"", "\xe9\xe8\xe9\xe8\xe9\xe8", "\uFFFD", true, true}, // no set is certain: one U+FFFD for the run
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
		if cp1252C1[b-0x80] != r {
			t.Errorf("0x%02X = U+%04X, want U+%04X", b, cp1252C1[b-0x80], r)
		}
	}
}

// The windows-1252 table copied from go-fast-rtf and the one written here
// from the WHATWG index were transcribed separately; they must agree.
func TestCP1252TablesAgree(t *testing.T) {
	for i := 0; i < 32; i++ {
		want := rune(cp1252[i])
		if want == 0 {
			want = 0xFFFD
		}
		if cp1252C1[i] != want {
			t.Errorf("0x%02X: U+%04X and U+%04X", 0x80+i, cp1252C1[i], want)
		}
	}
	for i := 32; i < 128; i++ {
		if cp1252[i] != uint16(0x80+i) {
			t.Errorf("0x%02X = U+%04X", 0x80+i, cp1252[i])
		}
	}
}

func TestCodePages(t *testing.T) {
	cases := []struct{ label, in, want string }{
		{"windows-1251", "\xcf\xf0\xe8\xe2\xe5\xf2", "\U0000041f\U00000440\U00000438\U00000432\U00000435\U00000442"},
		{"cp1250", "\xa3\xf3d\x9f", "\U00000141\U000000f3d\U0000017a"},
		{"ISO-8859-9", "\xdd\xfe", "\U00000130\U0000015f"},
		{"macintosh", "caf\x8e", "caf\U000000e9"},
		{"windows-1253", "\xe1", "\U000003b1"},
		{"tis-620", "\xa1", "\U00000e01"},
	}
	for _, c := range cases {
		got, res := Decode(c.label, []byte(c.in))
		if got != c.want || !res.Known || res.Replaced || !Known(c.label) {
			t.Errorf("%s: %q %+v, want %q", c.label, got, res, c.want)
		}
	}
	// A byte the mapping leaves undefined.
	if got, res := Decode("windows-1250", []byte("a\x81")); got != "a\U0000fffd" || !res.Replaced {
		t.Errorf("undefined byte: %q %+v", got, res)
	}
	// Not decoded: no verifiable table.
	for _, l := range []string{"koi8-r", "iso-8859-2", "iso-8859-5", "iso-8859-7"} {
		if Known(l) {
			t.Errorf("%s is known", l)
		}
	}
}
