package rfc822

import (
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
		if got := DecodeText([]byte(c.in), report); got != c.want {
			t.Errorf("DecodeText(%q) = %q, want %q", c.in, got, c.want)
		}
	}
	reported = nil
	if got := DecodeText([]byte("=?koi8-r?q?=C1?="), report); got != "�" || len(reported) != 1 || reported[0] != "koi8-r" {
		t.Errorf("unknown charset: %q, reported %q", got, reported)
	}
}

func TestParams(t *testing.T) {
	v, p := Params([]byte(`Text/Plain; charset="utf-8"; format=flowed;; name="a \"b\".txt"`), nil)
	if v != "text/plain" || p["charset"] != "utf-8" || p["format"] != "flowed" || p["name"] != `a "b".txt` {
		t.Fatalf("got %q %q", v, p)
	}
	// RFC 2231: continuations and the extended form.
	_, p = Params([]byte(`attachment; filename*0*=utf-8''r%C3%A9sum; filename*1="e.pdf"; filename="fallback.pdf"`), nil)
	if p["filename"] != "résume.pdf" {
		t.Fatalf("2231: %q", p)
	}
	_, p = Params([]byte(`attachment; filename*=iso-8859-1'fr'caf%E9.txt`), nil)
	if p["filename"] != "café.txt" {
		t.Fatalf("2231 latin1: %q", p)
	}
	_, p = Params([]byte(`attachment; filename="=?utf-8?B?w6kudHh0?="`), nil)
	if p["filename"] != "é.txt" {
		t.Fatalf("encoded word in a quoted value: %q", p)
	}
}

func TestTransferDecoding(t *testing.T) {
	if got := string(Base64([]byte("aGVs\r\nbG8g!d29y\nbGQ=\ntrailing"))); got != "hello world" {
		t.Errorf("Base64 = %q", got)
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
