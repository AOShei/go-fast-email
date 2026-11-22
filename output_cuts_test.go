package gofastemail_test

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/AOShei/go-fast-core"
	"github.com/AOShei/go-fast-email"
	"github.com/AOShei/go-fast-email/internal/msgtest"
)

// A part name is the subject up to 4096 bytes: a subject of 300 Chinese
// characters (900 bytes) is the name in full; a longer one is cut on a
// character boundary, with limit.exceeded.
func TestPartNameFromSubject(t *testing.T) {
	subject := strings.Repeat("中", 300)
	doc := extract(t, "From: a@example.com\nSubject: "+subject+"\n\nBody.\n", nil)
	if p := &doc.Parts[0]; p.Name != subject || len(p.Warnings) != 0 {
		t.Fatalf("name of %d bytes, warnings %+v", len(p.Name), p.Warnings)
	}
	subject = strings.Repeat("中", 1400) // 4200 bytes
	doc = extract(t, "From: a@example.com\nSubject: "+subject+"\n\nBody.\n", nil)
	p := &doc.Parts[0]
	if len(p.Name) > 4096 || len(p.Name) < 4094 || !utf8.ValidString(p.Name) || p.Blocks[0].Text != subject {
		t.Fatalf("name of %d bytes", len(p.Name))
	}
	one(t, p, gofastcore.WarnLimitExceeded, "subject of 4200 bytes", "cut to 4096 bytes")
}

// A URL of content outside the message is named in full up to 4096
// bytes; past that it is cut with a note in the warning, which is not
// output, so no limit.exceeded.
func TestLongReference(t *testing.T) {
	for _, n := range []int{2000, 5000} {
		url := "http://example.com/" + strings.Repeat("u", n)
		msg := "From: a@example.com\nContent-Type: multipart/mixed; boundary=b\n\n--b\n\nText.\n" +
			"--b\nContent-Type: message/external-body; access-type=URL; URL=\"" + url + "\"\n\nContent-Type: application/octet-stream\n\n--b--\n"
		doc := extract(t, msg, &gofastcore.Options{Child: dispatcher(new([]seen))})
		p := &doc.Parts[0]
		ref := one(t, p, gofastcore.WarnInputExternalReference)
		if n < 4096 {
			if !strings.Contains(ref.Message, url) || p.Has(gofastcore.WarnLimitExceeded) {
				t.Errorf("%d: %+v", n, p.Warnings)
			}
			continue
		}
		if !strings.Contains(ref.Message, "(cut at 4096 of its") || p.Has(gofastcore.WarnLimitExceeded) {
			t.Errorf("%d: %q, %+v", n, ref.Message[len(ref.Message)-60:], p.Warnings)
		}
	}
}

// A character set name too long to pass on to an embedded document is
// reported.
func TestLongCharsetNotPassedOn(t *testing.T) {
	label := "x-" + strings.Repeat("c", 70)
	msg := "From: a@example.com\nContent-Type: multipart/mixed; boundary=b\n\n--b\n\nText.\n" +
		"--b\nContent-Type: text/html; charset=" + label + "\n\n<p>hi</p>\n--b--\n"
	var got []seen
	doc := extract(t, msg, &gofastcore.Options{Child: dispatcher(&got)})
	if len(got) != 1 || got[0].mime != "text/html" {
		t.Fatalf("children %+v", got)
	}
	one(t, &doc.Parts[0], gofastcore.WarnLimitExceeded, "longer than 63 bytes", "(1 name)")
	// A registered name is passed on.
	got = nil
	doc = extract(t, strings.Replace(msg, label, "iso-8859-1", 1), &gofastcore.Options{Child: dispatcher(&got)})
	if len(got) != 1 || got[0].mime != "text/html; charset=iso-8859-1" || doc.Parts[0].Has(gofastcore.WarnLimitExceeded) {
		t.Fatalf("children %+v, warnings %+v", got, doc.Parts[0].Warnings)
	}
}

// In a mailbox, a message whose header is longer than 64 KiB still ends
// where its Content-Length says (mboxcl2): a "From " line in its body is
// text.
func TestCountedMessageLongHeader(t *testing.T) {
	body := "First line.\n\nFrom the body, not a separator.\n"
	box := "From a@example.com Tue Mar  3 10:00:00 2026\nFrom: a@example.com\nSubject: one\nX-Big: " + strings.Repeat("b", 100_000) +
		fmt.Sprintf("\nContent-Length: %d\n\n", len(body)) + body +
		"\nFrom b@example.com Tue Mar  3 11:00:00 2026\nFrom: b@example.com\nSubject: two\n\nLast.\n"
	doc := extract(t, box, nil)
	if len(doc.Parts) != 2 || !strings.Contains(strings.Join(texts(&doc.Parts[0]), "\n"), "From the body") {
		t.Fatalf("%d parts", len(doc.Parts))
	}
}

// A compound file with more directory entries than are read is refused
// as over the limit, not as damaged.
func TestMsgTooManyEntries(t *testing.T) {
	if testing.Short() {
		t.Skip("builds a 33 MB compound file")
	}
	data := manyEntries(1<<18 + 8)
	_, err := gofastemail.Extract(context.Background(), bytes.NewReader(data), int64(len(data)), nil)
	if !errors.Is(err, gofastcore.ErrLimitExceeded) || errors.Is(err, gofastcore.ErrMalformed) {
		t.Fatalf("err %v", err)
	}
}

// manyEntries is an Outlook message with n directory entries or more: its
// properties stream and n empty streams in one storage.
func manyEntries(n int) []byte {
	streams := map[string][]byte{"__properties_version1.0": make([]byte, 32)}
	for i := range n {
		streams[fmt.Sprintf("s/%06d", i)] = nil
	}
	return msgtest.Compound(streams)
}
