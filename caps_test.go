package gofastemail_test

import (
	"bytes"
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/AOShei/go-fast-core"
	"github.com/AOShei/go-fast-email"
	"github.com/AOShei/go-fast-email/internal/msgtest"
)

// The tests here check that a fixed bound of this library that cuts or
// leaves out content says so, once per part, and that a warning that can
// be given for many values is given once per part.

// warnings returns the warnings of the part with the given code.
func warnings(p *gofastcore.Part, code string) []gofastcore.Warning {
	var out []gofastcore.Warning
	for _, w := range p.Warnings {
		if w.Code == code {
			out = append(out, w)
		}
	}
	return out
}

// one checks that the part has exactly one warning with the code and
// that its message holds every given text.
func one(t *testing.T, p *gofastcore.Part, code string, holds ...string) gofastcore.Warning {
	t.Helper()
	ws := warnings(p, code)
	if len(ws) != 1 {
		t.Fatalf("%d warnings %s, want 1: %+v", len(ws), code, p.Warnings)
	}
	for _, h := range holds {
		if !strings.Contains(ws[0].Message, h) {
			t.Errorf("%s message %q does not hold %q", code, ws[0].Message, h)
		}
	}
	return ws[0]
}

// A file name longer than the bound is cut in the middle, keeping its
// extension, with one limit.exceeded for the part.
func TestLongFileName(t *testing.T) {
	long := strings.Repeat("n", 5000) + ".pdf"
	msg := "From: a@example.com\nSubject: names\nContent-Type: multipart/mixed; boundary=b\n\n--b\n\nText.\n"
	for i := range 2 {
		msg += fmt.Sprintf("--b\nContent-Type: application/pdf\nContent-Disposition: attachment; filename=\"%d%s\"\n\n%%PDF\n", i, long)
	}
	msg += "--b\nContent-Type: application/pdf\nContent-Disposition: attachment; filename=\"" + strings.Repeat("m", 300) + ".pdf\"\n\n%PDF\n--b--\n"
	var got []seen
	doc := extract(t, msg, &gofastcore.Options{Child: dispatcher(&got)})
	if len(got) != 3 {
		t.Fatalf("%d children", len(got))
	}
	for _, g := range got[:2] {
		if len(g.name) > 4096 || !strings.HasSuffix(g.name, "n\u2026.pdf") {
			t.Errorf("name of %d bytes ends %q", len(g.name), g.name[len(g.name)-12:])
		}
	}
	if got[2].name != strings.Repeat("m", 300)+".pdf" {
		t.Errorf("a name of 304 bytes is %d bytes", len(got[2].name))
	}
	one(t, &doc.Parts[0], gofastcore.WarnLimitExceeded, "longer than 4096 bytes", "(2 names)")

	// In an Outlook message too.
	data := msgtest.Build(msgtest.Msg{
		Props: []msgtest.Prop{{ID: 0x0037, Value: "names"}},
		Attachments: []msgtest.Attachment{{Props: []msgtest.Prop{
			{ID: 0x3707, Value: long}, {ID: 0x3705, Value: int32(1)}, {ID: 0x3701, Value: []byte("%PDF")},
		}}},
	})
	got = nil
	doc = extractBytes(t, data, &gofastcore.Options{Child: dispatcher(&got)})
	if len(got) != 1 || len(got[0].name) > 4096 || !strings.HasSuffix(got[0].name, "\u2026.pdf") {
		t.Fatalf("msg children %d", len(got))
	}
	one(t, &doc.Parts[0], gofastcore.WarnLimitExceeded, "(1 name)")
}

func extractBytes(t *testing.T, data []byte, opts *gofastcore.Options) *gofastcore.Document {
	t.Helper()
	doc, err := gofastemail.Extract(context.Background(), bytes.NewReader(data), int64(len(data)), opts)
	if err != nil {
		t.Fatal(err)
	}
	checkDoc(t, doc)
	return doc
}

