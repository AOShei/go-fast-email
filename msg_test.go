package gofastemail_test

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"testing"
	"time"

	"github.com/AOShei/go-fast-core"
	"github.com/AOShei/go-fast-email"
	"github.com/AOShei/go-fast-email/internal/msgtest"
)

// A compound file whose short-stream table cannot be followed is read
// without its short streams, with input.repaired, not refused.
func TestMsgDamagedShortStreams(t *testing.T) {
	data := outlookMessage()
	binary.LittleEndian.PutUint32(data[60:], 0x7FFFFFF0) // first sector of the short-stream table
	doc, err := gofastemail.Extract(context.Background(), bytes.NewReader(data), int64(len(data)), nil)
	if err != nil {
		t.Fatal(err)
	}
	checkDoc(t, doc)
	if !hasWarning(doc, gofastcore.WarnInputRepaired) || len(doc.Parts) != 1 {
		t.Fatalf("warnings %+v", doc.Warnings)
	}
}

// rtfLiteral is compressed RTF of literal bytes only ([MS-OXRTFCP]),
// ended by a reference to the dictionary's write position.
func rtfLiteral(text string) []byte {
	var body []byte
	write := 207
	for i := 0; i < len(text); i += 8 {
		end := min(i+8, len(text))
		body = append(body, 0)
		body = append(body, text[i:end]...)
		write += end - i
	}
	if len(text)%8 == 0 {
		body = append(body, 0x01)
	} else {
		body[len(body)-(len(text)%8)-1] |= 1 << (len(text) % 8)
	}
	ref := (write & 4095) << 4
	body = append(body, byte(ref>>8), byte(ref))
	head := make([]byte, 16)
	binary.LittleEndian.PutUint32(head[0:], uint32(len(body)+12))
	binary.LittleEndian.PutUint32(head[4:], uint32(len(text)))
	binary.LittleEndian.PutUint32(head[8:], 0x75465A4C)
	return append(head, body...)
}

func outlookMessage() []byte {
	sent := time.Date(2026, 3, 3, 10, 0, 0, 0, time.UTC)
	inner := &msgtest.Msg{Props: []msgtest.Prop{
		{ID: 0x0037, Value: "Inner subject"},
		{ID: 0x1000, Value: "Inner body."},
	}}
	return msgtest.Build(msgtest.Msg{
		Props: []msgtest.Prop{
			{ID: 0x0037, Value: "Quarterly of\U0000FB01ce report"},
			{ID: 0x0C1A, Value: "Alice"},
			{ID: 0x5D01, Value: "alice@example.com"},
			{ID: 0x0039, Value: sent},
			{ID: 0x001A, Value: "IPM.Note"},
			{ID: 0x007D, Value: "Received: from mx\r\nX-Origin: test\r\n\r\n"},
			{ID: 0x3FFD, Value: int32(1252)},
			{ID: 0x1000, Value: "Hello Bob,\r\n\r\nThe report is attached.\r\n"},
			{ID: 0x1013, Value: []byte("<p>Hello <b>Bob</b></p>")},
			{ID: 0x1009, Value: rtfLiteral("{\\rtf1 Hello Bob}")},
			{ID: 0x3FDE, Value: int32(65001)},
		},
		Recipients: [][]msgtest.Prop{
			{{ID: 0x3001, Value: "Bob"}, {ID: 0x39FE, Value: "bob@example.org"}, {ID: 0x0C15, Value: int32(1)}},
			{{ID: 0x3001, Value: "Carol"}, {ID: 0x39FE, Value: "carol@example.org"}, {ID: 0x0C15, Value: int32(2)}},
		},
		Attachments: []msgtest.Attachment{
			{Props: []msgtest.Prop{{ID: 0x3705, Value: int32(1)}, {ID: 0x3707, Value: "report.pdf"}, {ID: 0x370E, Value: "application/pdf"}, {ID: 0x3701, Value: []byte("%PDF-1.4\n")}}},
			{Props: []msgtest.Prop{{ID: 0x3707, Value: "fwd.msg"}}, Embedded: inner},
			{Props: []msgtest.Prop{{ID: 0x3705, Value: int32(6)}, {ID: 0x3707, Value: "Chart"}}},
			{Props: []msgtest.Prop{{ID: 0x3705, Value: int32(4)}, {ID: 0x3707, Value: "linked.doc"}, {ID: 0x370D, Value: "\\\\server\\share\\linked.doc"}}},
			{Props: []msgtest.Prop{{ID: 0x3705, Value: int32(1)}, {ID: 0x3707, Value: "logo.png"}, {ID: 0x370E, Value: "image/png"}, {ID: 0x7FFE, Value: true}, {ID: 0x3701, Value: []byte("\x89PNG")}}},
			{Props: []msgtest.Prop{{ID: 0x3705, Value: int32(1)}, {ID: 0x3704, Value: "OLD.TXT"}, {ID: 0x370E, Value: "text/plain"}, {ID: 0x3701, Value: []byte("old")}}},
		},
	})
}

