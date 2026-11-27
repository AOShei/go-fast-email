package gofastemail_test

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/AOShei/go-fast-core"
	"github.com/AOShei/go-fast-core/rfc822"
	"github.com/AOShei/go-fast-email"
	"github.com/AOShei/go-fast-email/internal/msgtest"
)

// needDBCS skips a test when go-fast-core's charset does not decode the
// double-byte code pages (it always does: the tables are embedded).
func needDBCS(t *testing.T) {
	t.Helper()
	for _, l := range []string{"shift_jis", "gbk", "euc-kr", "big5"} {
		if !rfc822.KnownCharset(l) {
			t.Skipf("%s is not decoded", l)
		}
	}
}

func unmapped(p *gofastcore.Part) []string {
	var msgs []string
	for _, w := range p.Warnings {
		if w.Code == gofastcore.WarnTextUnmappedGlyphs {
			msgs = append(msgs, w.Message)
		}
	}
	return msgs
}

func TestDBCSEncodedWords(t *testing.T) {
	needDBCS(t)
	msg := "From: a@example.com\nSubject: =?big5?B?pKSk5Q==?= x =?shift_jis?Q?=93=FA=96=7B?=\n\nBody.\n"
	doc := extract(t, msg, nil)
	p := &doc.Parts[0]
	if p.Blocks[0].Text != "中文 x 日本" || len(unmapped(p)) != 0 {
		t.Errorf("blocks %q, warnings %q", texts(p), unmapped(p))
	}
}

func TestDBCSBody(t *testing.T) {
	needDBCS(t)
	msg := "From: a@example.com\nContent-Type: text/plain; charset=GBK\n\n\xd6\xd0\xce\xc4\n"
	doc := extract(t, msg, nil)
	p := &doc.Parts[0]
	got := texts(p)
	if got[len(got)-1] != "paragraph: 中文" || len(unmapped(p)) != 0 {
		t.Errorf("blocks %q, warnings %q", got, unmapped(p))
	}
}

// A lead byte at the end of an encoded word is U+FFFD, with a warning.
func TestDBCSLeadByteAtEnd(t *testing.T) {
	needDBCS(t)
	msg := "From: a@example.com\nSubject: =?big5?B?pKSk?=\n\nBody.\n"
	doc := extract(t, msg, nil)
	p := &doc.Parts[0]
	w := unmapped(p)
	if p.Blocks[0].Text != "中�" || len(w) != 1 || !strings.Contains(w[0], `"big5"`) {
		t.Errorf("blocks %q, warnings %q", texts(p), w)
	}
}

func TestMsgCodepage950(t *testing.T) {
	needDBCS(t)
	data := msgtest.Build(msgtest.Msg{Props: []msgtest.Prop{
		{ID: 0x0037, Type: msgtest.TypeString8, Value: "S \xa4\xa4\xa4\xe5"},
		{ID: 0x0C1A, Type: msgtest.TypeString8, Value: "\xa4\xe5\xa4\xa4"},
		{ID: 0x5D01, Type: msgtest.TypeString8, Value: "a@example.com"},
		{ID: 0x1000, Type: msgtest.TypeString8, Value: "Body \xa4\xa4"},
		{ID: 0x3FFD, Value: int32(950)},
	}})
	doc, err := gofastemail.Extract(context.Background(), bytes.NewReader(data), int64(len(data)), nil)
	if err != nil {
		t.Fatal(err)
	}
	checkDoc(t, doc)
	p := &doc.Parts[0]
	got := texts(p)
	if got[0] != "heading: S 中文" {
		t.Errorf("subject %q", got[0])
	}
	if len(doc.Metadata.Authors) != 1 || !strings.HasPrefix(doc.Metadata.Authors[0], "文中") {
		t.Errorf("authors %q", doc.Metadata.Authors)
	}
	if got[len(got)-1] != "paragraph: Body 中" || len(unmapped(p)) != 0 {
		t.Errorf("blocks %q, warnings %q", got, unmapped(p))
	}
}

// The dev corpus file of code page 950 has its sender names decoded.
func TestCorpusChineseMsg(t *testing.T) {
	needDBCS(t)
	path := filepath.Join(corpusRoot(), "dev", "msg", "tika-testMSG_chinese.msg")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Skip("corpus file not present")
	}
	doc, err := gofastemail.Extract(context.Background(), bytes.NewReader(data), int64(len(data)), nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(doc.Metadata.Authors) == 0 {
		t.Fatal("no authors")
	}
	for _, a := range doc.Metadata.Authors {
		if strings.ContainsRune(a, '�') {
			t.Errorf("author %q", a)
		}
	}
	if strings.ContainsRune(doc.Metadata.Title, '�') {
		t.Errorf("title %q", doc.Metadata.Title)
	}
}
