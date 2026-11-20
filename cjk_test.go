package gofastemail_test

import (
	"bytes"
	"context"
	"encoding/base64"
	"testing"

	"github.com/AOShei/go-fast-email"
	"github.com/AOShei/go-fast-email/internal/msgtest"
)

// u returns the characters of code points cps, so that expected text is
// written without non-ASCII characters in the source.
func u(cps ...rune) string { return string(cps) }

// cjkCases are text in GB18030, EUC-JP and ISO-2022-JP, with the code
// page a .msg file gives each, and what each decodes to.
var cjkCases = []struct {
	label    string
	codepage int32
	in, want string
}{
	// Two-byte, four-byte (U+00A5) and supplementary (U+20000).
	{"gb18030", 54936, "\xd6\xd0\xce\xc4\x81\x30\x84\x36\x95\x32\x82\x36", u(0x4E2D, 0x6587, 0xA5, 0x20000)},
	// JIS X 0208, half-width katakana and JIS X 0212.
	{"euc-jp", 51932, "\xa4\xa2\x8e\xb1\x8f\xb0\xa1", u(0x3042, 0xFF71, 0x4E02)},
	// JIS X 0208, JIS-Roman and half-width katakana, back to ASCII.
	{"iso-2022-jp", 50220, "\x1b$B$\"$$\x1b(J\\\x1b(I1\x1b(Bx", u(0x3042, 0x3044, 0xA5, 0xFF71) + "x"},
}

func TestCJKEncodedWords(t *testing.T) {
	for _, c := range cjkCases {
		w := "=?" + c.label + "?B?" + base64.StdEncoding.EncodeToString([]byte(c.in)) + "?="
		// Each encoded word starts in ASCII, so a second word decodes
		// the same as the first.
		doc := extract(t, "From: a@example.com\nSubject: "+w+" "+w+"\n\nBody.\n", nil)
		p := &doc.Parts[0]
		if p.Blocks[0].Text != c.want+c.want || len(unmapped(p)) != 0 {
			t.Errorf("%s: blocks %q, warnings %q", c.label, texts(p), unmapped(p))
		}
	}
}

func TestCJKBody(t *testing.T) {
	for _, c := range cjkCases {
		doc := extract(t, "From: a@example.com\nContent-Type: text/plain; charset="+c.label+"\n\n"+c.in+"\n", nil)
		p := &doc.Parts[0]
		got := texts(p)
		if got[len(got)-1] != "paragraph: "+c.want || len(unmapped(p)) != 0 {
			t.Errorf("%s: blocks %q, warnings %q", c.label, got, unmapped(p))
		}
	}
}

func TestCJKMsgCodepage(t *testing.T) {
	for _, c := range cjkCases {
		data := msgtest.Build(msgtest.Msg{Props: []msgtest.Prop{
			{ID: 0x0037, Type: msgtest.TypeString8, Value: "S " + c.in},
			{ID: 0x5D01, Type: msgtest.TypeString8, Value: "a@example.com"},
			{ID: 0x1000, Type: msgtest.TypeString8, Value: "Body " + c.in},
			{ID: 0x3FFD, Value: c.codepage},
		}})
		doc, err := gofastemail.Extract(context.Background(), bytes.NewReader(data), int64(len(data)), nil)
		if err != nil {
			t.Fatal(err)
		}
		checkDoc(t, doc)
		p := &doc.Parts[0]
		got := texts(p)
		if got[0] != "heading: S "+c.want || got[len(got)-1] != "paragraph: Body "+c.want || len(unmapped(p)) != 0 {
			t.Errorf("%d: blocks %q, warnings %q", c.codepage, got, unmapped(p))
		}
	}
}
