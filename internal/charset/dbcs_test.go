package charset

import (
	"testing"
	"unicode/utf8"
)

// needTables skips a test when the generated table of code page cp is not
// in tables/ (see tables/README.txt).
func needTables(t testing.TB, cp int) {
	t.Helper()
	if dbcsTable(cp) == nil {
		t.Skipf("tables/cp%d.txt is not generated; see internal/charset/tables/README.txt", cp)
	}
}

// The decoder on a small table of its own, so that it is tested whether
// or not the generated tables are present.
func TestDecodeDBCSSmallTable(t *testing.T) {
	tb := parseDBCS("# test\nA4A4 4E2D\nA4E5 6587\n80 20AC\nbad line\n")
	for _, c := range []struct {
		in, want string
		replaced bool
	}{
		{"\xa4\xa4\xa4\xe5", "中文", false},
		{"a\x80b", "a€b", false},
		{"x\xa4", "x�", true},        // lead byte at the end
		{"\xa4A", "�A", true},        // ASCII trail is read again
		{"\xa4\xa1z", "�z", true},    // pair with no character
		{"\xa4\xa4\xff", "中�", true}, // 0xFF is not a lead byte in 950
	} {
		got, res := decodeDBCS(tb, 950, []byte(c.in))
		if got != c.want || res.Replaced != c.replaced || !res.Known {
			t.Errorf("%q: %q %+v, want %q replaced=%v", c.in, got, res, c.want, c.replaced)
		}
	}
}

func TestDBCSLabels(t *testing.T) {
	cases := []struct {
		cp       int
		labels   []string
		in, want string
	}{
		{932, []string{"shift_jis", "sjis", "ms932", "windows-31j", "x-sjis", "csshiftjis", "ms_kanji", "Shift_JIS"}, "\x93\xfa\x96\x7b", "日本"},
		{936, []string{"gbk", "gb2312", "chinese", "csgb2312", "gb_2312-80", "iso-ir-58", "x-gbk", "cp936", "GB2312"}, "\xd6\xd0\xce\xc4", "中文"},
		{949, []string{"euc-kr", "ks_c_5601-1987", "windows-949", "cseuckr", "korean", "cp949"}, "\xc7\xd1\xb1\xb9", "한국"},
		{950, []string{"big5", "big5-hkscs", "cn-big5", "csbig5", "x-x-big5", "cp950"}, "\xa4\xa4\xa4\xe5", "中文"},
	}
	for _, c := range cases {
		t.Run(c.labels[0], func(t *testing.T) {
			needTables(t, c.cp)
			for _, l := range c.labels {
				got, res := Decode(l, []byte("a"+c.in+"b"))
				if got != "a"+c.want+"b" || !res.Known || res.Replaced || !Known(l) {
					t.Errorf("%s: %q %+v, want %q", l, got, res, c.want)
				}
			}
			// A lead byte at the end.
			got, res := Decode(c.labels[0], []byte(c.in[:3]))
			if got != c.want[:3]+"�" || !res.Replaced {
				t.Errorf("lead byte at the end: %q %+v", got, res)
			}
		})
	}
	// Not decoded.
	for _, l := range []string{"gb18030", "iso-2022-jp", "euc-jp", "koi8-r", "koi8-u"} {
		if Known(l) {
			t.Errorf("%s is known", l)
		}
	}
}

// FuzzDecodeDBCS checks that the double-byte decoders give valid UTF-8,
// never more than three bytes of output per byte of input, and no panic.
func FuzzDecodeDBCS(f *testing.F) {
	for _, s := range []string{"\x93\xfa\x96\x7b", "\xa4\xa4\xa4", "\xf0\x40\xaa\xa1\xa1\x40", "\x81\x30\x81\x30", "a\x80\xff"} {
		f.Add(s)
	}
	small := parseDBCS("A4A4 4E2D\n80 20AC\n")
	f.Fuzz(func(t *testing.T, s string) {
		for _, cp := range []int{932, 936, 949, 950} {
			tb := dbcsTable(cp)
			if tb == nil {
				tb = small // the generated table is not present
			}
			got, _ := decodeDBCS(tb, cp, []byte(s))
			if !utf8.ValidString(got) || len(got) > 3*len(s) {
				t.Fatalf("%d %q: %q", cp, s, got)
			}
		}
	})
}