func TestMsg(t *testing.T) {
	data := outlookMessage()
	var got []seen
	doc, err := gofastemail.Extract(context.Background(), bytes.NewReader(data), int64(len(data)), &gofastcore.Options{Child: dispatcher(&got), Name: "m.msg"})
	if err != nil {
		t.Fatal(err)
	}
	checkDoc(t, doc)
	if doc.Source.Format != "msg" || len(doc.Parts) != 1 {
		t.Fatalf("source %+v", doc.Source)
	}
	p := &doc.Parts[0]
	equal(t, "blocks", texts(p), []string{
		"heading: Quarterly office report",
		"paragraph: From: Alice <alice@example.com>\nTo: Bob <bob@example.org>\nCc: Carol <carol@example.org>\nDate: Tue, 03 Mar 2026 10:00:00 +0000\n" +
			"Attachments: report.pdf, fwd.msg, Chart, linked.doc, OLD.TXT",
		"paragraph: Hello Bob,",
		"paragraph: The report is attached.",
		"image: ", // logo.png, hidden from the list of attachments: shown in the body
	})
	if img := p.Blocks[4]; img.Image.MIME != "image/png" || img.Attrs["name"] != "logo.png" || img.Image.ID == "" {
		t.Errorf("image block %+v %+v", img, img.Image)
	}
	md := doc.Metadata
	if md.Title != "Quarterly office report" || md.Authors[0] != "Alice <alice@example.com>" || !md.Created.Equal(time.Date(2026, 3, 3, 10, 0, 0, 0, time.UTC)) {
		t.Errorf("metadata %+v", md)
	}
	if md.Extra["x-origin"] != "test" || md.Extra["message-class"] != "IPM.Note" {
		t.Errorf("extra %q", md.Extra)
	}
	if !p.Has(gofastemail.WarnAlternatives) || !p.Has(gofastcore.WarnInputExternalReference) {
		t.Errorf("warnings %+v", p.Warnings)
	}
	// Children: html, rtf, pdf, embedded message, OLE, linked, logo, old.
	if len(doc.Children) != 8 || p.Blocks[1].Attrs["children"] != "0,1,2,3,4,5,6,7" {
		t.Fatalf("%d children, attr %q", len(doc.Children), p.Blocks[1].Attrs["children"])
	}
	var mimes []string
	for _, s := range got {
		mimes = append(mimes, s.mime)
	}
	equal(t, "given to the dispatcher", mimes, []string{"text/html; charset=utf-8", "application/rtf", "application/pdf", "image/png", "text/plain"})
	if string(got[1].data) != "{\\rtf1 Hello Bob}" || string(got[2].data) != "%PDF-1.4\n" {
		t.Errorf("rtf %q, pdf %q", got[1].data, got[2].data)
	}
	inner := doc.Children[3]
	if inner.Source.Format != "msg" || inner.Metadata.Title != "Inner subject" || inner.Source.Name != "fwd.msg" {
		t.Fatalf("embedded %+v %+v", inner.Source, inner.Metadata)
	}
	equal(t, "embedded", texts(&inner.Parts[0]), []string{"heading: Inner subject", "paragraph: Inner body."})
	for _, i := range []int{4, 5} {
		if !hasChildWarning(doc.Children[i], gofastcore.WarnChildNotExtracted) {
			t.Errorf("child %d: %+v", i, doc.Children[i].Warnings)
		}
	}
}

func TestMsgLimits(t *testing.T) {
	data := outlookMessage()
	// At depth limit 0 the embedded message is listed, not read.
	var got []seen
	doc, err := gofastemail.Extract(context.Background(), bytes.NewReader(data), int64(len(data)),
		&gofastcore.Options{Child: dispatcher(&got), Limits: gofastcore.Limits{MaxDepth: -1, MaxTextBytes: 400}})
	if err != nil {
		t.Fatal(err)
	}
	checkDoc(t, doc)
	if !doc.Parts[0].Has(gofastcore.WarnLimitExceeded) {
		t.Errorf("text limit: %+v", doc.Parts[0].Warnings)
	}
	// Not an Outlook message.
	other := msgtest.Compound(map[string][]byte{"WordDocument": make([]byte, 600)})
	if _, err := gofastemail.Extract(context.Background(), bytes.NewReader(other), int64(len(other)), nil); !errors.Is(err, gofastcore.ErrUnsupported) {
		t.Errorf("other compound file: %v", err)
	}
	cut := data[:600]
	if _, err := gofastemail.Extract(context.Background(), bytes.NewReader(cut), int64(len(cut)), nil); !errors.Is(err, gofastcore.ErrMalformed) {
		t.Errorf("cut short: %v", err)
	}
}

// An Outlook message without PR_MESSAGE_CODEPAGE whose PR_INTERNET_CPID
// says UTF-8 but whose 8-bit strings are in the ANSI code page of its
// locale (PR_MESSAGE_LOCALE_ID), as in Apache POI's
// ASCII_UTF-8_CP1252_LCID1031.msg: the strings that are not UTF-8 are read
// in the locale's code page; those that are UTF-8 stay UTF-8.
func TestMsgLocaleCodepage(t *testing.T) {
	data := msgtest.Build(msgtest.Msg{Props: []msgtest.Prop{
		{ID: 0x0037, Type: msgtest.TypeString8, Value: "Subject \xf6\xe4\xfc Subject"},
		{ID: 0x1000, Type: msgtest.TypeString8, Value: "Body \xc3\xb6 Body"},
		{ID: 0x3FDE, Value: int32(65001)},
		{ID: 0x3FF1, Value: int32(1031)},
	}})
	doc, err := gofastemail.Extract(context.Background(), bytes.NewReader(data), int64(len(data)), nil)
	if err != nil {
		t.Fatal(err)
	}
	checkDoc(t, doc)
	p := &doc.Parts[0]
	got := texts(p)
	if got[0] != "heading: Subject öäü Subject" {
		t.Errorf("subject %q", got[0])
	}
	if got[len(got)-1] != "paragraph: Body ö Body" {
		t.Errorf("body %q", got[len(got)-1])
	}
	if p.Has(gofastcore.WarnTextUnmappedGlyphs) {
		t.Errorf("warnings %+v", p.Warnings)
	}
}
