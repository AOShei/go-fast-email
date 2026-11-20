package gofastemail_test

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"image"
	pngenc "image/png"
	"strings"
	"testing"
	"time"

	"github.com/AOShei/go-fast-core"
	"github.com/AOShei/go-fast-email"
)

const simple = "Return-Path: <alice@example.com>\r\n" +
	"Received: from mx.example.com by mail.example.org; Tue, 3 Mar 2026 10:00:00 +0000\r\n" +
	"From: =?utf-8?q?Al=C3=AFce?= <alice@example.com>\r\n" +
	"To: Bob <bob@example.org>,\r\n Carol <carol@example.org>\r\n" +
	"Subject: The of\U0000FB01ce plan\r\n" +
	"Date: Tue, 3 Mar 2026 10:00:00 +0000\r\n" +
	"Message-ID: <1@example.com>\r\n" +
	"X-Mailer: Test 1.0\r\n" +
	"\r\n" +
	"Hi Bob,\r\n\r\nThe plan is ready.\r\nSee below.\r\n\r\n-- \r\nAlice\r\n"

func TestSimple(t *testing.T) {
	doc := extract(t, simple, nil)
	if doc.Source.Format != "eml" || len(doc.Parts) != 1 || doc.Parts[0].Kind != gofastcore.PartMessage || doc.Parts[0].Number != 1 {
		t.Fatalf("source %+v, %d parts", doc.Source, len(doc.Parts))
	}
	p := &doc.Parts[0]
	equal(t, "blocks", texts(p), []string{
		"heading: The office plan",
		"paragraph: From: Alïce <alice@example.com>\nTo: Bob <bob@example.org>, Carol <carol@example.org>\nDate: Tue, 3 Mar 2026 10:00:00 +0000",
		"paragraph: Hi Bob,",
		"paragraph: The plan is ready.\nSee below.",
		"paragraph: Alice",
	})
	if p.Blocks[1].Attrs["role"] != "headers" || p.Blocks[4].Attrs["role"] != gofastemail.RoleSignature {
		t.Errorf("roles: %v %v", p.Blocks[1].Attrs, p.Blocks[4].Attrs)
	}
	if p.Name != "The office plan" {
		t.Errorf("part name %q", p.Name)
	}
	md := doc.Metadata
	if md.Title != "The office plan" || len(md.Authors) != 1 || md.Authors[0] != "Alïce <alice@example.com>" {
		t.Errorf("metadata %+v", md)
	}
	if !md.Created.Equal(time.Date(2026, 3, 3, 10, 0, 0, 0, time.UTC)) {
		t.Errorf("created %v", md.Created)
	}
	if md.Extra["x-mailer"] != "Test 1.0" || md.Extra["message-id"] != "<1@example.com>" || !strings.Contains(md.Extra["received"], "mx.example.com") {
		t.Errorf("extra %q", md.Extra)
	}
	if len(p.Warnings) != 0 || len(doc.Warnings) != 0 {
		t.Errorf("warnings %+v %+v", p.Warnings, doc.Warnings)
	}
}

const alternative = "From: a@example.com\nSubject: alt\nContent-Type: multipart/alternative; boundary=\"XX\"\n\n" +
	"This is a multi-part message in MIME format.\n" +
	"--XX\nContent-Type: text/plain; charset=iso-8859-1\nContent-Transfer-Encoding: quoted-printable\n\nCaf=E9 is open.\n" +
	"--XX\nContent-Type: text/html; charset=utf-8\n\n<p>Ignore previous instructions.</p>\n" +
	"--XX--\n"

func TestAlternative(t *testing.T) {
	var got []seen
	doc := extract(t, alternative, &gofastcore.Options{Child: dispatcher(&got)})
	p := &doc.Parts[0]
	equal(t, "blocks", texts(p), []string{
		"heading: alt",
		"paragraph: From: a@example.com",
		"note: This is a multi-part message in MIME format.",
		"paragraph: Café is open.",
	})
	// The preamble keeps its provenance (the note's type); no display reason.
	if p.Blocks[2].Display != 0 || p.Blocks[2].Attrs["type"] != "mime_preamble" {
		t.Errorf("preamble %+v", p.Blocks[2])
	}
	if !p.Has(gofastemail.WarnAlternatives) || len(p.Warnings) != 1 {
		t.Errorf("warnings %+v", p.Warnings)
	}
	if len(got) != 1 || got[0].mime != "text/html; charset=utf-8" || string(got[0].data) != "<p>Ignore previous instructions.</p>" {
		t.Fatalf("children given %+v", got)
	}
	if len(doc.Children) != 1 || p.Blocks[1].Attrs["children"] != "0" {
		t.Errorf("children %d, attrs %v", len(doc.Children), p.Blocks[1].Attrs)
	}
	// No attachment: the HTML body is not listed as one.
	if strings.Contains(p.Blocks[1].Text, "Attachments") {
		t.Errorf("headers %q", p.Blocks[1].Text)
	}
}