// Past 64 parameters the ones this library reads are still read; the
// others are counted in one warning.
func TestManyParameters(t *testing.T) {
	var junk strings.Builder
	for i := range 70 {
		fmt.Fprintf(&junk, "; x%d=%d", i, i)
	}
	msg := "From: a@example.com\nSubject: params\nContent-Type: multipart/mixed" + junk.String() + "; boundary=\"b\"\n\n" +
		"--b\nContent-Type: text/plain" + junk.String() + "; charset=iso-8859-1\n\ncaf\xe9\n" +
		"--b\nContent-Type: application/pdf\nContent-Disposition: attachment" + junk.String() + "; filename=\"a.pdf\"; name*1000=\"x\"\n\n%PDF\n" +
		"--b--\n"
	var got []seen
	doc := extract(t, msg, &gofastcore.Options{Child: dispatcher(&got)})
	p := &doc.Parts[0]
	equal(t, "blocks", texts(p), []string{"heading: params", "paragraph: From: a@example.com\nAttachments: a.pdf", "paragraph: café"})
	if p.Has(gofastcore.WarnInputRepaired) {
		t.Errorf("repaired: %+v", p.Warnings)
	}
	ws := warnings(p, gofastcore.WarnLimitExceeded)
	if len(ws) != 2 || !strings.Contains(ws[0].Message, "more than 64 parameters") || !strings.Contains(ws[0].Message, "(18 parameters not read)") ||
		!strings.Contains(ws[1].Message, "above 999") || !strings.Contains(ws[1].Message, "(1 piece)") {
		t.Fatalf("warnings %+v", p.Warnings)
	}
}

// Encoded words left undecoded by a bound are reported.
func TestLongEncodedWords(t *testing.T) {
	word := "=?utf-8?q?" + strings.Repeat("a", 5000) + "?="
	label := "=?" + strings.Repeat("x", 120) + "?q?=E9?="
	msg := "From: a@example.com\nSubject: " + word + " " + word + "\nX-Label: " + label + "\n\nBody.\n"
	doc := extract(t, msg, nil)
	p := &doc.Parts[0]
	ws := warnings(p, gofastcore.WarnLimitExceeded)
	if len(ws) != 2 || !strings.Contains(ws[0].Message, "within 4096 bytes") || !strings.Contains(ws[0].Message, "(2 words)") ||
		!strings.Contains(ws[1].Message, "longer than 100 bytes") || !strings.Contains(ws[1].Message, "(1 word)") {
		t.Fatalf("warnings %+v", p.Warnings)
	}
	if !strings.Contains(doc.Metadata.Title, word) {
		t.Errorf("the long word is not kept as written")
	}
}

// A mailbox "From " line cut in the headers note is reported.
func TestLongEnvelope(t *testing.T) {
	box := "From " + strings.Repeat("e", 2000) + " Tue Mar  3 10:00:00 2026\nFrom: a@example.com\nSubject: one\n\nBody.\n\n" +
		"From b@example.com Tue Mar  3 11:00:00 2026\nFrom: b@example.com\nSubject: two\n\nBody.\n"
	doc := extract(t, box, nil)
	if len(doc.Parts) != 2 {
		t.Fatalf("%d parts", len(doc.Parts))
	}
	one(t, &doc.Parts[0], gofastcore.WarnLimitExceeded, "(2025 bytes)", "cut to 1024 bytes")
	if len(doc.Parts[1].Warnings) != 0 {
		t.Errorf("second message: %+v", doc.Parts[1].Warnings)
	}
}

// An image description cut to its bound is reported.
func TestLongImageDescription(t *testing.T) {
	msg := "From: a@example.com\nSubject: img\nContent-Type: multipart/mixed; boundary=b\n\n" +
		"--b\nContent-Type: image/gif\nContent-Description: " + strings.Repeat("d", 5000) + "\n\nGIF89a\n" +
		"--b\nContent-Type: image/gif\nContent-Description: short\n\nGIF89a\n--b--\n"
	doc := extract(t, msg, nil)
	p := &doc.Parts[0]
	one(t, p, gofastcore.WarnLimitExceeded, "longer than 4096 bytes", "(1 description)")
}

