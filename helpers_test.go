package gofastemail_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"testing"
	"unicode"

	"github.com/AOShei/go-fast-core"
	"github.com/AOShei/go-fast-email"
)

// seen is one embedded document the test dispatcher was given.
type seen struct {
	name, mime string
	data       []byte
}

// dispatcher returns a ChildFunc that records what it is given, extracts
// messages with this library, and gives every other child one paragraph
// with its bytes as text (for text) or its size.
func dispatcher(got *[]seen) gofastcore.ChildFunc {
	return func(ctx context.Context, name, mime string, r io.ReaderAt, size int64, opts *gofastcore.Options) (*gofastcore.Document, error) {
		data := make([]byte, size)
		if _, err := r.ReadAt(data, 0); err != nil && !errors.Is(err, io.EOF) {
			return nil, err
		}
		*got = append(*got, seen{name, mime, data})
		if strings.HasPrefix(mime, "message/") {
			return gofastemail.Extract(ctx, r, size, opts)
		}
		if strings.HasPrefix(mime, "application/x-unknown") {
			return nil, gofastcore.ErrUnsupported
		}
		d := gofastcore.New("test")
		p := gofastcore.Part{Kind: gofastcore.PartBody, Number: 1}
		printable := strings.Map(func(r rune) rune {
			if unicode.IsControl(r) {
				return ' '
			}
			return r
		}, strings.ToValidUTF8(string(data), ""))
		if t := strings.TrimSpace(gofastcore.ExpandLigatures(printable)); t != "" && strings.HasPrefix(mime, "text/") {
			p.Blocks = append(p.Blocks, gofastcore.Block{Kind: gofastcore.KindParagraph, Text: strings.Join(strings.Fields(t), " ")})
		}
		p.Finish()
		d.Parts = append(d.Parts, p)
		return d, nil
	}
}

// extract runs Extract on data and checks the result with Validate, and
// that it encodes as JSON.
func extract(t testing.TB, data string, opts *gofastcore.Options) *gofastcore.Document {
	t.Helper()
	doc, err := gofastemail.Extract(context.Background(), bytes.NewReader([]byte(data)), int64(len(data)), opts)
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	checkDoc(t, doc)
	return doc
}

func checkDoc(t testing.TB, doc *gofastcore.Document) {
	t.Helper()
	if err := doc.Validate(); err != nil {
		t.Fatalf("Validate: %v", err)
	}
	if _, err := json.Marshal(doc); err != nil {
		t.Fatalf("json: %v", err)
	}
}

// texts returns the kind and text of every block of the part.
func texts(p *gofastcore.Part) []string {
	var out []string
	for _, b := range p.Blocks {
		out = append(out, b.Kind.String()+": "+b.Text)
	}
	return out
}

func hasWarning(doc *gofastcore.Document, code string) bool {
	for _, w := range doc.AllWarnings() {
		if w.Code == code {
			return true
		}
	}
	return false
}

func equal(t *testing.T, what string, got, want []string) {
	t.Helper()
	if strings.Join(got, "\n|") != strings.Join(want, "\n|") {
		t.Errorf("%s:\ngot  %q\nwant %q", what, got, want)
	}
}
