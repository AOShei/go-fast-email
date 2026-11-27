package gofastemail_test

import (
	"strings"
	"testing"

	gofastcore "github.com/AOShei/go-fast-core"
)

// TestControlsRemovedCounted: control characters with no meaning as text,
// in a header value and the body, are removed and counted; line ends,
// tabs, the folding of a header and the form feed (a page separator, a
// line break in go-fast-core's clean) are not.
func TestControlsRemovedCounted(t *testing.T) {
	src := "From: A\x01n <a@example.com>\r\nSubject: Hel\x00lo\r\n there\r\nContent-Type: text/plain\r\n\r\nline\x1bone\r\n\tline two\x0c\r\n"
	doc := extract(t, src, nil)
	var got []string
	for _, w := range doc.Parts[0].Warnings {
		if w.Code == gofastcore.WarnTextControlCharactersRemoved {
			got = append(got, w.Message)
		}
	}
	want := "3 control characters with no meaning as text were removed from the message"
	if len(got) != 1 || got[0] != want {
		t.Errorf("%q, want [%q]", got, want)
	}
	text := doc.Text()
	if strings.ContainsAny(text, "\x00\x01\x1b\x0c") || !strings.Contains(text, "lineone") {
		t.Errorf("text %q", text)
	}
	if clean := extract(t, "Subject: plain\r\n\r\nbody\r\n\tline\r\n", nil); len(clean.Parts[0].Warnings) != 0 {
		t.Errorf("no control characters: %v", clean.Parts[0].Warnings)
	}
}
