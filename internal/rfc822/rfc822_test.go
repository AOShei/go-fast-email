package rfc822

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"strings"
	"testing"

	"github.com/AOShei/go-fast-email/internal/charset"
)

func TestParseHeader(t *testing.T) {
	msg := "From sender@x Tue Jan  1 00:00:00 2026\nSubject: a\r\n  folded\r\nX-Two : 2\r\n\r\nbody"
	h, at, p := ParseHeader([]byte(msg), 0)
	if len(h) != 2 || string(h[0].Value) != "a  folded" || h[1].Name != "X-Two" || msg[at:] != "body" || p != (Problems{}) {
		t.Fatalf("got %q at %d, %+v", h, at, p)
	}
	h, at, p = ParseHeader([]byte("A: 1\nnot a header\nB: 2\n"), 0)
	if len(h) != 1 || !p.Malformed || !strings.HasPrefix("A: 1\nnot a header\nB: 2\n"[at:], "not a header") {
		t.Fatalf("malformed: %q at %d %+v", h, at, p)
	}
	_, at, p = ParseHeader([]byte("A: 1234567890\nB: 2\n\nbody"), 8)
	if !p.Truncated || at != 8 {
		t.Fatalf("truncated: at %d %+v", at, p)
	}
}

func TestDecodeText(t *testing.T) {
	var reported []string
	report := func(label string, res charset.Result) { reported = append(reported, label) }
	for _, c := range []struct{ in, want string }{
		{"plain", "plain"},
		{"=?utf-8?q?caf=C3=A9_au_lait?=", "café au lait"},
		{"=?UTF-8?B?Y2Fmw6k=?= =?utf-8?b?IG5vaXI=?=", "café noir"},
		{"Re: =?iso-8859-1?q?r=E9sum=E9?= now", "Re: résumé now"},
		{"=?utf-8?q?a?=  b", "a  b"},
		{"=?broken", "=?broken"},
		{"=?utf-8?x?abc?=", "=?utf-8?x?abc?="},
		{"=?utf-8*en?q?x?=", "x"},
	} {
		if got, _ := DecodeText([]byte(c.in), &Decoding{Report: report}); got != c.want {
			t.Errorf("DecodeText(%q) = %q, want %q", c.in, got, c.want)
		}
	}
	reported = nil
	if got, _ := DecodeText([]byte("=?koi8-r?q?=C1?="), &Decoding{Report: report}); got != "�" || len(reported) != 1 || reported[0] != "koi8-r" {
		t.Errorf("unknown charset: %q, reported %q", got, reported)
	}
}

func TestParams(t *testing.T) {
	v, p, _ := Params([]byte(`Text/Plain; charset="utf-8"; format=flowed;; name="a \"b\".txt"`), nil)
	if v != "text/plain" || p["charset"] != "utf-8" || p["format"] != "flowed" || p["name"] != `a "b".txt` {
		t.Fatalf("got %q %q", v, p)
	}
	// RFC 2231: continuations and the extended form.
	_, p, _ = Params([]byte(`attachment; filename*0*=utf-8''r%C3%A9sum; filename*1="e.pdf"; filename="fallback.pdf"`), nil)
	if p["filename"] != "résume.pdf" {
		t.Fatalf("2231: %q", p)
	}
	_, p, _ = Params([]byte(`attachment; filename*=iso-8859-1'fr'caf%E9.txt`), nil)
	if p["filename"] != "café.txt" {
		t.Fatalf("2231 latin1: %q", p)
	}
	_, p, _ = Params([]byte(`attachment; filename="=?utf-8?B?w6kudHh0?="`), nil)
	if p["filename"] != "é.txt" {
		t.Fatalf("encoded word in a quoted value: %q", p)
	}
}

// TestParamsPastBound: past MaxParams parameters the ones the reader uses
// are still read and the others are not; RFC 2231 pieces past MaxPiece
// are counted.
func TestParamsPastBound(t *testing.T) {
	var b strings.Builder
	b.WriteString("multipart/mixed")
	for i := range 100 {
		fmt.Fprintf(&b, "; x%d=%d", i, i)
	}
	b.WriteString(`; boundary="b1"; charset=utf-8; filename*0="a"; filename*1="b.pdf"; filename*1000="c"`)
	v, p, c := Params([]byte(b.String()), nil)
	if v != "multipart/mixed" || p["boundary"] != "b1" || p["charset"] != "utf-8" || p["filename"] != "ab.pdf" {
		t.Fatalf("got %q %q", v, p)
	}
	if _, ok := p["x70"]; ok || p["x10"] != "10" {
		t.Fatalf("x70 or x10: %q", p)
	}
	if c != (Cuts{Pieces: 1}) {
		t.Fatalf("cuts %+v", c)
	}
	// Under the bound nothing is counted.
	if _, _, c := Params([]byte(`text/plain; a=1; name*5="x"; name*abc="y"`), nil); c != (Cuts{}) {
		t.Fatalf("cuts under the bound: %+v", c)
	}
}

