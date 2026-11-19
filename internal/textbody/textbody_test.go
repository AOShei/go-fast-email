package textbody

import (
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
	got, cut := Blocks(text, Options{}, all)
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
	got, _ := Blocks(text, Options{Flowed: true}, all)
	check(t, got, []want{
		{gofastcore.KindParagraph, 0, "This is a long paragraph that flows.", ""},
		{gofastcore.KindQuote, 1, "quoted that flows", ""},
		{gofastcore.KindParagraph, 0, "sig", RoleSignature},
	})
	got, _ = Blocks("abc \ndef", Options{Flowed: true, DelSp: true}, all)
	check(t, got, []want{{gofastcore.KindParagraph, 0, "abcdef", ""}})
}

func TestBudget(t *testing.T) {
	n := 0
	got, cut := Blocks("a\n\nb\n\nc", Options{}, func(int) bool { n++; return n <= 2 })
	if !cut || len(got) != 2 {
		t.Fatalf("cut=%v, %d blocks", cut, len(got))
	}
}
