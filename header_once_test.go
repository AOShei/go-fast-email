package gofastemail_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/AOShei/go-fast-core"
	"github.com/AOShei/go-fast-email"
)

// TestHeaderReadOnce: a file's header is read once, for whether it is a
// message and for the message; where the field lies against the 64 KiB a
// file is looked into for one, and against the header's own limit, the
// answer and the document are what reading it twice gave.
func TestHeaderReadOnce(t *testing.T) {
	// Lines that begin with whitespace before the first field are
	// skipped; a file that begins "From " is a mailbox, read elsewhere.
	long := " " + strings.Repeat("x", 70<<10) + "\n"
	for _, c := range []struct {
		name, data string
		max        int
		isMessage  bool
		fields     int // fields in the metadata
	}{
		{"plain", "A: b\n\nbody\n", 0, true, 1},
		{"field past the look", long + "A: b\n\nbody\n", 0, false, 0},
		{"field after a short skipped line", " x\nA: b\n\nbody\n", 0, true, 1},
		{"field past a small header limit", " " + strings.Repeat("x", 200) + "\nA: b\n\nbody\n", 100, true, 0},
		{"field within a small header limit", "A: b\nC: d\n\nbody\n", 6, true, 2},
		{"no field", "not a header\n\nbody\n", 0, false, 0},
	} {
		doc, err := (&gofastemail.Extractor{MaxHeaderBytes: c.max}).Extract(context.Background(), strings.NewReader(c.data), int64(len(c.data)), &gofastcore.Options{})
		if (err == nil) != c.isMessage {
			t.Errorf("%s: error %v", c.name, err)
			continue
		}
		if err != nil {
			continue
		}
		if len(doc.Metadata.Extra) != c.fields {
			b, _ := json.Marshal(doc.Metadata)
			t.Errorf("%s: metadata %s", c.name, b)
		}
	}
}
