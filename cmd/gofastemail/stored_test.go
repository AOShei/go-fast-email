package main

import (
	"bytes"
	"context"
	"flag"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/AOShei/go-fast-core"
	"github.com/AOShei/go-fast-html"
)

// Every row of COMMAND_FLAGS.md's -stored table.
func TestStoredFlag(t *testing.T) {
	cases := []struct {
		args  []string
		all   bool
		kinds []string
		err   bool
	}{
		{nil, false, nil, false},
		{[]string{"-stored"}, true, nil, false},
		{[]string{"-stored=true"}, true, nil, false},
		{[]string{"-stored=all"}, true, nil, false},
		{[]string{"-stored=false"}, false, nil, false},
		{[]string{"-stored="}, false, nil, false},
		{[]string{"-stored=markup_comment"}, false, []string{"markup_comment"}, false},
		{[]string{"-stored=field_code,macro"}, false, []string{"field_code", "macro"}, false},
		{[]string{"-stored=field_code+macro"}, false, []string{"field_code", "macro"}, false},
		{[]string{"-stored=stylesheet,generator,custom_property"}, false, []string{"stylesheet", "generator", "custom_property"}, false},
		{[]string{"-stored=nope"}, false, nil, true},
		{[]string{"-stored=field_code,nope"}, false, nil, true},
	}
	for _, c := range cases {
		fs := flag.NewFlagSet("t", flag.ContinueOnError)
		fs.SetOutput(&bytes.Buffer{})
		var s storedFlag
		fs.Var(&s, "stored", "")
		err := fs.Parse(c.args)
		if (err != nil) != c.err || (err == nil && (s.all != c.all || !slices.Equal(s.kinds, c.kinds))) {
			t.Errorf("%v: all %v kinds %v err %v; want %v %v error %v", c.args, s.all, s.kinds, err, c.all, c.kinds, c.err)
		}
	}
	// Every kind of core, go-fast-html and go-fast-rtf is accepted.
	for _, k := range append(append(slices.Clone(gofastcore.StoredKindNames), gofasthtml.StoredKindNames...), "generator", "custom_property") {
		var s storedFlag
		if err := s.Set(k); err != nil {
			t.Errorf("Set(%q): %v", k, err)
		}
	}
}

// A name that is not a kind is an error, exit status 2, listing the kinds.
func TestStoredUnknownKind(t *testing.T) {
	path := filepath.Join(t.TempDir(), "a.eml")
	if err := os.WriteFile(path, []byte("From: a@example.com\n\nBody.\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	var out, errs bytes.Buffer
	if code := run(context.Background(), []string{"-stored=nope", path}, &out, &errs); code != 2 {
		t.Fatalf("exit %d", code)
	}
	for _, k := range []string{gofastcore.StoredMarkupComment, gofastcore.StoredFieldCode, "stylesheet", "generator", "custom_property"} {
		if !strings.Contains(errs.String(), k) {
			t.Errorf("error does not list %q: %s", k, errs.String())
		}
	}
	if !strings.Contains(errs.String(), "in Markdown and text, include stored data (data the file keeps but shows nowhere): all of it, or with =kinds only those kinds (such as markup_comment, field_code, macro)") {
		t.Errorf("help text: %s", errs.String())
	}
}

// An HTML-only message with a comment: go-fast-html stores the comment as
// markup_comment, shown only when asked for.
func TestStoredMarkupComment(t *testing.T) {
	path := filepath.Join(t.TempDir(), "c.eml")
	eml := "From: a@example.com\nSubject: c\nContent-Type: text/html; charset=utf-8\n\n<p>Visible words.</p><!-- a quiet remark -->\n"
	if err := os.WriteFile(path, []byte(eml), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct {
		flags []string
		want  bool
	}{
		{nil, false},
		{[]string{"-stored=false"}, false},
		{[]string{"-stored=field_code"}, false},
		{[]string{"-stored=markup_comment"}, true},
		{[]string{"-stored"}, true},
	} {
		text := runText(t, path, c.flags...)
		if !strings.Contains(text, "Visible words.") {
			t.Errorf("%v: no body text:\n%s", c.flags, text)
		}
		if strings.Contains(text, "a quiet remark") != c.want {
			t.Errorf("%v: comment shown %v, want %v:\n%s", c.flags, !c.want, c.want, text)
		}
	}
}
