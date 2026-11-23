package charset

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf8"
)

// detectWant is the label each file of testdata/detect (gen/gen_detect.py,
// the same set as go-fast-html's) is recognised as, by its codec; a codec
// not listed must not be decided, unless the set chosen gives the same
// text (a short Czech text whose letters windows-1252 has at the same
// bytes).
var detectWant = map[string]string{
	"cp932": "shift_jis", "gbk": "gbk", "cp950": "big5", "cp949": "euc-kr", "cp1251": "windows-1251",
	"cp1252": "windows-1252", "utf_16_le": "utf-16le", "utf_16_be": "utf-16be", "utf8bad": "utf-8",
}

var decodable = map[string]string{"cp1250": "windows-1250", "cp1253": "windows-1253", "cp1254": "windows-1254",
	"cp1255": "windows-1255", "cp1258": "windows-1258"}

func TestDetect(t *testing.T) {
	files, err := filepath.Glob("testdata/detect/*")
	if err != nil || len(files) < 30 {
		t.Fatalf("testdata/detect: %d files, %v", len(files), err)
	}
	for _, f := range files {
		b, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		codec := f[strings.LastIndexByte(f, '.')+1:]
		want, decided := detectWant[codec]
		for _, s := range [][]byte{b, b[:min(len(b), 600)]} {
			got, res := Decode("", s)
			switch {
			case decided && res.Detected != want:
				t.Errorf("%s (%d bytes): %+v, want %s", f, len(s), res, want)
			case !decided && res.Detected != "":
				te, ok := decodable[codec]
				if !ok {
					t.Errorf("%s (%d bytes): decided %s, want not decided", f, len(s), res.Detected)
					continue
				}
				if a, _ := Decode(te, s); a != got {
					t.Errorf("%s (%d bytes): decided %s, which gives other text; want not decided", f, len(s), res.Detected)
				}
			case !decided && !res.Undetermined && !strings.HasPrefix(codec, "euc_jp"):
				t.Errorf("%s (%d bytes): %+v, want Undetermined", f, len(s), res)
			}
		}
	}
}

// TestDetectDeclared: text that declares a set, or is valid UTF-8, is not
// judged.
func TestDetectDeclared(t *testing.T) {
	for _, c := range [][2]string{{"windows-1250", "caf\xe9"}, {"utf-8", "caf\xe9"}, {"", "caf\xc3\xa9"}, {"us-ascii", "plain"}} {
		if _, res := Decode(c[0], []byte(c[1])); res.Detected != "" || res.Undetermined {
			t.Errorf("%q %q: %+v", c[0], c[1], res)
		}
	}
}

// BenchmarkDetect: the time detection adds to text that declares no set
// and is not UTF-8, at most detectSample bytes read.
func BenchmarkDetect(b *testing.B) {
	for _, f := range []string{"fr.cp1252", "ja.cp932", "zh.gbk", "ko.cp949", "ru.cp1251", "cs.cp1250"} {
		data, err := os.ReadFile("testdata/detect/" + f)
		if err != nil {
			b.Fatal(err)
		}
		b.Run(f, func(b *testing.B) {
			b.SetBytes(int64(len(data)))
			for range b.N {
				Detect(data)
			}
		})
	}
}

// FuzzDetect: on any bytes, Decode of text that declares no set gives
// valid UTF-8, and Detect's answer decodes.
func FuzzDetect(f *testing.F) {
	for _, s := range []string{"caf\xe9", "\x82\xa0\x82\xa2", "\xc4\xe3\xba\xc3", "h\x00i\x00", "\xff"} {
		f.Add([]byte(s))
	}
	f.Fuzz(func(t *testing.T, b []byte) {
		s, res := Decode("", b)
		if !utf8.ValidString(s) {
			t.Fatalf("not UTF-8: %+v", res)
		}
		if res.Detected != "" && res.Undetermined {
			t.Fatalf("%+v", res)
		}
	})
}