// Where the text budget ends at the list of attachments, the list is
// left out with limit.exceeded, as wherever else the budget ends.
func TestAttachmentsLineBudget(t *testing.T) {
	msg := "From: a@example.com\nSubject: s\nContent-Type: multipart/mixed; boundary=b\n\n--b\n\nText.\n" +
		"--b\nContent-Type: application/pdf\nContent-Disposition: attachment; filename=\"a-long-attachment-name.pdf\"\n\n%PDF\n--b--\n"
	cut := 0
	for budget := int64(1); budget < 2000; budget++ {
		doc := extract(t, msg, &gofastcore.Options{Limits: gofastcore.Limits{MaxTextBytes: budget}})
		p := &doc.Parts[0]
		listed := false
		for _, b := range p.Blocks {
			listed = listed || strings.Contains(b.Text, "Attachments:")
		}
		if !listed && !p.Has(gofastcore.WarnLimitExceeded) {
			t.Fatalf("budget %d: no list of attachments and no warning: %q", budget, texts(p))
		}
		if !listed && len(p.Blocks) == 3 {
			cut++ // every block written, only the list left out
		}
	}
	if cut == 0 {
		t.Fatal("no budget ends at the list of attachments")
	}
}

// The fixed-length properties past the 65536th entry of a property
// stream are reported.
func TestMsgManyPropertyEntries(t *testing.T) {
	props := []msgtest.Prop{{ID: 0x0037, Value: "entries"}}
	for i := range 65600 {
		props = append(props, msgtest.Prop{ID: uint16(0x8000 + i%0x7000), Value: int32(i)})
	}
	data := msgtest.Build(msgtest.Msg{Props: props})
	doc := extractBytes(t, data, nil)
	one(t, &doc.Parts[0], gofastcore.WarnLimitExceeded, "more than 65536 entries", "(65 entries)")
}

// The transport headers of an Outlook message cut by a bound are
// reported as those of a message are.
func TestMsgTransportHeaderLimits(t *testing.T) {
	var h strings.Builder
	for i := range 10_010 {
		fmt.Fprintf(&h, "X-H%d: %d\r\n", i, i)
	}
	data := msgtest.Build(msgtest.Msg{Props: []msgtest.Prop{{ID: 0x0037, Value: "transport"}, {ID: 0x007D, Value: h.String()}}})
	doc := extractBytes(t, data, nil)
	one(t, &doc.Parts[0], gofastcore.WarnLimitExceeded, "more than 10000 fields")

	e := &gofastemail.Extractor{MaxHeaderBytes: 1000}
	doc, err := e.Extract(context.Background(), bytes.NewReader(data), int64(len(data)), nil)
	if err != nil {
		t.Fatal(err)
	}
	one(t, &doc.Parts[0], gofastcore.WarnLimitExceeded, "longer than 1000 bytes")
}