// TestDecodeTextBounds: encoded words kept from being decoded by MaxWord
// or MaxLabel are counted, others that are not words are not.
func TestDecodeTextBounds(t *testing.T) {
	long := "=?utf-8?q?" + strings.Repeat("a", MaxWord+10) + "?="
	if got, c := DecodeText([]byte(long), nil); got != long || c.LongWords != 1 {
		t.Fatalf("long word: %d bytes, %+v", len(got), c)
	}
	label := "=?" + strings.Repeat("x", MaxLabel+1) + "?q?=E9?="
	if got, c := DecodeText([]byte(label), nil); got != label || c.LongLabels != 1 {
		t.Fatalf("long label: %q, %+v", got, c)
	}
	for _, s := range []string{"=?utf-8?q?" + strings.Repeat("a ", MaxWord), "=?broken", "=?utf-8?x?abc?=", "a =?utf-8?q?ok?= b"} {
		if _, c := DecodeText([]byte(s), nil); c != (Cuts{}) {
			t.Fatalf("%.40q: %+v", s, c)
		}
	}
	// A long word in a quoted parameter value is counted through Params.
	if _, _, c := Params([]byte(`attachment; filename="`+long+`"`), nil); c.LongWords != 1 {
		t.Fatalf("Params: %+v", c)
	}
}

// TestWordInsideCutWord: a word that begins within the bound of a cut
// word, at a "=?" before the last one there, is decoded; the cut word is
// counted once.
func TestWordInsideCutWord(t *testing.T) {
	inner := "=?utf-8?q?" + strings.Repeat("C", 200) + "=?B" + strings.Repeat("D", 3847) + "?="
	in := "=?utf-8?q?" + strings.Repeat("A", 90) + inner
	got, c := DecodeText([]byte(in), nil)
	want := "=?utf-8?q?" + strings.Repeat("A", 90) + strings.Repeat("C", 200) + "=?B" + strings.Repeat("D", 3847)
	if got != want || c != (Cuts{LongWords: 1}) {
		t.Fatalf("%d bytes back (want %d), cuts %+v", len(got), len(want), c)
	}
}

// TestNotAWordSkip: where a "=?" is not a word because of whitespace, the
// search goes on from the whitespace, where no word can begin before,
// and not 2 bytes on.
func TestNotAWordSkip(t *testing.T) {
	for _, c := range []struct{ in, rest string }{
		{"=?a?q?xx=?yy zz?=", " zz?="},
		{"=?a?q?" + strings.Repeat("x", 3000) + " z", " z"},
		{"=?a=?q?xx yy", ""},         // a word may begin in "=?a=?q?"
		{"=?a?q?x=?l\nl?q?ok?=", ""}, // a line break may be in a label
	} {
		if w := encodedWord(c.in, nil, 0, 0); w.ok || w.cut != cutNone || w.rest != c.rest {
			t.Errorf("%.20q: rest %q, want %q", c.in, w.rest, c.rest)
		}
	}
	if got, _ := DecodeText([]byte("=?a?q?x=?l\nl?q?ok?="), nil); got != "=?a?q?xok" {
		t.Errorf("label with a line break: %q", got)
	}
	// A megabyte of word starts with a space every 4,000 bytes is read
	// as written.
	var b strings.Builder
	for b.Len() < 1<<20 {
		b.WriteString(strings.Repeat("=?a?q?x", 570))
		b.WriteByte(' ')
	}
	if got, c := DecodeText([]byte(b.String()), nil); got != b.String() || c != (Cuts{}) {
		t.Errorf("%d bytes back, cuts %+v", len(got), c)
	}
}

func TestTransferDecoding(t *testing.T) {
	if got := string(Base64([]byte("aGVs\r\nbG8g!d29y\nbGQ=\ntrailing"))); got != "hello world" {
		t.Errorf("Base64 = %q", got)
	}
	// Lengths around the size of the decoder's buffer, wrapped at 76.
	for _, n := range []int{0, 1, 2, 3, 3071, 3072, 3073, 10000} {
		raw := make([]byte, n)
		for i := range raw {
			raw[i] = byte(i*7 + i/251)
		}
		enc := base64.StdEncoding.EncodeToString(raw)
		var wrapped strings.Builder
		for len(enc) > 76 {
			wrapped.WriteString(enc[:76] + "\r\n")
			enc = enc[76:]
		}
		wrapped.WriteString(enc)
		if got := Base64([]byte(wrapped.String())); !bytes.Equal(got, raw) {
			t.Errorf("Base64 of %d bytes: got %d bytes, not the same", n, len(got))
		}
	}
	if got := string(QuotedPrintable([]byte("caf=C3=A9 =\r\nau lait  \r\n=XYend=3d"))); got != "caf\xc3\xa9 au lait\n=XYend=" {
		t.Errorf("QuotedPrintable = %q", got)
	}
}

func TestSplit(t *testing.T) {
	body := "preamble\r\n--b\r\nContent-Type: text/plain\r\n\r\none\r\n--b \r\n\r\ntwo\n--bb not a boundary\n--b--\r\nepilogue"
	m := Split([]byte(body), "b", 10)
	if string(m.Preamble) != "preamble" || len(m.Parts) != 2 || !m.Closed || string(m.Epilogue) != "epilogue" {
		t.Fatalf("got %+v", m)
	}
	if string(m.Parts[0]) != "Content-Type: text/plain\r\n\r\none" || string(m.Parts[1]) != "\r\ntwo\n--bb not a boundary" {
		t.Fatalf("parts %q", m.Parts)
	}
	m = Split([]byte("--b\nx\n--b\ny"), "b", 1)
	if len(m.Parts) != 1 || !m.More || m.Closed {
		t.Fatalf("limit: %+v", m)
	}
	m = Split([]byte("--b\nx\n--b\ny"), "b", 10)
	if len(m.Parts) != 2 || m.Closed || string(m.Parts[1]) != "y" {
		t.Fatalf("unclosed: %+v", m)
	}
}
