package gofastemail_test

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/AOShei/go-fast-core"
	"github.com/AOShei/go-fast-email"
)

// BenchmarkExtract extracts one mailbox, one .eml and one .msg of the
// public corpus's dev set. It skips when the corpus is absent.
func BenchmarkExtract(b *testing.B) {
	for _, name := range []string{
		"dev/mbox/apache-users-tomcat-2025-09.mbox",
		"dev/eml/tika-testEML_embedded_xhtml_and_img.eml",
		"dev/msg/tika-test-outlook2003.msg",
	} {
		data, err := os.ReadFile(filepath.Join(corpusRoot(), name))
		if err != nil {
			b.Skipf("no corpus: %v", err)
		}
		b.Run(filepath.Base(name), func(b *testing.B) {
			b.SetBytes(int64(len(data)))
			b.ReportAllocs()
			for b.Loop() {
				doc, err := gofastemail.Extract(context.Background(), bytes.NewReader(data), int64(len(data)), &gofastcore.Options{})
				if err != nil || len(doc.Parts) == 0 {
					b.Fatal(err)
				}
			}
		})
	}
}