// Warnings that can be given for many values come once per part, with a
// count and the first values; given once, the message is as before.
func TestWarningsGrouped(t *testing.T) {
	subject := func(n int) string {
		var b strings.Builder
		for i := range n {
			fmt.Fprintf(&b, "=?zz-%d?q?=E9?= ", i)
		}
		return b.String()
	}
	doc := extract(t, "From: a@example.com\nSubject: "+subject(1)+"\n\nBody.\n", nil)
	one(t, &doc.Parts[0], gofastcore.WarnTextUnmappedGlyphs, `character set "zz-0" is not decoded; bytes outside ASCII are U+FFFD`)

	doc = extract(t, "From: a@example.com\nSubject: "+subject(20)+subject(20)+"\nX-Bad: =?utf-8?q?=FF?= =?iso-2022-jp?q?=FF?=\n\nBody.\n", nil)
	p := &doc.Parts[0]
	ws := warnings(p, gofastcore.WarnTextUnmappedGlyphs)
	if len(ws) != 2 || ws[0].Message != `20 character sets are not decoded ("zz-0", "zz-1", "zz-2", "zz-3", "zz-4", "zz-5", "zz-6", "zz-7" and 12 more); bytes outside ASCII in them are U+FFFD` ||
		!strings.HasPrefix(ws[1].Message, `bytes not valid in 2 character sets ("utf-8", "iso-2022-jp")`) {
		t.Fatalf("warnings %+v", p.Warnings)
	}

	// Past the bound on distinct values counted.
	doc = extract(t, "From: a@example.com\nSubject: "+subject(10_050)+"\n\nBody.\n", nil)
	one(t, &doc.Parts[0], gofastcore.WarnTextUnmappedGlyphs, "more than 10000 character sets", `"zz-7" and others`)

	// Transfer encodings, boundaries, external bodies, alternatives.
	msg := "From: a@example.com\nSubject: s\nContent-Type: multipart/mixed; boundary=b\n\n--b\n\nText.\n"
	for _, cte := range []string{"x-one", "x-two", "x-one", "x-three"} {
		msg += "--b\nContent-Type: application/pdf\nContent-Transfer-Encoding: " + cte + "\nContent-Disposition: attachment\n\n%PDF\n"
	}
	for _, mt := range []string{"multipart/mixed", "multipart/related", "multipart/mixed"} {
		msg += "--b\nContent-Type: " + mt + "\n\nno boundary\n"
	}
	for _, u := range []string{"http://example.com/1", "http://example.com/2", "http://example.com/1"} {
		msg += "--b\nContent-Type: message/external-body; access-type=URL; URL=\"" + u + "\"\n\nContent-Type: application/octet-stream\n\n"
	}
	for _, kinds := range [][]string{{"text/plain", "text/html"}, {"text/html", "text/enriched"}, {"text/plain", "text/html"}} {
		msg += "--b\nContent-Type: multipart/alternative; boundary=a\n\n"
		for _, k := range kinds {
			msg += "--a\nContent-Type: " + k + "\n\nalt\n"
		}
		msg += "--a--\n"
	}
	msg += "--b--\n"
	doc = extract(t, msg, &gofastcore.Options{Child: dispatcher(new([]seen))})
	p = &doc.Parts[0]
	one(t, p, gofastemail.WarnUnknownTransferEncoding, `3 transfer encodings ("x-one", "x-two", "x-three")`)
	one(t, p, gofastcore.WarnInputRepaired, `bodies of 2 media types with no boundary ("multipart/mixed", "multipart/related")`)
	one(t, p, gofastcore.WarnInputExternalReference, `parts refer to 2 places outside the message for their content: "http://example.com/1" (access type "URL"); "http://example.com/2" (access type "URL")`)
	one(t, p, gofastemail.WarnAlternatives, "2 sets of alternative bodies of different kinds: (text/plain, text/html); (text/html, text/enriched);")
	if len(p.Warnings) != 4 {
		t.Errorf("warnings %+v", p.Warnings)
	}

	// Attachments of an Outlook message that refer to files outside it.
	var atts []msgtest.Attachment
	for _, path := range []string{`C:\a.doc`, `C:\b.doc`, `C:\a.doc`} {
		atts = append(atts, msgtest.Attachment{Props: []msgtest.Prop{{ID: 0x3705, Value: int32(4)}, {ID: 0x370D, Value: path}}})
	}
	doc = extractBytes(t, msgtest.Build(msgtest.Msg{Props: []msgtest.Prop{{ID: 0x0037, Value: "refs"}}, Attachments: atts}), nil)
	one(t, &doc.Parts[0], gofastcore.WarnInputExternalReference, `attachments refer to 2 files outside the message: "C:\\a.doc"; "C:\\b.doc"`)
}
