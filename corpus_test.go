package gofastemail_test

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/AOShei/go-fast-core"
	"github.com/AOShei/go-fast-email"
)

// corpusRoot is GOFAST_CORPUS, or ../.testdata/corpus.
func corpusRoot() string {
	if d := os.Getenv("GOFAST_CORPUS"); d != "" {
		return d
	}
	return filepath.Join("..", ".testdata", "corpus")
}

// tally counts what came out of one set of files.
type tally struct {
	files, panics, timeouts, invalid int
	errors, hidden, warnings         map[string]int
}

func (t *tally) add(m map[string]int, k string) { m[k]++ }

func sorted(m map[string]int) string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var b strings.Builder
	for _, k := range keys {
		fmt.Fprintf(&b, " %s=%d", k, m[k])
	}
	return b.String()
}

// TestCorpus runs the extractor over the public corpus: every eml, mbox
// and msg file under dev/ and heldout/, each with a time limit and a
// moderate text budget. It logs a tally per set and fails on a panic, a
// Validate failure or a timeout. For dev files it also logs every hidden
// flag with an excerpt, for review; for held-out files only counts.
func TestCorpus(t *testing.T) {
	root := corpusRoot()
	if _, err := os.Stat(root); err != nil {
		t.Skipf("no corpus at %s", root)
	}
	e := &gofastemail.Extractor{MaxMessageBytes: 64 << 20}
	child := func(ctx context.Context, name, mime string, r io.ReaderAt, size int64, o *gofastcore.Options) (*gofastcore.Document, error) {
		switch strings.ToLower(mime) {
		case "message/rfc822", "message/global", "application/vnd.ms-outlook":
			return e.Extract(ctx, r, size, o)
		}
		return nil, fmt.Errorf("no extractor for %q: %w", mime, gofastcore.ErrUnsupported)
	}
	for _, set := range []string{"dev", "heldout"} {
		for _, format := range []string{"eml", "mbox", "msg"} {
			dir := filepath.Join(root, set, format)
			entries, err := os.ReadDir(dir)
			if err != nil {
				continue
			}
			tl := &tally{errors: map[string]int{}, hidden: map[string]int{}, warnings: map[string]int{}}
			for _, ent := range entries {
				if ent.IsDir() {
					continue
				}
				tl.files++
				path := filepath.Join(dir, ent.Name())
				label := ent.Name()
				if set == "heldout" {
					label = fmt.Sprintf("held-out file %d", tl.files) // never name one
				}
				corpusFile(t, e, child, path, label, set == "dev", tl)
			}
			t.Logf("%s/%s: files=%d panics=%d timeouts=%d invalid=%d\n  errors:%s\n  hidden:%s\n  warnings:%s",
				set, format, tl.files, tl.panics, tl.timeouts, tl.invalid, sorted(tl.errors), sorted(tl.hidden), sorted(tl.warnings))
		}
	}
}

func corpusFile(t *testing.T, e *gofastemail.Extractor, child gofastcore.ChildFunc, path, label string, dev bool, tl *tally) {
	type result struct {
		doc   *gofastcore.Document
		err   error
		panic any
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	done := make(chan result, 1)
	go func() {
		defer func() {
			if p := recover(); p != nil {
				done <- result{panic: p}
			}
		}()
		f, err := os.Open(path)
		if err != nil {
			done <- result{err: err}
			return
		}
		defer f.Close()
		info, err := f.Stat()
		if err != nil {
			done <- result{err: err}
			return
		}
		opts := &gofastcore.Options{Child: child, Name: info.Name(),
			Limits: gofastcore.Limits{MaxTextBytes: 64 << 20, MaxDecodedBytes: 256 << 20}}
		doc, err := e.Extract(ctx, f, info.Size(), opts)
		done <- result{doc: doc, err: err}
	}()
	var res result
	select {
	case res = <-done:
	case <-time.After(12 * time.Second):
		tl.timeouts++
		t.Errorf("%s: timeout", label)
		return
	}
	switch {
	case res.panic != nil:
		tl.panics++
		if dev {
			t.Errorf("%s: panic: %v", label, res.panic)
		} else {
			t.Errorf("%s: panic", label)
		}
		return
	case errors.Is(res.err, context.DeadlineExceeded):
		tl.timeouts++
		t.Errorf("%s: timeout", label)
		return
	case res.err != nil:
		kind := "other"
		for _, k := range []struct {
			err  error
			name string
		}{{gofastcore.ErrMalformed, "malformed"}, {gofastcore.ErrUnsupported, "unsupported"}, {gofastcore.ErrLimitExceeded, "limit"},
			{gofastcore.ErrNoPartsSelected, "no_parts"}, {gofastcore.ErrPasswordRequired, "password"}} {
			if errors.Is(res.err, k.err) {
				kind = k.name
				break
			}
		}
		tl.add(tl.errors, kind)
		if dev {
			t.Logf("%s: error: %v", label, res.err)
		}
		return
	}
	if err := res.doc.Validate(); err != nil {
		tl.invalid++
		if dev {
			t.Errorf("%s: Validate: %v", label, err)
		} else {
			t.Errorf("%s: Validate failed", label)
		}
	}
	var walk func(d *gofastcore.Document)
	walk = func(d *gofastcore.Document) {
		for _, w := range d.AllWarnings() {
			tl.add(tl.warnings, w.Code)
		}
		for p, b := range d.AllBlocks() {
			if b.Hidden == 0 {
				continue
			}
			tl.add(tl.hidden, b.Hidden.String())
			if dev {
				excerpt := []rune(strings.Join(strings.Fields(b.Text), " "))
				if len(excerpt) > 60 {
					excerpt = excerpt[:60]
				}
				t.Logf("FLAG %s part %d %s %s: %q", label, p.Number, b.Kind, b.Hidden, string(excerpt))
			}
		}
		for _, c := range d.Children {
			walk(c)
		}
	}
	walk(res.doc)
}
