package main

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/AOShei/go-fast-core"
)

func TestRun(t *testing.T) {
	dir := t.TempDir()
	inner := "From: c@example.com\nSubject: inner\n\nInner body.\n"
	eml := "From: a@example.com\nSubject: hello\nContent-Type: multipart/mixed; boundary=b\n\n--b\n\nOuter body.\n" +
		"--b\nContent-Type: message/rfc822\n\n" + inner + "--b\nContent-Type: application/pdf\nContent-Disposition: attachment; filename=x.pdf\n\n%PDF\n--b--\n"
	if err := os.WriteFile(filepath.Join(dir, "a.eml"), []byte(eml), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "b.mbox"), []byte("From x\nSubject: one\n\nfirst\n\nFrom y\nSubject: two\n\nsecond\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	var out, errs bytes.Buffer
	if code := run(context.Background(), []string{filepath.Join(dir, "a.eml")}, &out, &errs); code != 0 {
		t.Fatalf("exit %d: %s", code, errs.String())
	}
	var doc gofastcore.Document
	if err := json.Unmarshal(out.Bytes(), &doc); err != nil {
		t.Fatal(err)
	}
	if doc.Metadata.Title != "hello" || len(doc.Children) != 2 || doc.Children[0].Metadata.Title != "inner" {
		t.Fatalf("document %+v", doc.Metadata)
	}
	out.Reset()
	if code := run(context.Background(), []string{"-format", "text", "-parts", "2", filepath.Join(dir, "b.mbox")}, &out, &errs); code != 0 {
		t.Fatalf("exit %d: %s", code, errs.String())
	}
	if !strings.Contains(out.String(), "second") || strings.Contains(out.String(), "first") {
		t.Fatalf("text:\n%s", out.String())
	}
	if code := run(context.Background(), []string{"-format", "nope", dir}, &out, &errs); code != 2 {
		t.Fatalf("bad flag: exit %d", code)
	}
}
