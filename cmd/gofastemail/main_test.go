package main

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/AOShei/go-fast-core"
	"github.com/AOShei/go-fast-email"
	"github.com/AOShei/go-fast-email/internal/msgtest"
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
	if !strings.Contains(errs.String(), "the reasons are hidden, no_ink\n") {
		t.Errorf("error does not list exactly the reasons: %s", errs.String())
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

// runText runs the command with -format text over one file and returns
// what it printed.
func runText(t *testing.T, path string, flags ...string) string {
	t.Helper()
	var out, errs bytes.Buffer
	args := append(append([]string{"-format", "text"}, flags...), path)
	if code := run(context.Background(), args, &out, &errs); code != 0 {
		t.Fatalf("%v: exit %d: %s", args, code, errs.String())
	}
	return out.String()
}

// The command reads HTML and RTF bodies with go-fast-html and go-fast-rtf:
// their text is in the text output.
func TestBodiesReadAsChildren(t *testing.T) {
	dir := t.TempDir()
	files := map[string][]byte{
		"html.eml": []byte("From: a@example.com\nSubject: html\nContent-Type: text/html; charset=utf-8\n\n<html><body><p>Hello <b>from HTML</b></p></body></html>\n"),
		"alt.eml": []byte("From: a@example.com\nSubject: alt\nContent-Type: multipart/alternative; boundary=b\n\n" +
			"--b\nContent-Type: text/plain\n\nPlain version.\n--b\nContent-Type: text/html\n\n<p>Rich version.</p>\n--b--\n"),
		"rtf.eml": []byte("From: a@example.com\nSubject: rtf\nContent-Type: multipart/mixed; boundary=b\n\n" +
			"--b\n\nSee attached.\n--b\nContent-Type: application/rtf\nContent-Disposition: attachment; filename=n.rtf\n\n{\\rtf1 Attached rich text.}\n--b--\n"),
		"html.msg": msgtest.Build(msgtest.Msg{Props: []msgtest.Prop{
			{ID: 0x0037, Value: "html"},
			{ID: 0x1013, Value: []byte("<p>Outlook HTML body.</p>")},
		}}),
		"rtf.msg": msgtest.Build(msgtest.Msg{Props: []msgtest.Prop{
			{ID: 0x0037, Value: "rtf"},
			{ID: 0x1009, Value: rtfLiteral("{\\rtf1 Outlook RTF body.}")},
		}}),
		// The charset only in the MIME header (or PR_INTERNET_CPID), none
		// in the HTML: "\xcf\xf0\xe8\xe2\xe5\xf2" is windows-1251.
		"cp1251.eml": []byte("From: a@example.com\nSubject: cp1251\nContent-Type: text/html; charset=windows-1251\n\n<p>\xcf\xf0\xe8\xe2\xe5\xf2</p>\n"),
		"cp1251.msg": msgtest.Build(msgtest.Msg{Props: []msgtest.Prop{
			{ID: 0x0037, Value: "cp1251"},
			{ID: 0x3FDE, Value: int32(1251)},
			{ID: 0x1013, Value: []byte("<p>\xcf\xf0\xe8\xe2\xe5\xf2</p>")},
		}}),
	}
	want := map[string][]string{
		"html.eml":   {"Hello from HTML", "[child 1"},
		"alt.eml":    {"Plain version.", "Rich version."},
		"rtf.eml":    {"See attached.", "Attached rich text.", "[child 1: n.rtf]"},
		"html.msg":   {"Outlook HTML body.", "[child 1"},
		"rtf.msg":    {"Outlook RTF body.", "[child 1"},
		"cp1251.eml": {"\u041f\u0440\u0438\u0432\u0435\u0442"},
		"cp1251.msg": {"\u041f\u0440\u0438\u0432\u0435\u0442"},
	}
	for name, data := range files {
		path := filepath.Join(dir, name)
		if err := os.WriteFile(path, data, 0o600); err != nil {
			t.Fatal(err)
		}
		text := runText(t, path)
		for _, w := range want[name] {
			if !strings.Contains(text, w) {
				t.Errorf("%s: %q not in text:\n%s", name, w, text)
			}
		}
	}
}

// -children is on by default; -children=false leaves out each child's
// text and the line naming it.
func TestChildrenFlag(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "a.eml")
	eml := "From: a@example.com\nSubject: hello\nContent-Type: multipart/mixed; boundary=b\n\n--b\n\nOuter body.\n" +
		"--b\nContent-Type: text/html\nContent-Disposition: attachment; filename=page.html\n\n<p>Child body.</p>\n--b--\n"
	if err := os.WriteFile(path, []byte(eml), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, flags := range [][]string{nil, {"-children"}, {"-children=true"}} {
		text := runText(t, path, flags...)
		if !strings.Contains(text, "Outer body.") || !strings.Contains(text, "Child body.") || !strings.Contains(text, "[child 1: page.html]") {
			t.Errorf("%v: text:\n%s", flags, text)
		}
	}
	text := runText(t, path, "-children=false")
	if !strings.Contains(text, "Outer body.") || strings.Contains(text, "Child body.") || strings.Contains(text, "[child 1") {
		t.Errorf("-children=false: text:\n%s", text)
	}

	// An HTML-only message: its body is a child.
	html := filepath.Join(dir, "b.eml")
	if err := os.WriteFile(html, []byte("From: a@example.com\nSubject: h\nContent-Type: text/html\n\n<p>Only HTML.</p>\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if text := runText(t, html); !strings.Contains(text, "Only HTML.") || !strings.Contains(text, "[child 1") {
		t.Errorf("default: text:\n%s", text)
	}
	if text := runText(t, html, "-children=false"); strings.Contains(text, "Only HTML.") || strings.Contains(text, "[child 1") {
		t.Errorf("-children=false: text:\n%s", text)
	}
	if text := runText(t, html, "-format", "markdown"); !strings.Contains(text, "Only HTML.") || !strings.Contains(text, "<!-- child 1") {
		t.Errorf("markdown:\n%s", text)
	}
}

// An attachment declared application/octet-stream (or with no type), as
// Outlook stores many (POI's attachment_test_msg.msg: pj1.txt;
// lots-of-recipients.msg: an attached .eml), is read by the extractor its
// file name's extension stands for; other types are not second-guessed.
func TestDispatcherByExtension(t *testing.T) {
	child := dispatcher(&gofastemail.Extractor{})
	for _, c := range []struct{ name, mime, body, format string }{
		{"pj1.txt", "application/octet-stream", "Plain text.", "txt"},
		{"Fwd_ Note.EML", "application/octet-stream", "Subject: inner\n\nInner body.\n", "eml"},
		{"page.htm", "", "<p>Page</p>", "html"},
		{"notes.rtf", "application/octet-stream", "{\\rtf1 Notes}", "rtf"},
	} {
		doc, err := child(context.Background(), c.name, c.mime, strings.NewReader(c.body), int64(len(c.body)), &gofastcore.Options{})
		if err != nil {
			t.Errorf("%s: %v", c.name, err)
			continue
		}
		if doc.Source.Format != c.format {
			t.Errorf("%s: format %q, want %q", c.name, doc.Source.Format, c.format)
		}
	}
	for _, c := range []struct{ name, mime string }{{"x.bin", "application/octet-stream"}, {"x.txt", "application/pdf"}} {
		if _, err := child(context.Background(), c.name, c.mime, strings.NewReader("x"), 1, &gofastcore.Options{}); !errors.Is(err, gofastcore.ErrUnsupported) {
			t.Errorf("%s %s: %v", c.name, c.mime, err)
		}
	}
}

func TestMediaType(t *testing.T) {
	for v, want := range map[string]string{
		"text/html; charset=utf-8": "text/html",
		"Application/RTF":          "application/rtf",
		"text/plain; bad":          "text/plain",
	} {
		if got := mediaType(v); got != want {
			t.Errorf("mediaType(%q) = %q, want %q", v, got, want)
		}
	}
}
