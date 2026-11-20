package charset

import (
	"testing"
	"unicode/utf8"
)

// u returns the characters of code points cps, so that expected text is
// written without non-ASCII characters in the source.
func u(cps ...rune) string { return string(cps) }

const ffd = string(utf8.RuneError)

func TestGB18030(t *testing.T) {
	for _, c := range []struct {
		in, want string
		replaced bool
	}{
		{"a\xd6\xd0\xce\xc4b", "a" + u(0x4E2D, 0x6587) + "b", false}, // two-byte
		{"\x80", u(0x20AC), false},
		{"\x81\x30\x84\x36", u(0xA5), false},                 // four-byte, pointer 36 (gb18030-4.txt "24 A5")
		{"\x95\x32\x82\x36", u(0x20000), false},              // supplementary
		{"\xe3\x32\x9a\x35", u(0x10FFFF), false},             // the last pointer, 1237575
		{"\x81\x35\xf4\x37", u(0xE7C7), false},               // pointer 7457
		{"\xa8\xbc", u(0x1E3F), false},                       // as the Encoding Standard has it
		{"\x84\x31\xa5\x30z", ffd + "z", true},               // pointer 39420: an error of four bytes
		{"\xe3\x32\x9a\x36z", ffd + "z", true},               // pointer 1237576
		{"\x81\x30\x81", ffd, true},                          // truncated: one error
		{"x\xd6", "x" + ffd, true},                           // lead byte at the end
		{"\x81\x30z", ffd + "0z", true},                      // second and z read again
		{"\x81\x30\x81\x7f", ffd + "0" + ffd + "\x7f", true}, // second, third and byte read again
		{"\xd6\x7f", ffd + "\x7f", true},                     // ASCII trail read again
		{"\xff", ffd, true},
	} {
		got, res := Decode("GB18030", []byte(c.in))
		if got != c.want || res.Replaced != c.replaced || !res.Known {
			t.Errorf("%q: %q %+v, want %q", c.in, got, res, c.want)
		}
	}
	if !Known("gb18030") {
		t.Error("gb18030 not known")
	}
	// gbk stays on code page 936: a four-byte sequence is not decoded.
	if got, _ := Decode("gbk", []byte("\x95\x32\x82\x36")); got == u(0x20000) {
		t.Errorf("gbk decoded a four-byte sequence: %q", got)
	}
}

func TestEUCJP(t *testing.T) {
	for _, l := range []string{"euc-jp", "cseucpkdfmtjapanese", "x-euc-jp", "EUC-JP"} {
		if !Known(l) {
			t.Errorf("%s not known", l)
		}
	}
	for _, c := range []struct {
		in, want string
		replaced bool
	}{
		{"a\xa4\xa2\xa1\xc1b", "a" + u(0x3042, 0xFF5E) + "b", false},
		{"\x8e\xb1", u(0xFF71), false},     // half-width katakana
		{"\x8f\xb0\xa1", u(0x4E02), false}, // JIS X 0212, three bytes
		{"\x8f\xa2\xaf", u(0x02D8), false},
		{"\x8f\xb0", ffd, true},            // truncated three-byte
		{"\xa4", ffd, true},                // lead at the end
		{"\xa4a", ffd + "a", true},         // ASCII read again
		{"\x8e\xe0z", ffd + "z", true},     // not katakana: both bytes
		{"\x8f\xa1\xa1z", ffd + "z", true}, // no character in JIS X 0212
		{"\x80\xff", ffd + ffd, true},
	} {
		got, res := Decode("euc-jp", []byte(c.in))
		if got != c.want || res.Replaced != c.replaced || !res.Known {
			t.Errorf("%q: %q %+v, want %q", c.in, got, res, c.want)
		}
	}
}

func TestISO2022JP(t *testing.T) {
	for _, l := range []string{"iso-2022-jp", "csiso2022jp", "ISO-2022-JP"} {
		if !Known(l) {
			t.Errorf("%s not known", l)
		}
	}
	for _, c := range []struct {
		in, want string
		replaced bool
	}{
		{"a\x1b$B$\"$$\x1b(Bb", "a" + u(0x3042, 0x3044) + "b", false},
		{"\x1b$@$\"\x1b(B", u(0x3042), false},
		{"\x1b(J\\~a\x1b(B\\~", u(0xA5, 0x203E) + "a\\~", false}, // JIS-Roman, then ASCII
		{"\x1b(I1\x1b(B", u(0xFF71), false},
		{"\x1b$B$\"\nx", u(0x3042) + ffd + ffd, true}, // not a pair: both errors
		{"\x1b$B$", ffd, true},                        // unpaired byte at the end
		{"a\x1b", "a" + ffd, true},                    // truncated escape
		{"a\x1b$", "a" + ffd + "$", true},             // truncated escape, $ read again
		{"a\x1b(Zx", "a" + ffd + "(Zx", true},         // unknown escape
		{"\x1b$B\x1b(Ba", ffd + "a", true},            // two escapes with nothing between
		{"a\x80b\x0ec", "a" + ffd + "b" + ffd + "c", true},
		{"\x1b(I\x60", ffd, true},
	} {
		got, res := Decode("iso-2022-jp", []byte(c.in))
		if got != c.want || res.Replaced != c.replaced || !res.Known {
			t.Errorf("%q: %q %+v, want %q", c.in, got, res, c.want)
		}
	}
}

// FuzzDecodeCJK checks that the GB18030, EUC-JP and ISO-2022-JP decoders
// give valid UTF-8, never more than three bytes of output per byte of
// input, and no panic.
func FuzzDecodeCJK(f *testing.F) {
	for _, s := range []string{"\x95\x32\x82\x36\x81\x30", "\x8f\xb0\xa1\x8e\xb1\xa4", "\x1b$B$\"\x1b(J\\\x1b(I1\x1b", "\x84\x31\xa5\x30"} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, s string) {
		for _, l := range []string{"gb18030", "euc-jp", "iso-2022-jp"} {
			got, _ := Decode(l, []byte(s))
			if !utf8.ValidString(got) || len(got) > 3*len(s) {
				t.Fatalf("%s %q: %q", l, s, got)
			}
		}
	})
}
