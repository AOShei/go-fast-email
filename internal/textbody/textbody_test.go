package textbody

import (
	"runtime"
	"strings"
	"testing"

	"github.com/AOShei/go-fast-core"
)

func all(int) bool { return true }

type want struct {
	kind  gofastcore.Kind
	level uint8
	text  string
	role  string
}

func check(t *testing.T, got []gofastcore.Block, w []want) {
	t.Helper()
	if len(got) != len(w) {
		t.Fatalf("got %d blocks, want %d: %+v", len(got), len(w), got)
	}
	for i := range w {
		if got[i].Kind != w[i].kind || got[i].Level != w[i].level || got[i].Text != w[i].text || got[i].Attrs["role"] != w[i].role {
			t.Errorf("block %d = %v %d %q %q, want %+v", i, got[i].Kind, got[i].Level, got[i].Text, got[i].Attrs["role"], w[i])
		}
	}
}

func TestPlain(t *testing.T) {
	text := "Hello Bob,\n\nFirst line\nsecond line  \n\nOn Monday, Alice wrote:\n> quoted one\n> > deeper\n>\n> back\n\n-- \nAlice\nExample Ltd\n"
	got, cut := Blocks(nil, text, Options{}, all)
	if cut {
		t.Fatal("cut")
	}
	check(t, got, []want{
		{gofastcore.KindParagraph, 0, "Hello Bob,", ""},
		{gofastcore.KindParagraph, 0, "First line\nsecond line", ""},
		{gofastcore.KindParagraph, 0, "On Monday, Alice wrote:", ""},
		{gofastcore.KindQuote, 1, "quoted one", ""},
		{gofastcore.KindQuote, 2, "deeper", ""},
		{gofastcore.KindQuote, 1, "back", ""},
		{gofastcore.KindParagraph, 0, "Alice\nExample Ltd", RoleSignature},
	})
}

func TestFlowed(t *testing.T) {
	text := "This is a long \nparagraph that \nflows.\n\n> quoted that \n> flows\n>> \n-- \nsig"
	got, _ := Blocks(nil, text, Options{Flowed: true}, all)
	check(t, got, []want{
		{gofastcore.KindParagraph, 0, "This is a long paragraph that flows.", ""},
		{gofastcore.KindQuote, 1, "quoted that flows", ""},
		{gofastcore.KindParagraph, 0, "sig", RoleSignature},
	})
	got, _ = Blocks(nil, "abc \ndef", Options{Flowed: true, DelSp: true}, all)
	check(t, got, []want{{gofastcore.KindParagraph, 0, "abcdef", ""}})
}

// TestWorstCase joins half a million flowed lines; joining them one by one
// into a string would take minutes.
func TestWorstCase(t *testing.T) {
	text := strings.Repeat("ab \n", 500_000) + "end"
	got, _ := Blocks(nil, text, Options{Flowed: true}, all)
	if len(got) != 1 || len(got[0].Text) != 3*500_000+3 {
		t.Fatalf("%d blocks", len(got))
	}
}

// TestLineBreaksCostNothing: a body of line breaks makes no blocks, and
// reading it must not hold anything for each line.
func TestLineBreaksCostNothing(t *testing.T) {
	for _, text := range []string{strings.Repeat("\n", 8<<20), strings.Repeat(">\n", 4<<20), strings.Repeat("x \n", 3<<20)} {
		var before, after runtime.MemStats
		runtime.ReadMemStats(&before)
		got, _ := Blocks(nil, text, Options{Flowed: true}, all)
		runtime.ReadMemStats(&after)
		if alloc := after.TotalAlloc - before.TotalAlloc; alloc > uint64(5*len(text))+1<<20 { // a builder's growth, not a cost per line
			t.Errorf("%d bytes of input, %d allocated, %d blocks", len(text), alloc, len(got))
		}
	}
}

func TestBudget(t *testing.T) {
	n := 0
	got, cut := Blocks(nil, "a\n\nb\n\nc", Options{}, func(int) bool { n++; return n <= 2 })
	if !cut || len(got) != 2 {
		t.Fatalf("cut=%v, %d blocks", cut, len(got))
	}
}
