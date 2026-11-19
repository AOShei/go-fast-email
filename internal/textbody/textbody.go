// Package textbody turns the text of a text/plain body into blocks.
//
// Paragraphs are separated by blank lines. A line break inside a
// paragraph is kept as a hard break, "\n": in plain-text mail the sender's
// line breaks are what a reader sees, and a list or an address is broken
// into lines on purpose. Only where the sender marks a break as soft, with
// format=flowed (RFC 3676), are lines joined.
//
// Lines quoted with ">" are quote blocks with the depth of their quoting.
// The signature, which follows a line "-- ", is kept as paragraphs marked
// with Attrs["role"] = "signature".
package textbody

import (
	"strings"

	"github.com/AOShei/go-fast-core"
)

// RoleSignature is Attrs["role"] of a block of the signature.
const RoleSignature = "signature"

// MaxQuoteDepth bounds the depth of a quote block; deeper quoting is given
// this depth, with the ">" marks beyond it kept in the text.
const MaxQuoteDepth = 64

// Options say how the body is encoded.
type Options struct {
	Flowed bool // format=flowed
	DelSp  bool // delsp=yes: the space before a soft break is not text
}

// line is one logical line: its quote depth and text.
type line struct {
	depth int
	text  string
	sig   bool // the signature separator
}

// Blocks returns the blocks of text, which uses "\n" for line breaks and
// has no other control characters. take is called with the cost of each
// block before it is added (gofastcore.BlockCost and the text); when it
// returns false no more blocks are made, and Blocks reports that it was
// cut short.
func Blocks(text string, o Options, take func(int) bool) ([]gofastcore.Block, bool) {
	lines := split(text, o)
	var blocks []gofastcore.Block
	var para []string
	depth, inSig := 0, false
	flush := func() bool {
		if len(para) == 0 {
			return true
		}
		t := strings.TrimSpace(strings.Join(para, "\n"))
		para = para[:0]
		if t == "" {
			return true
		}
		b := gofastcore.Block{Kind: gofastcore.KindParagraph, Text: t}
		switch {
		case depth > 0:
			b.Kind, b.Level = gofastcore.KindQuote, uint8(depth)
		case inSig:
			b.Attrs = map[string]string{"role": RoleSignature}
		}
		if !take(gofastcore.BlockCost + len(t)) {
			return false
		}
		blocks = append(blocks, b)
		return true
	}
	for _, l := range lines {
		if l.sig && l.depth == 0 && !inSig {
			if !flush() {
				return blocks, true
			}
			inSig = true
			continue
		}
		if strings.TrimSpace(l.text) == "" || l.depth != depth {
			if !flush() {
				return blocks, true
			}
			depth = l.depth
			if strings.TrimSpace(l.text) == "" {
				continue
			}
		}
		para = append(para, strings.TrimRight(l.text, " \t"))
	}
	if !flush() {
		return blocks, true
	}
	return blocks, false
}

// split returns the logical lines of text: with their quote marks counted
// and removed, and, for format=flowed, with soft breaks joined.
func split(text string, o Options) []line {
	raw := strings.Split(text, "\n")
	out := make([]line, 0, len(raw))
	joining := false // the previous line ended in a soft break
	for _, s := range raw {
		depth, rest := quoteDepth(s, o.Flowed)
		sig := rest == "-- " || rest == "--"
		if o.Flowed {
			// Space-stuffing (RFC 3676, 4.4): one leading space is not
			// text.
			rest = strings.TrimPrefix(rest, " ")
		}
		soft := o.Flowed && !sig && strings.HasSuffix(rest, " ")
		if soft && o.DelSp {
			rest = rest[:len(rest)-1]
		}
		if joining && len(out) > 0 && out[len(out)-1].depth == depth && !sig {
			out[len(out)-1].text += rest
		} else {
			out = append(out, line{depth: depth, text: rest, sig: sig && !joining})
		}
		joining = soft
	}
	return out
}

// quoteDepth counts the ">" marks that begin s. In flowed text they are
// contiguous (RFC 3676, 4.5); in other text, as people and programs write
// them, they may be separated by spaces ("> > text"), and the space after
// the last one is not text.
func quoteDepth(s string, flowed bool) (int, string) {
	depth := 0
	i := 0
	for i < len(s) && depth < MaxQuoteDepth {
		if s[i] == '>' {
			depth++
			i++
			continue
		}
		if !flowed && s[i] == ' ' && i+1 < len(s) && s[i+1] == '>' && depth > 0 {
			i++
			continue
		}
		break
	}
	rest := s[i:]
	if depth > 0 && !flowed {
		rest = strings.TrimPrefix(rest, " ")
	}
	return depth, rest
}
