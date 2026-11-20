package gofastemail_test

import (
	"bytes"
	"context"
	"encoding/binary"
	"runtime"
	"strings"
	"testing"
	"unicode/utf16"

	"github.com/AOShei/go-fast-core"
	"github.com/AOShei/go-fast-email"
	"github.com/AOShei/go-fast-email/internal/msgtest"
)

// utf16Name is a directory entry's name as a compound file stores it.
func utf16Name(s string) []byte {
	var b []byte
	for _, u := range utf16.Encode([]rune(s)) {
		b = binary.LittleEndian.AppendUint16(b, u)
	}
	return b
}

// pointAt makes every directory entry named from share the sectors of the
// entry named to: a stream stored once and read many times.
func pointAt(t *testing.T, data []byte, from, to string) int {
	t.Helper()
	at := bytes.Index(data, utf16Name(to))
	if at < 0 || at%128 != 0 {
		t.Fatalf("no entry %s", to)
	}
	start, size := data[at+116:at+120], data[at+120:at+128]
	n := 0
	name := utf16Name(from)
	for i := 0; ; {
		j := bytes.Index(data[i:], name)
		if j < 0 {
			break
		}
		i += j
		if i%128 == 0 {
			copy(data[i+116:], start)
			copy(data[i+120:], size)
			n++
		}
		i += len(name)
	}
	return n
}

// A compound file can give many properties the same sectors. What is
// stored once is read as often as it is named, and must count against the
// limits each time: 300 recipients whose names are one stream of 512 KiB
// are 150 MiB of names.
func TestMsgSharedStreams(t *testing.T) {
	big := strings.Repeat("A", 256<<10)
	m := msgtest.Msg{Props: []msgtest.Prop{{ID: 0x0037, Value: "Subject"}, {ID: 0x6789, Value: big}}}
	for range 300 {
		m.Recipients = append(m.Recipients, []msgtest.Prop{{ID: 0x3001, Value: "r"}})
	}
	data := msgtest.Build(m)
	if n := pointAt(t, data, "__substg1.0_3001001F", "__substg1.0_6789001F"); n != 300 {
		t.Fatalf("%d entries patched", n)
	}
	var before, after runtime.MemStats
	runtime.ReadMemStats(&before)
	doc, err := gofastemail.Extract(context.Background(), bytes.NewReader(data), int64(len(data)),
		&gofastcore.Options{Limits: gofastcore.Limits{MaxTextBytes: 1 << 20, MaxDecodedBytes: 4 << 20}})
	runtime.ReadMemStats(&after)
	if err != nil {
		t.Fatal(err)
	}
	checkDoc(t, doc)
	if got := after.TotalAlloc - before.TotalAlloc; got > 64<<20 {
		t.Errorf("%d MiB allocated for a file of %d KiB", got>>20, len(data)>>10)
	}
	if !hasWarning(doc, gofastcore.WarnLimitExceeded) {
		t.Errorf("no limit.exceeded: %+v", doc.Parts[0].Warnings)
	}
}

// With a large decode budget, the recipients held before the text budget
// sees them are bounded by what the text budget has left.
func TestMsgSharedStreamsTextBudget(t *testing.T) {
	big := strings.Repeat("A", 256<<10)
	m := msgtest.Msg{Props: []msgtest.Prop{{ID: 0x0037, Value: "Subject"}, {ID: 0x6789, Value: big}}}
	for range 300 {
		m.Recipients = append(m.Recipients, []msgtest.Prop{{ID: 0x3001, Value: "r"}})
	}
	data := msgtest.Build(m)
	pointAt(t, data, "__substg1.0_3001001F", "__substg1.0_6789001F")
	var before, after runtime.MemStats
	runtime.ReadMemStats(&before)
	doc, err := gofastemail.Extract(context.Background(), bytes.NewReader(data), int64(len(data)),
		&gofastcore.Options{Limits: gofastcore.Limits{MaxTextBytes: 1 << 20, MaxDecodedBytes: 1 << 30}})
	runtime.ReadMemStats(&after)
	if err != nil {
		t.Fatal(err)
	}
	checkDoc(t, doc)
	if got := after.TotalAlloc - before.TotalAlloc; got > 64<<20 {
		t.Errorf("%d MiB allocated for a file of %d KiB", got>>20, len(data)>>10)
	}
	if doc.Metadata.Title != "Subject" || !hasWarning(doc, gofastcore.WarnLimitExceeded) {
		t.Errorf("title %q, warnings %+v", doc.Metadata.Title, doc.Parts[0].Warnings)
	}
}
