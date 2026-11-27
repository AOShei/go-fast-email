// Package clean makes extracted strings follow the text conventions of
// the go-fast model, through go-fast-core's clean, which every library
// shares: no control characters other than tab and newline, line breaks
// (CR LF, CR, vertical tab, form feed, NEL, the line and paragraph
// separators) as "\n", and the presentation-form ligatures written as
// their letters where the string is text. Line and Cut are mail's own.
package clean

import (
	"strings"
	"unicode/utf8"

	core "github.com/AOShei/go-fast-core/clean"
)

// Text returns s with line breaks as "\n", every other control character
// removed, and ligatures expanded.
func Text(s string) string {
	t, _ := TextN(s)
	return t
}

// TextN is Text, and the number of control characters removed: not
// counting tab and line breaks, which are kept.
func TextN(s string) (string, int) {
	return core.String([]byte(s), core.Ligatures)
}

// LineN is Line, and the number of control characters removed: not
// counting tab and line breaks, which become spaces.
func LineN(s string) (string, int) {
	t, n := core.String([]byte(s), core.Verbatim)
	return line(t), n
}

// Raw is Text without the expansion of ligatures, for strings that
// identify something: an address, a message id, a file name.
func Raw(s string) string {
	t, _ := core.String([]byte(s), core.Verbatim)
	return t
}

// RawN is Raw, and the number of control characters removed, as TextN.
func RawN(s string) (string, int) { return core.String([]byte(s), core.Verbatim) }

// Line returns s as one line: line breaks and tabs as spaces, runs of
// spaces as one, trimmed. For a header value, a part name or a file name.
func Line(s string) string {
	return line(Raw(s))
}

func line(s string) string {
	if strings.ContainsAny(s, "\n\t") {
		s = strings.Join(strings.Fields(s), " ")
	}
	return strings.TrimSpace(s)
}

// Cut returns s cut to at most n bytes, at a character boundary.
func Cut(s string, n int) string {
	if len(s) <= n {
		return s
	}
	for n > 0 && !utf8.RuneStart(s[n]) {
		n--
	}
	return s[:n]
}