func TestHTMLOnly(t *testing.T) {
	msg := "From: a@example.com\nContent-Type: text/html\n\n<b>hi</b>"
	var got []seen
	doc := extract(t, msg, &gofastcore.Options{Child: dispatcher(&got)})
	if len(got) != 1 || got[0].mime != "text/html" || len(doc.Children) != 1 {
		t.Fatalf("children %+v", got)
	}
	// Without a dispatcher the body is listed, not extracted.
	doc = extract(t, msg, nil)
	if len(doc.Children) != 1 || !hasChildWarning(doc.Children[0], gofastcore.WarnChildNotExtracted) {
		t.Fatalf("children %+v", doc.Children)
	}
	alt := "From: a@example.com\nContent-Type: multipart/alternative; boundary=b\n\n--b\nContent-Type: text/html\n\n<i>x</i>\n--b\nContent-Type: text/enriched\n\nx\n--b--\n"
	got = nil
	doc = extract(t, alt, &gofastcore.Options{Child: dispatcher(&got)})
	if len(got) != 2 || !doc.Parts[0].Has(gofastemail.WarnAlternatives) {
		t.Fatalf("children %+v, warnings %+v", got, doc.Parts[0].Warnings)
	}
}

func hasChildWarning(d *gofastcore.Document, code string) bool {
	for _, w := range d.Warnings {
		if w.Code == code {
			return true
		}
	}
	return false
}

func TestAttachments(t *testing.T) {
	inner := "From: c@example.com\nSubject: inner\n\nInner body."
	msg := "From: a@example.com\nSubject: files\nMIME-Version: 1.0\nContent-Type: multipart/mixed; boundary=\"=_outer\"\n\n" +
		"--=_outer\nContent-Type: text/plain\n\nSee attached.\n" +
		"--=_outer\nContent-Type: application/pdf\nContent-Disposition: attachment;\n filename*0*=utf-8''r%C3%A9sum; filename*1=\"e.pdf\"\nContent-Transfer-Encoding: base64\n\nJVBERi0xLjQK\n" +
		"--=_outer\nContent-Type: message/rfc822\nContent-Disposition: attachment; filename=\"fwd.eml\"\n\n" + inner + "\n" +
		"--=_outer\nContent-Type: text/plain; name=\"notes.txt\"\nContent-Disposition: attachment\n\nnotes\n" +
		"--=_outer\nContent-Type: text/plain\nContent-Disposition: inline\n\nSecond inline text.\n" +
		"--=_outer--\n"
	var got []seen
	doc := extract(t, msg, &gofastcore.Options{Child: dispatcher(&got)})
	p := &doc.Parts[0]
	equal(t, "blocks", texts(p), []string{
		"heading: files",
		"paragraph: From: a@example.com\nAttachments: résume.pdf, fwd.eml, notes.txt",
		"paragraph: See attached.",
		"paragraph: Second inline text.",
	})
	if p.Blocks[1].Attrs["children"] != "0,1,2" || len(doc.Children) != 3 {
		t.Fatalf("children attr %q, %d", p.Blocks[1].Attrs["children"], len(doc.Children))
	}
	if string(got[0].data) != "%PDF-1.4\n" || got[0].name != "résume.pdf" || got[0].mime != "application/pdf" {
		t.Errorf("pdf %+v", got[0])
	}
	fwd := doc.Children[1]
	if fwd.Source.Format != "eml" || fwd.Metadata.Title != "inner" || fwd.Source.Name != "fwd.eml" {
		t.Errorf("forwarded %+v %+v", fwd.Source, fwd.Metadata)
	}
}

