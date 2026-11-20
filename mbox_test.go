package gofastemail_test

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/AOShei/go-fast-core"
	"github.com/AOShei/go-fast-email"
)

const mailbox = "From alice@example.com Tue Mar  3 10:00:00 2026\n" +
	"From: alice@example.com\nSubject: one\nX-Spam: no\n\n" +
	"First body.\n>From the start, quoted.\n>>From twice.\n\n" +
	"From bob@example.com Tue Mar  3 11:00:00 2026\n" +
	"From: bob@example.com\nSubject: two\nContent-Type: multipart/mixed; boundary=b\n\n" +
	"--b\n\nSecond body.\n--b\nContent-Type: application/pdf\nContent-Disposition: attachment; filename=a.pdf\n\n%PDF\n--b--\n\n" +
	"From carol@example.com Tue Mar  3 12:00:00 2026\n" +
	"From: carol@example.com\nSubject: three\nContent-Length: 28\n\n" +
	"Counted.\n\nFrom inside body.\n\n" +
	"From dave@example.com Tue Mar  3 13:00:00 2026\n" +
	"From: dave@example.com\nSubject: four\n\nLast.\n"

func TestMbox(t *testing.T) {
	var got []seen
	doc := extract(t, mailbox, &gofastcore.Options{Child: dispatcher(&got)})
	if doc.Source.Format != "mbox" || len(doc.Parts) != 4 {
		t.Fatalf("format %q, %d parts", doc.Source.Format, len(doc.Parts))
	}
	equal(t, "message 1", texts(&doc.Parts[0]), []string{
		"heading: one",
		"paragraph: From: alice@example.com",
		"note: From alice@example.com Tue Mar  3 10:00:00 2026\nX-Spam: no",
		"paragraph: First body.\nFrom the start, quoted.",
		"quote: From twice.", // ">>From" unquoted to ">From", a quoted line
	})
	// Headers are routine: a note, not flagged.
	if h := doc.Parts[0].Blocks[2].Hidden; h != 0 || doc.Parts[0].Blocks[2].Attrs["type"] != "headers" || doc.Parts[0].Has(gofastcore.WarnTextHidden) {
		t.Errorf("headers note hidden %v", h)
	}
	equal(t, "message 2", texts(&doc.Parts[1])[3:], []string{"paragraph: Second body."})
	if doc.Parts[1].Blocks[1].Attrs["children"] != "0" || !strings.Contains(doc.Parts[1].Blocks[1].Text, "Attachments: a.pdf") {
		t.Errorf("attachment: %+v", doc.Parts[1].Blocks[1])
	}
	equal(t, "message 3", texts(&doc.Parts[2])[3:], []string{"paragraph: Counted.", "paragraph: From inside body."})
	equal(t, "message 4", texts(&doc.Parts[3])[3:], []string{"paragraph: Last."})
	for i, p := range doc.Parts {
		if p.Number != i+1 || p.Kind != gofastcore.PartMessage {
			t.Errorf("part %d: %v %d", i, p.Kind, p.Number)
		}
	}
	if len(doc.Metadata.Extra) != 0 || doc.Metadata.Title != "" {
		t.Errorf("metadata %+v", doc.Metadata)
	}
}

func TestMboxRange(t *testing.T) {
	doc := extract(t, mailbox, &gofastcore.Options{Parts: gofastcore.PartRange{First: 2, Last: 3}})
	if len(doc.Parts) != 2 || doc.Parts[0].Number != 2 || doc.Parts[1].Name != "three" {
		t.Fatalf("parts %d", len(doc.Parts))
	}
	doc = extract(t, mailbox, &gofastcore.Options{Limits: gofastcore.Limits{MaxParts: 1}})
	if len(doc.Parts) != 1 || !hasWarning(doc, gofastcore.WarnLimitExceeded) {
		t.Fatalf("max parts: %d %+v", len(doc.Parts), doc.Warnings)
	}
	_, err := gofastemail.Extract(context.Background(), strings.NewReader(mailbox), int64(len(mailbox)), &gofastcore.Options{Parts: gofastcore.PartRange{First: 9}})
	if !errors.Is(err, gofastcore.ErrNoPartsSelected) {
		t.Fatalf("range past the end: %v", err)
	}
}

// TestMboxMany checks that a mailbox of many empty messages is bounded by
// the text budget, and that a long line does not confuse the scan.
func TestMboxMany(t *testing.T) {
	var b bytes.Buffer
	for i := 0; i < 5000; i++ {
		b.WriteString("From x\n\n")
	}
	doc := extract(t, b.String(), &gofastcore.Options{Limits: gofastcore.Limits{MaxTextBytes: 100 * gofastcore.BlockCost}})
	if len(doc.Parts) > 101 || !hasWarning(doc, gofastcore.WarnLimitExceeded) {
		t.Fatalf("%d parts", len(doc.Parts))
	}
	long := "From a\nSubject: s\n\n" + strings.Repeat("x", 200<<10) + "From not a separator\n\nFrom b\nSubject: t\n\nbody\n"
	doc = extract(t, long, nil)
	if len(doc.Parts) != 2 || doc.Parts[1].Name != "t" {
		t.Fatalf("long line: %d parts", len(doc.Parts))
	}
}
