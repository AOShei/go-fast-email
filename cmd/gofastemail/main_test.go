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

func TestDisplayFlag(t *testing.T) {
	cases := []struct {
		value string
		want  gofastcore.Display
		err   bool
	}{
		{"true", gofastcore.DisplayAny, false}, // the flag given alone
		{"all", gofastcore.DisplayAny, false},
		{"false", 0, false},
		{"", 0, false},
		{"hidden", gofastcore.DisplayHidden, false},
		{"no_ink", gofastcore.DisplayNoInk, false},
		{"hidden,no_ink", gofastcore.DisplayHidden | gofastcore.DisplayNoInk, false},
		{"hidden+no_ink", gofastcore.DisplayHidden | gofastcore.DisplayNoInk, false},
		{"unknown", 0, true},
		{"nope", 0, true},
	}
	for _, c := range cases {
		var f displayFlag
		err := f.Set(c.value)
		if (err != nil) != c.err || (err == nil && f.reasons != c.want) {
			t.Errorf("Set(%q) = %v, %v; want %v, error %v", c.value, f.reasons, err, c.want, c.err)
		}
	}
}

func TestRenderFlags(t *testing.T) {
	path := filepath.Join(t.TempDir(), "a.eml")
	if err := os.WriteFile(path, []byte("From: a@example.com\nSubject: hello\n\nBody.\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{
		{"-exclude-display"},
		{"-exclude-display=hidden"},
		{"-exclude-display=all"},
		{"-furniture", "-stored", "-part-markers", "-exclude-display=hidden,no_ink"},
	} {
		var out, errs bytes.Buffer
		args = append(args, "-format", "text", path)
		if code := run(context.Background(), args, &out, &errs); code != 0 {
			t.Fatalf("%v: exit %d: %s", args, code, errs.String())
		}
		if !strings.Contains(out.String(), "Body.") {
			t.Errorf("%v: text:\n%s", args, out.String())
		}
	}
	var out, errs bytes.Buffer
	if code := run(context.Background(), []string{"-exclude-display=unknown", path}, &out, &errs); code != 2 {
		t.Fatalf("-exclude-display=unknown: exit %d", code)
	}
	if !strings.Contains(errs.String(), "hidden, no_ink") {
		t.Errorf("error does not list the reasons: %s", errs.String())
	}
}