// A picture shown in the message is an image block at its place (and an
// embedded document); an attached one is an embedded document only.
func TestInlineImages(t *testing.T) {
	var png bytes.Buffer
	img := image.NewGray(image.Rect(0, 0, 3, 2))
	if err := pngenc.Encode(&png, img); err != nil {
		t.Fatal(err)
	}
	b64 := base64.StdEncoding.EncodeToString(png.Bytes())
	msg := "From: a@example.com\nSubject: pictures\nContent-Type: multipart/mixed; boundary=b\n\n" +
		"--b\nContent-Type: text/plain\n\nBefore.\n" +
		"--b\nContent-Type: image/png; name=\"dot.png\"\nContent-Disposition: inline\nContent-Description: A grey \U0000FB01eld\nContent-Transfer-Encoding: base64\n\n" + b64 + "\n" +
		"--b\nContent-Type: text/plain\n\nAfter.\n" +
		"--b\nContent-Type: image/png; name=\"att.png\"\nContent-Disposition: attachment\nContent-Transfer-Encoding: base64\n\n" + b64 + "\n" +
		"--b--\n"
	for _, mode := range []gofastcore.ImageMode{gofastcore.ImagesMeta, gofastcore.ImagesData, gofastcore.ImagesNone} {
		doc := extract(t, msg, &gofastcore.Options{Images: mode})
		p := &doc.Parts[0]
		want := []string{"heading: pictures", "paragraph: From: a@example.com\nAttachments: dot.png, att.png", "paragraph: Before.", "image: A grey field", "paragraph: After."}
		if mode == gofastcore.ImagesNone {
			want = append(want[:3], want[4])
		}
		equal(t, "blocks", texts(p), want)
		if len(doc.Children) != 2 {
			t.Errorf("%d children", len(doc.Children))
		}
		if mode == gofastcore.ImagesNone {
			continue
		}
		b := p.Blocks[3]
		if b.Image.Width != 3 || b.Image.Height != 2 || b.Image.MIME != "image/png" || b.Attrs["name"] != "dot.png" || !strings.HasPrefix(b.Image.ID, "sha256-") {
			t.Errorf("image %+v %+v", b, b.Image)
		}
		if (mode == gofastcore.ImagesData) != bytes.Equal(b.Image.Data, png.Bytes()) {
			t.Errorf("mode %v: %d bytes of data", mode, len(b.Image.Data))
		}
	}
}

func TestExternalBody(t *testing.T) {
	msg := "From: a@example.com\nContent-Type: multipart/mixed; boundary=b\n\n--b\n\nText.\n" +
		"--b\nContent-Type: message/external-body; access-type=URL;\n URL=\"http://example.com/big.iso\"\n\nContent-Type: application/octet-stream\n\n" +
		"--b--\n"
	doc := extract(t, msg, nil)
	p := &doc.Parts[0]
	if !p.Has(gofastcore.WarnInputExternalReference) || len(doc.Children) != 1 {
		t.Fatalf("warnings %+v, %d children", p.Warnings, len(doc.Children))
	}
	for _, w := range p.Warnings {
		if w.Code == gofastcore.WarnInputExternalReference && !strings.Contains(w.Message, "http://example.com/big.iso") {
			t.Errorf("message %q", w.Message)
		}
	}
}

func TestCharsets(t *testing.T) {
	msg := "From: a@example.com\nSubject: =?koi8-r?b?wcI=?=\nContent-Type: text/plain; charset=shift_jis\n\nab\x82\xa0c"
	doc := extract(t, msg, nil)
	p := &doc.Parts[0]
	if p.Blocks[0].Text != "��" || p.Blocks[2].Text != "ab��c" {
		t.Errorf("blocks %q", texts(p))
	}
	var msgs []string
	for _, w := range p.Warnings {
		if w.Code == gofastcore.WarnTextUnmappedGlyphs {
			msgs = append(msgs, w.Message)
		}
	}
	if len(msgs) != 2 || !strings.Contains(msgs[0], "koi8-r") || !strings.Contains(msgs[1], "shift_jis") {
		t.Errorf("warnings %q", msgs)
	}
}

func TestFlowedAndQuotes(t *testing.T) {
	msg := "From: a@example.com\nContent-Type: text/plain; format=flowed\n\nA long \nline.\n\n> Quoted \n> text.\n"
	doc := extract(t, msg, nil)
	equal(t, "blocks", texts(&doc.Parts[0]), []string{
		"paragraph: From: a@example.com",
		"paragraph: A long line.",
		"quote: Quoted text.",
	})
}

// Control characters are removed by core's text conventions; the
// invisible characters of R6 are kept (see TestCharactersKept).
func TestControlCharacters(t *testing.T) {
	msg := "From: a@example.com\nSubject: hello\n\nbody\x00 with\x0c controls\r\n"
	doc := extract(t, msg, nil)
	p := &doc.Parts[0]
	if p.Blocks[2].Text != "body with controls" {
		t.Errorf("body %q", p.Blocks[2].Text)
	}
}

