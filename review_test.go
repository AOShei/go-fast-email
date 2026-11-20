package gofastemail_test

import (
	"bytes"
	"context"
	"encoding/binary"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"
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

// A header given many times has its values joined in Metadata.Extra;
// joining them one at a time copies what is joined so far each time.
func TestRepeatedHeaderIsLinear(t *testing.T) {
	data := "From: a\n" + strings.Repeat("X-A: "+strings.Repeat("v", 90)+"\n", 9_000) + "\nbody\n"
	var before, after runtime.MemStats
	runtime.ReadMemStats(&before)
	doc := extract(t, data, nil)
	runtime.ReadMemStats(&after)
	if got := after.TotalAlloc - before.TotalAlloc; got > 64<<20 {
		t.Errorf("%d MiB allocated for a header section of %d KiB", got>>20, len(data)>>10)
	}
	if n := strings.Count(doc.Metadata.Extra["x-a"], "\n"); n != 8_999 {
		t.Errorf("%d values joined", n+1)
	}
}

// Encoded words that never end: each start of one must not search the
// whole rest of the header for its end.
func TestUnendedEncodedWords(t *testing.T) {
	data := "From: a\nSubject: " + strings.Repeat("=?a?q?b ", 120_000) + "\n\nbody\n"
	start := time.Now()
	extract(t, data, nil)
	if d := time.Since(start); d > time.Second {
		t.Errorf("%v for a header of %d KiB", d, len(data)>>10)
	}
}

// Many recipients: finding each one's properties must not walk every
// entry of the directory each time.
func TestMsgManyRecipients(t *testing.T) {
	m := msgtest.Msg{Props: []msgtest.Prop{{ID: 0x0037, Value: "Subject"}}}
	for range 10_000 {
		m.Recipients = append(m.Recipients, []msgtest.Prop{{ID: 0x3001, Value: "r"}})
	}
	data := msgtest.Build(m)
	start := time.Now()
	doc, err := gofastemail.Extract(context.Background(), bytes.NewReader(data), int64(len(data)), nil)
	if err != nil {
		t.Fatal(err)
	}
	checkDoc(t, doc)
	if d := time.Since(start); d > 2*time.Second {
		t.Errorf("%v for %d recipients in %d KiB", d, len(m.Recipients), len(data)>>10)
	}
}

// The headers a mail program does not show are written with ligatures as
// their letters too: in a mailbox's note of headers, and in the note of
// an outlier header.
func TestHeaderNotesExpandLigatures(t *testing.T) {
	long := strings.Repeat("word ", 40)
	ffi := string(rune(0xFB03)) // the ligature ffi
	for _, data := range []string{
		"From: a\nX-Note: o" + ffi + "ce " + long + "\n\nbody\n",
		"From x\nFrom: a\nX-Note: o" + ffi + "ce\n\nbody\n",
	} {
		doc := extract(t, data, nil)
		found := false
		for _, b := range doc.Parts[0].Blocks {
			if strings.Contains(b.Text, "office") {
				found = true
			}
		}
		if !found {
			t.Errorf("%q: %q", data[:20], texts(&doc.Parts[0]))
		}
	}
}

// A mailbox of many tiny messages: looking at each message's header for a
// Content-Length must cost about the header, not a fixed 64 KiB.
func TestMboxTinyMessages(t *testing.T) {
	data := strings.Repeat("From x\nA: b\n\n", 400_000)
	var before, after runtime.MemStats
	runtime.ReadMemStats(&before)
	doc := extract(t, data, &gofastcore.Options{Parts: gofastcore.PartRange{First: 400_000}})
	runtime.ReadMemStats(&after)
	if len(doc.Parts) != 1 {
		t.Fatalf("%d parts", len(doc.Parts))
	}
	if got := after.TotalAlloc - before.TotalAlloc; got > 256<<20 {
		t.Errorf("%d MiB allocated for a mailbox of %d KiB", got>>20, len(data)>>10)
	}
	// A header longer than the first look still gives its Content-Length.
	body := "one\n\nFrom inside the body\n"
	msg := "From a\nX-Long: " + strings.Repeat("x", 3000) + "\nContent-Length: " + strconv.Itoa(len(body)) + "\n\n" + body
	doc = extract(t, msg+"\n"+msg, nil)
	if len(doc.Parts) != 2 {
		t.Errorf("%d parts", len(doc.Parts))
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
