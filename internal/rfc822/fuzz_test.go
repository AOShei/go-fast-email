package rfc822

import (
	"reflect"
	"strings"
	"testing"
	"unicode/utf8"
)

// FuzzHeader runs the header, parameter and encoded-word readers on
// arbitrary bytes: they must not panic, and what they return as text must
// be valid UTF-8.
func FuzzHeader(f *testing.F) {
	f.Add([]byte("Subject: =?utf-8?q?a?= =?utf-8?b?Yg==?=\nContent-Type: text/plain; charset*0*=utf-8''a; charset*1=b\n\nbody"))
	f.Add([]byte("A: 1\n  2\nnot a header\n"))
	f.Fuzz(func(t *testing.T, data []byte) {
		h, at, p := ParseHeader(data, 4096)
		if at < 0 || at > len(data) {
			t.Fatalf("body at %d of %d", at, len(data))
		}
		// A scratch that read another header first reads it the same.
		var s Scratch
		s.ParseHeader([]byte("Other: "+strings.Repeat("x", len(data)%300)+"\n\tmore\nAnd: y\n\n"), 4096)
		sh, sat, sp := s.ParseHeader(data, 4096)
		if !reflect.DeepEqual(sh, h) || sat != at || sp != p {
			t.Fatalf("with a scratch %q %d %+v, without %q %d %+v", sh, sat, sp, h, at, p)
		}
		// Under any smaller limit there are fields exactly when the first
		// field begins below it.
		if len(sh) > 0 != (s.First() >= 0) {
			t.Fatalf("%d fields, first at %d", len(sh), s.First())
		}
		first := s.First()
		for _, limit := range []int{1, 7, 64, len(data) / 2, len(data), 4095} {
			if limit > 4096 || limit <= 0 {
				continue
			}
			small, _, _ := ParseHeader(data, limit)
			if want := first >= 0 && first < limit; len(small) > 0 != want {
				t.Fatalf("limit %d: %d fields, first at %d of the larger reading", limit, len(small), first)
			}
		}
		for _, field := range h {
			if s, _ := DecodeText(field.Value, nil); !utf8.ValidString(s) {
				t.Fatalf("DecodeText gave invalid UTF-8 %q", s)
			}
			v, p, _ := Params(field.Value, nil)
			if !utf8.ValidString(v) {
				t.Fatalf("Params value %q", v)
			}
			for k, x := range p {
				if !utf8.ValidString(k) || !utf8.ValidString(x) {
					t.Fatalf("Params %q=%q", k, x)
				}
			}
		}
	})
}

// FuzzBody runs the multipart splitter and the transfer decoders on
// arbitrary bytes.
func FuzzBody(f *testing.F) {
	f.Add([]byte("pre\n--b\nx\n--b--\npost"), "b")
	f.Add([]byte("=3D=\n=XY \r\naGVsbG8="), "")
	f.Fuzz(func(t *testing.T, data []byte, boundary string) {
		if len(Base64(data)) > len(data) || len(QuotedPrintable(data)) > len(data)+1 {
			t.Fatal("decoding grew the input")
		}
		if boundary == "" {
			return
		}
		m := Split(data, boundary, 100)
		total := len(m.Preamble) + len(m.Epilogue)
		for _, p := range m.Parts {
			total += len(p)
		}
		if total > len(data) || len(m.Parts) > 100 {
			t.Fatalf("split gave %d bytes in %d parts from %d", total, len(m.Parts), len(data))
		}
	})
}
