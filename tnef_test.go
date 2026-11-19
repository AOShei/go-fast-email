package gofastemail_test

import (
	"encoding/base64"
	"testing"

	"github.com/AOShei/go-fast-core"
	"github.com/AOShei/go-fast-email/internal/msgtest"
)

// winmailDat is a TNEF stream as Outlook attaches it: the RTF body, a
// plain body in the old attribute, a file, and an attached message.
func winmailDat() []byte {
	inner := msgtest.TNEF(
		msgtest.TNEFAttr(1, 0x00069003, msgtest.TNEFProps(
			msgtest.TNEFProp{ID: 0x0037, Value: "Inner"},
			msgtest.TNEFProp{ID: 0x1000, Value: "Inner body."},
		)),
	)
	return msgtest.TNEF(
		msgtest.TNEFAttr(1, 0x00069007, []byte{0xE4, 0x04, 0, 0, 0, 0, 0, 0}), // code page 1252
		msgtest.TNEFAttr(1, 0x0002800C, []byte("Caf\xe9 at noon.\r\n\r\nBring the report.")),
		msgtest.TNEFAttr(1, 0x00069003, msgtest.TNEFProps(
			msgtest.TNEFProp{ID: 0x0037, Value: "Lunch"},
			msgtest.TNEFProp{ID: 0x001A, Value: "IPM.Microsoft Mail.Note"},
			msgtest.TNEFProp{ID: 0x1009, Value: rtfLiteral("{\\rtf1 Caf\\'e9 at noon.}")},
		)),
		msgtest.TNEFAttr(2, 0x00069002, make([]byte, 14)),
		msgtest.TNEFAttr(2, 0x00018010, []byte("REPORT.PDF\x00")),
		msgtest.TNEFAttr(2, 0x0006800F, []byte("%PDF-1.4\n")),
		msgtest.TNEFAttr(2, 0x00069005, msgtest.TNEFProps(
			msgtest.TNEFProp{ID: 0x3705, Value: int32(1)},
			msgtest.TNEFProp{ID: 0x3707, Value: "Quarterly report.pdf"},
			msgtest.TNEFProp{ID: 0x370E, Value: "application/pdf"},
		)),
		msgtest.TNEFAttr(2, 0x00069002, make([]byte, 14)),
		msgtest.TNEFAttr(2, 0x00069005, msgtest.TNEFProps(
			msgtest.TNEFProp{ID: 0x3705, Value: int32(5)},
			msgtest.TNEFProp{ID: 0x3001, Value: "Inner"},
			msgtest.TNEFProp{ID: 0x3701, Type: 0x000D, Value: append(make([]byte, 16), inner...)},
		)),
	)
}

func TestTNEF(t *testing.T) {
	msg := "From: a@example.com\nSubject: Lunch\nContent-Type: multipart/mixed; boundary=b\n\n" +
		"--b\nContent-Type: text/plain\n\nSee the attachment.\n" +
		"--b\nContent-Type: application/ms-tnef; name=\"winmail.dat\"\nContent-Disposition: attachment; filename=\"winmail.dat\"\nContent-Transfer-Encoding: base64\n\n" +
		base64.StdEncoding.EncodeToString(winmailDat()) + "\n--b--\n"
	var got []seen
	doc := extract(t, msg, &gofastcore.Options{Child: dispatcher(&got)})
	if len(doc.Children) != 1 {
		t.Fatalf("%d children", len(doc.Children))
	}
	w := doc.Children[0]
	if w.Source.Format != "tnef" || w.Source.Name != "winmail.dat" || w.Metadata.Title != "Lunch" || w.Metadata.Extra["message-class"] != "IPM.Microsoft Mail.Note" {
		t.Fatalf("winmail.dat: %+v %+v", w.Source, w.Metadata)
	}
	equal(t, "blocks", texts(&w.Parts[0]), []string{"heading: Lunch", "paragraph: Café at noon.", "paragraph: Bring the report."})
	if !w.Parts[0].Has("email.alternatives") {
		t.Errorf("warnings %+v", w.Parts[0].Warnings)
	}
	// Children: the RTF body, the file, the attached message.
	if len(w.Children) != 3 {
		t.Fatalf("%d children of winmail.dat", len(w.Children))
	}
	var names []string
	for _, s := range got {
		names = append(names, s.name+" "+s.mime)
	}
	equal(t, "given to the dispatcher", names, []string{" application/rtf", "Quarterly report.pdf application/pdf"})
	if string(got[0].data) != "{\\rtf1 Caf\\'e9 at noon.}" || string(got[1].data) != "%PDF-1.4\n" {
		t.Errorf("rtf %q, pdf %q", got[0].data, got[1].data)
	}
	inner := w.Children[2]
	if inner.Source.Format != "tnef" || inner.Metadata.Title != "Inner" {
		t.Fatalf("inner %+v %+v", inner.Source, inner.Metadata)
	}
	equal(t, "inner", texts(&inner.Parts[0]), []string{"heading: Inner", "paragraph: Inner body."})

	// Without a dispatcher the stream is read all the same.
	doc = extract(t, msg, nil)
	if len(doc.Children) != 1 || doc.Children[0].Source.Format != "tnef" {
		t.Fatalf("no dispatcher: %+v", doc.Children)
	}
	// At the depth limit it is listed, not read.
	doc = extract(t, msg, &gofastcore.Options{Depth: gofastcore.DefaultMaxDepth})
	if len(doc.Children) != 1 || !hasChildWarning(doc.Children[0], gofastcore.WarnChildNotExtracted) {
		t.Fatalf("at the depth limit: %+v", doc.Children[0])
	}
}
