package gofastemail_test

import (
	"bytes"
	"context"
	"os"
	"strings"
	"testing"

	gofastcore "github.com/AOShei/go-fast-core"
	gofastemail "github.com/AOShei/go-fast-email"
)

func detectText(t *testing.T, name string, n int) string {
	t.Helper()
	b, err := os.ReadFile("internal/charset/testdata/detect/" + name)
	if err != nil {
		t.Fatal(err)
	}
	// Cut after a line end or space: a character boundary in every set.
	n = min(n, len(b))
	for n > 0 && b[n-1] >= 0x40 {
		n--
	}
	return string(b[:n])
}

func codeMessages(doc *gofastcore.Document, code string) []string {
	var out []string
	for _, w := range doc.AllWarnings() {
		if code == "" || w.Code == code {
			out = append(out, w.Code+": "+w.Message)
		}
	}
	return out
}

func extractWith(t *testing.T, e *gofastemail.Extractor, src string) *gofastcore.Document {
	t.Helper()
	doc, err := e.Extract(context.Background(), bytes.NewReader([]byte(src)), int64(len(src)), nil)
	if err != nil {
		t.Fatal(err)
	}
	checkDoc(t, doc)
	return doc
}

// TestEncodingDetected: a body that declares no character set, or
// US-ASCII, and is not UTF-8 is read in the set its bytes are certainly
// in, with text.encoding_detected naming it.
func TestEncodingDetected(t *testing.T) {
	for _, c := range []struct{ file, ctype, set, want string }{
		{"ja.cp932", "text/plain", "shift_jis", "水は地球の表面"},
		{"ru.cp1251", "text/plain; charset=us-ascii", "windows-1251", "Вода покрывает"},
		{"zh.gbk", "", "gbk", "水是地球上最常见的物质之一"},
	} {
		src := "Subject: x\r\nMIME-Version: 1.0\r\n"
		if c.ctype != "" {
			src += "Content-Type: " + c.ctype + "\r\n"
		}
		src += "\r\n" + detectText(t, c.file, 1500)
		doc := extractWith(t, &gofastemail.Extractor{}, src)
		if !strings.Contains(doc.Text(), c.want) {
			t.Errorf("%s: %.120q", c.file, doc.Text())
		}
		got := codeMessages(doc, gofastcore.WarnTextEncodingDetected)
		if len(got) != 1 || !strings.Contains(got[0], "read as "+c.set+",") || !strings.Contains(got[0], "-charset") {
			t.Errorf("%s: %q", c.file, got)
		}
		if u := codeMessages(doc, gofastcore.WarnTextUnmappedGlyphs); len(u) != 0 {
			t.Errorf("%s: %q", c.file, u)
		}
	}
}

// TestEncodingUndetermined: where no set is certain the bytes outside
// ASCII are U+FFFD, as before, and the warning says how to give the set;
// Extractor.Charset reads the text.
func TestEncodingUndetermined(t *testing.T) {
	src := "Subject: x\r\n\r\n" + detectText(t, "cs.cp1250", 1500)
	doc := extractWith(t, &gofastemail.Extractor{}, src)
	u := codeMessages(doc, gofastcore.WarnTextUnmappedGlyphs)
	if len(u) != 1 || !strings.Contains(u[0], "no character set is certain") || !strings.Contains(doc.Text(), "\uFFFD") {
		t.Errorf("%q %v", u, codeMessages(doc, gofastcore.WarnTextEncodingDetected))
	}
	doc = extractWith(t, &gofastemail.Extractor{Charset: "windows-1250"}, src)
	if !strings.Contains(doc.Text(), "Českou republikou protékají řeky") || len(codeMessages(doc, "")) != 0 {
		t.Errorf("with Charset: %.200q %v", doc.Text(), codeMessages(doc, ""))
	}
}

// TestEncodingHeader: header text outside encoded words that is not UTF-8
// is read like a body; valid UTF-8 (RFC 6532) is not judged.
func TestEncodingHeader(t *testing.T) {
	subject, _, _ := strings.Cut(detectText(t, "ja.cp932", 4000), "\n") // the first paragraph
	doc := extractWith(t, &gofastemail.Extractor{}, "Subject: "+subject+"\r\n\r\nbody\r\n")
	if !strings.HasPrefix(doc.Metadata.Title, "水は地球") || len(codeMessages(doc, gofastcore.WarnTextEncodingDetected)) != 1 {
		t.Errorf("%q %v", doc.Metadata.Title, codeMessages(doc, ""))
	}
	doc = extractWith(t, &gofastemail.Extractor{}, "Subject: caf\xc3\xa9\r\n\r\nbody\r\n")
	if doc.Metadata.Title != "café" || len(codeMessages(doc, "")) != 0 {
		t.Errorf("%q %v", doc.Metadata.Title, codeMessages(doc, ""))
	}
}
