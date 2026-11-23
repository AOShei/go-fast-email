package rfc822

import (
	"strings"
	"unicode/utf8"

	"github.com/AOShei/go-fast-email/internal/charset"
)

// CharsetFunc is told of every decoding that did not go cleanly, or was
// decided from the bytes: a character set this package does not know,
// bytes that are not valid in the one declared, or bytes outside ASCII in
// a header that declares no character set (an empty label), read in the
// set recognised (res.Detected) or not (res.Undetermined).
type CharsetFunc func(label string, res charset.Result)

// Decoding is how header text is decoded: the character set of text that
// declares none (Extractor.Charset; empty for none), and who is told of
// what decoding found. A nil *Decoding is no default and no report.
type Decoding struct {
	Default string
	Report  CharsetFunc
}

func (d *Decoding) report(label string, res charset.Result) {
	if d != nil && d.Report != nil {
		d.Report(label, res)
	}
}

// undeclared decodes text that declares no character set.
func (d *Decoding) undeclared(raw []byte) (string, charset.Result) {
	def := ""
	if d != nil {
		def = d.Default
	}
	return charset.DecodeDefault("", def, raw)
}

// DecodeText returns a header value as text: its bytes as UTF-8, with the
// encoded words of RFC 2047 decoded, and the whitespace between two
// adjacent encoded words removed as that RFC has it. Bytes outside ASCII
// outside an encoded word are taken as UTF-8 (RFC 6532); where they are
// not valid UTF-8 they become U+FFFD and report is told. Encoded words
// that a bound of this package keeps from being decoded are left as
// written and counted in the Cuts returned.
func DecodeText(raw []byte, d *Decoding) (string, Cuts) {
	var cuts Cuts
	var s string
	if utf8.Valid(raw) {
		s = string(raw)
	} else {
		var res charset.Result
		s, res = d.undeclared(raw)
		d.report("", res)
	}
	if !strings.Contains(s, "=?") {
		return s, cuts
	}
	var sb strings.Builder
	sb.Grow(len(s))
	pending := "" // whitespace after an encoded word, written only if text follows
	afterWord := false
	// What is known of the text, by offset in the whole of it: off is
	// where s begins; [cleanFrom, cleanTo) holds no whitespace and no "?="
	// that begins before cleanTo-1, as found while looking for the end of
	// a word, so that the words that begin within it do not search it
	// again; cutTo is where the bound of the last cut word counted ends.
	off, cleanFrom, cleanTo, cutTo := 0, 0, 0, 0
	for len(s) > 0 {
		i := strings.Index(s, "=?")
		if i < 0 {
			sb.WriteString(pending)
			sb.WriteString(s)
			break
		}
		at := off + i
		w := encodedWord(s[i:], d, cleanFrom-at, cleanTo-at)
		if w.clean > 0 {
			from, to := at+w.textAt, at+w.clean
			if from >= cleanFrom && from <= cleanTo {
				cleanTo = max(cleanTo, to)
			} else {
				cleanFrom, cleanTo = from, to
			}
		}
		switch w.cut {
		case cutWord:
			// A word that begins within the bound of one counted is
			// part of the same run: a long run with "=?" all through it
			// is one cut word per bound, not one per "=?".
			if at >= cutTo {
				cuts.LongWords++
				cutTo = at + w.clean
			}
		case cutLabel:
			cuts.LongLabels++
		}
		if !w.ok {
			sb.WriteString(pending)
			pending = ""
			skip := i + 2
			if w.cut == cutNone && w.rest != "" {
				// Not a word, and no word begins before rest.
				skip = len(s) - len(w.rest)
			}
			sb.WriteString(s[:skip])
			s, off = s[skip:], off+skip
			afterWord = false
			continue
		}
		gap := s[:i]
		if afterWord && strings.Trim(gap, " \t\r\n") == "" {
			gap = "" // between two encoded words
		} else {
			sb.WriteString(pending)
		}
		pending = ""
		sb.WriteString(gap)
		sb.WriteString(w.text)
		off += len(s) - len(w.rest)
		s = w.rest
		afterWord = true
		// Whitespace that follows is held back until it is known whether
		// another encoded word follows it.
		j := 0
		for j < len(s) && (s[j] == ' ' || s[j] == '\t' || s[j] == '\r' || s[j] == '\n') {
			j++
		}
		pending, s, off = s[:j], s[j:], off+j
	}
	return sb.String(), cuts
}

