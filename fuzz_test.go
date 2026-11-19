package gofastemail_test

import (
	"bytes"
	"context"
	"errors"
	"testing"

	"github.com/AOShei/go-fast-core"
	"github.com/AOShei/go-fast-email"
)

// FuzzExtract feeds arbitrary bytes to Extract. Whatever the input, the
// call must return a valid document or an error that wraps one of the
// shared errors; it must not panic, hang or exhaust memory.
func FuzzExtract(f *testing.F) {
	for _, s := range seeds() {
		f.Add([]byte(s))
	}
	e := &gofastemail.Extractor{MaxMessageBytes: 1 << 20}
	f.Fuzz(func(t *testing.T, data []byte) {
		var got []seen
		opts := &gofastcore.Options{Child: dispatcher(&got), Limits: gofastcore.Limits{MaxDecodedBytes: 4 << 20, MaxTextBytes: 4 << 20}}
		doc, err := e.Extract(context.Background(), bytes.NewReader(data), int64(len(data)), opts)
		checkOutcome(t, doc, err)
	})
}

// seeds are the messages of the tests, for the fuzzers.
func seeds() []string {
	return []string{"", simple, alternative, "From: a\n\nb", "From x\nFrom: a\n\nb\n\nFrom y\n\nc",
		"From: a\nContent-Type: multipart/mixed; boundary=b\n\n--b\nContent-Type: message/rfc822\n\nFrom: c\n\nd\n--b--\n",
		"From: a\nContent-Type: text/plain; format=flowed; delsp=yes; charset*=utf-8''x\nContent-Transfer-Encoding: quoted-printable\n\n> a =\n b=3D\n-- \nsig"}
}

func checkOutcome(t *testing.T, doc *gofastcore.Document, err error) {
	t.Helper()
	if err != nil {
		for _, shared := range []error{gofastcore.ErrMalformed, gofastcore.ErrUnsupported, gofastcore.ErrLimitExceeded, gofastcore.ErrNoPartsSelected} {
			if errors.Is(err, shared) {
				return
			}
		}
		t.Fatalf("error wraps no shared error: %v", err)
	}
	if doc == nil {
		t.Fatal("no document and no error")
	}
	if err := doc.Validate(); err != nil {
		t.Fatalf("Validate: %v", err)
	}
}
