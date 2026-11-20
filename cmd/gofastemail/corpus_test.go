package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/AOShei/go-fast-core"
	"github.com/AOShei/go-fast-email"
)

// corpusRoot is GOFAST_CORPUS, or ../.testdata/corpus from the module root.
func corpusRoot() string {
	if d := os.Getenv("GOFAST_CORPUS"); d != "" {
		return d
	}
	return filepath.Join("..", "..", "..", ".testdata", "corpus")
}

// TestCommandCorpus runs the command's dispatcher (go-fast-html and
// go-fast-rtf for bodies, this library for attached messages) over the
// dev files of the public corpus, each under a time limit and a moderate
// text budget. It fails on a panic, a timeout, a document that does not
// validate, or a child with child.failed. With -v it logs, per file, the
// format, the warnings and every child: its type, format, text size and
// warnings, and the count of U+FFFD in its text.
func TestCommandCorpus(t *testing.T) {
	root := corpusRoot()
	if _, err := os.Stat(root); err != nil {
		t.Skipf("no corpus at %s", root)
	}
	extractor := &gofastemail.Extractor{MaxMessageBytes: 64 << 20}
	child := dispatcher(extractor)
	codes := map[string]int{}
	files := 0
	start := time.Now()
	for _, format := range []string{"eml", "mbox", "msg"} {
		dir := filepath.Join(root, "dev", format)
		entries, err := os.ReadDir(dir)
		if err != nil {
			continue
		}
		for _, ent := range entries {
			if ent.IsDir() {
				continue
			}
			files++
			commandCorpusFile(t, extractor, child, filepath.Join(dir, ent.Name()), codes)
		}
	}
	keys := make([]string, 0, len(codes))
	for k := range codes {
		keys = append(keys, fmt.Sprintf("%s=%d", k, codes[k]))
	}
	sort.Strings(keys)
	t.Logf("dev: files=%d in %v; warnings (children included): %s", files, time.Since(start).Round(time.Millisecond), strings.Join(keys, " "))
}

func commandCorpusFile(t *testing.T, e *gofastemail.Extractor, child gofastcore.ChildFunc, path string, codes map[string]int) {
	name := filepath.Base(path)
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
		opts := &gofastcore.Options{Child: child, Name: name,
			Limits: gofastcore.Limits{MaxTextBytes: 64 << 20, MaxDecodedBytes: 256 << 20}}
		doc, err := e.ExtractFile(ctx, path, opts)
		done <- result{doc: doc, err: err}
	}()
	var res result
	select {
	case res = <-done:
	case <-time.After(12 * time.Second):
		t.Errorf("%s: timeout", name)
		return
	}
	switch {
	case res.panic != nil:
		t.Errorf("%s: panic: %v", name, res.panic)
		return
	case res.err != nil:
		t.Logf("%s: error: %v", name, res.err)
		return
	}
	if err := res.doc.Validate(); err != nil {
		t.Errorf("%s: Validate: %v", name, err)
	}
	var report strings.Builder
	res.doc.Walk(func(d *gofastcore.Document, depth int) bool {
		var ws []string
		for _, w := range d.AllWarnings() {
			codes[w.Code]++
			ws = append(ws, w.Code)
			if w.Code == gofastcore.WarnChildFailed {
				t.Errorf("%s: %s: %s", name, w.Code, w.Message)
			}
		}
		text := d.Text()
		fmt.Fprintf(&report, "\n%s%q %s format=%s parts=%d text=%d fffd=%d warnings=%v",
			strings.Repeat("  ", depth), d.Source.Name, d.Source.MIME, d.Source.Format, len(d.Parts), len(text),
			strings.Count(text, "�"), ws)
		return true
	})
	if testing.Verbose() {
		t.Logf("%s:%s", name, report.String())
	}
}