// The bounds on an encoded word.
const (
	// MaxWord bounds the encoded text of an encoded word. The longest
	// word RFC 2047 allows is 75 characters; longer ones are written by
	// some programs, so the bound here is looser.
	MaxWord = 4096
	// MaxLabel bounds the character set name of an encoded word.
	MaxLabel = 100
)

// What kept an encoded word from being decoded, where it was a bound.
const (
	cutNone = iota
	cutWord
	cutLabel
)

// word is what encodedWord finds at a "=?".
type word struct {
	text string // the decoded text, if ok
	// rest: what follows the word, if ok; where it is not a word and no
	// word can begin before some later point, the text from that point;
	// otherwise empty.
	rest string
	ok   bool
	cut  int // what bound kept it from being decoded, if one did
	// textAt is the offset of the encoded text; clean, if not 0, the
	// offset up to which the text from textAt holds no whitespace and no
	// "?=" that begins before clean-1.
	textAt, clean int
}

// encodedWord decodes the encoded word "=?charset?enc?text?=" at the start
// of s. Where it is not decoded because of MaxWord or MaxLabel, and not
// because of its form, cut says which. [from, to), offsets in s, is known
// to hold no whitespace and no "?=" that begins before to-1; the search
// for the end does not look there again.
func encodedWord(s string, d *Decoding, from, to int) word {
	body := s[2:]
	q1 := strings.IndexByte(body, '?')
	if q1 <= 0 || len(body) < q1+3 || body[q1+2] != '?' {
		return word{}
	}
	label, enc := body[:q1], body[q1+1]
	if enc != 'B' && enc != 'b' && enc != 'Q' && enc != 'q' || strings.ContainsAny(label, " \t\"") {
		return word{}
	}
	text := body[q1+3:]
	textAt := 2 + q1 + 3
	// The end is looked for no further than a word may be long, so that
	// words that never end do not each search the rest of the text.
	window := text[:min(len(text), MaxWord+2)]
	k := 0 // where the search begins: what is before it is known
	if from <= textAt && to-1 > textAt {
		k = min(to-1-textAt, len(window))
	}
	end := strings.Index(window[k:], "?=")
	if end < 0 {
		end = len(window)
	} else {
		end += k
	}
	if strings.ContainsAny(text[k:end], " \t\r\n") {
		// Whitespace before any "?=": not a word. Nor does a word begin
		// before a space or tab there, since no part of a word holds one
		// (a line break may be in a label) and no word could end before
		// it. Unless one begins within "=?label?E?", the caller goes on
		// from the space or tab, not from 2 bytes on.
		w := strings.IndexAny(text[k:end], " \t")
		if w < 0 || strings.Contains(s[2:textAt], "=?") {
			return word{}
		}
		return word{rest: text[k+w:]}
	}
	if end == len(window) {
		// No end within the bound, and nothing a word may not hold.
		clean := textAt + len(window)
		if len(window) == MaxWord+2 {
			return word{cut: cutWord, textAt: textAt, clean: clean} // it runs on past the bound
		}
		return word{textAt: textAt, clean: clean} // it never ends
	}
	if q1 > MaxLabel {
		return word{cut: cutLabel}
	}
	var raw []byte
	if enc == 'B' || enc == 'b' {
		raw = Base64([]byte(text[:end]))
	} else {
		raw = qword(text[:end])
	}
	out, res := charset.Decode(label, raw)
	if !res.Known || res.Replaced || res.Detected != "" {
		d.report(charset.Normalize(label), res)
	}
	return word{text: out, rest: text[end+2:], ok: true}
}

// qword decodes the Q encoding of an encoded word: "_" is a space and
// "=XX" a byte.
func qword(s string) []byte {
	out := make([]byte, 0, len(s))
	for i := 0; i < len(s); i++ {
		switch c := s[i]; {
		case c == '_':
			out = append(out, ' ')
		case c == '=' && i+2 < len(s) && isHex(s[i+1]) && isHex(s[i+2]):
			out = append(out, unhex(s[i+1])<<4|unhex(s[i+2]))
			i += 2
		default:
			out = append(out, c)
		}
	}
	return out
}

func isHex(c byte) bool {
	return c >= '0' && c <= '9' || c >= 'a' && c <= 'f' || c >= 'A' && c <= 'F'
}

func unhex(c byte) byte {
	switch {
	case c >= '0' && c <= '9':
		return c - '0'
	case c >= 'a' && c <= 'f':
		return c - 'a' + 10
	}
	return c - 'A' + 10
}