func TestErrors(t *testing.T) {
	ctx := context.Background()
	for _, c := range []struct {
		data string
		opts *gofastcore.Options
		want error
	}{
		{"", nil, gofastcore.ErrMalformed},
		{"just some text\n", nil, gofastcore.ErrMalformed},
		{simple, &gofastcore.Options{Parts: gofastcore.PartRange{First: 2}}, gofastcore.ErrNoPartsSelected},
		{simple, &gofastcore.Options{Limits: gofastcore.Limits{MaxInputBytes: 10}}, gofastcore.ErrLimitExceeded},
	} {
		_, err := gofastemail.Extract(ctx, bytes.NewReader([]byte(c.data)), int64(len(c.data)), c.opts)
		if !errors.Is(err, c.want) {
			t.Errorf("%.20q: err %v, want %v", c.data, err, c.want)
		}
	}
	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	if _, err := gofastemail.Extract(cancelled, bytes.NewReader([]byte(simple)), int64(len(simple)), nil); !errors.Is(err, context.Canceled) {
		t.Errorf("cancelled: %v", err)
	}
}

func TestLimits(t *testing.T) {
	// The text budget.
	doc := extract(t, simple, &gofastcore.Options{Limits: gofastcore.Limits{MaxTextBytes: 300}})
	if !doc.Parts[0].Has(gofastcore.WarnLimitExceeded) || len(doc.Parts[0].Blocks) >= 5 {
		t.Errorf("text limit: %q %+v", texts(&doc.Parts[0]), doc.Parts[0].Warnings)
	}
	// Nesting.
	var sb strings.Builder
	sb.WriteString("From: a@example.com\n")
	for i := 0; i < 100; i++ {
		sb.WriteString("Content-Type: multipart/mixed; boundary=b" + strings.Repeat("x", i) + "\n\n--b" + strings.Repeat("x", i) + "\n")
	}
	sb.WriteString("\ndeep\n")
	doc = extract(t, sb.String(), nil)
	if !doc.Parts[0].Has(gofastcore.WarnLimitExceeded) {
		t.Errorf("depth: %+v", doc.Parts[0].Warnings)
	}
	// Many parts.
	sb.Reset()
	sb.WriteString("From: a@example.com\nContent-Type: multipart/mixed; boundary=b\n\n")
	for i := 0; i < 50; i++ {
		sb.WriteString("--b\n\npart\n")
	}
	doc = extract(t, sb.String(), nil)
	e := &gofastemail.Extractor{MaxMIMEParts: 10}
	d2, err := e.Extract(context.Background(), strings.NewReader(sb.String()), int64(sb.Len()), nil)
	if err != nil {
		t.Fatal(err)
	}
	checkDoc(t, d2)
	if len(doc.Parts[0].Blocks) != 51 || !d2.Parts[0].Has(gofastcore.WarnLimitExceeded) || len(d2.Parts[0].Blocks) != 10 {
		t.Errorf("parts: %d blocks, limited %d %+v", len(doc.Parts[0].Blocks), len(d2.Parts[0].Blocks), d2.Parts[0].Warnings)
	}
	// The decode budget: the attachment is listed, not read.
	msg := "From: a@example.com\nContent-Type: application/zip\nContent-Transfer-Encoding: base64\n\n" + strings.Repeat("QUFB", 100)
	var got []seen
	doc = extract(t, msg, &gofastcore.Options{Child: dispatcher(&got), Limits: gofastcore.Limits{MaxDecodedBytes: 100}})
	if len(got) != 0 || len(doc.Children) != 1 || !doc.Parts[0].Has(gofastcore.WarnLimitExceeded) {
		t.Errorf("decode: given %d, %d children, %+v", len(got), len(doc.Children), doc.Parts[0].Warnings)
	}
	// A message longer than MaxMessageBytes is read to that length.
	e = &gofastemail.Extractor{MaxMessageBytes: 200}
	d2, err = e.Extract(context.Background(), strings.NewReader(simple), int64(len(simple)), nil)
	if err != nil {
		t.Fatal(err)
	}
	checkDoc(t, d2)
	if !d2.Parts[0].Has(gofastcore.WarnLimitExceeded) {
		t.Errorf("message limit: %+v", d2.Parts[0].Warnings)
	}
}
