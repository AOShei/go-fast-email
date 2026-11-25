// Package clean makes extracted strings follow the text conventions of
// the go-fast model: no control characters other than tab and newline,
// line breaks as "\n", and the presentation-form ligatures written as
// their letters where the string is text.
package clean

import (
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/AOShei/go-fast-core"
)

// Text returns s with "\r\n" and a lone "\r" as "\n", every other control
// character removed, and ligatures expanded.
func Text(s string) string {
	return gofastcore.ExpandLigatures(Raw(s))
}

// TextN is Text, and the number of control characters removed: not
// counting tab and line ends, which are kept.
func TextN(s string) (string, int) {
	t, n := raw(s)
	return gofastcore.ExpandLigatures(t), n
}

// LineN is Line, and the number of control characters removed: not
// counting tab and line ends, which become spaces.
func LineN(s string) (string, int) {
	t, n := raw(s)
	return line(t), n
}

// Raw is Text without the expansion of ligatures, for strings that
// identify something: an address, a message id, a file name.
func Raw(s string) string {
	t, _ := raw(s)
	return t
}

// RawN is Raw, and the number of control characters removed, as TextN.
func RawN(s string) (string, int) { return raw(s) }

func raw(s string) (string, int) {
	if !needs(s) {
		return s, 0
	}
	removed := 0
	var sb strings.Builder
	sb.Grow(len(s))
	for i := 0; i < len(s); {
		r, n := utf8.DecodeRuneInString(s[i:])
		switch {
		case r == utf8.RuneError && n == 1:
			sb.WriteString("�")
		case r == '\r':
			sb.WriteByte('\n')
			if i+1 < len(s) && s[i+1] == '\n' {
				i++
			}
		case r == '\n' || r == '\t':
			sb.WriteRune(r)
		case unicode.Is(unicode.Cc, r):
			removed++
		default:
			sb.WriteString(s[i : i+n])
		}
		i += n
	}
	return sb.String(), removed
}

func needs(s string) bool {
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c < 0x20 && c != '\n' && c != '\t' || c == 0x7F || c >= 0x80 {
			if c >= 0x80 {
				// C1 controls are U+0080 to U+009F, encoded as C2 80 to C2 9F.
				if c == 0xC2 && i+1 < len(s) && s[i+1] >= 0x80 && s[i+1] < 0xA0 {
					return true
				}
				continue
			}
			return true
		}
	}
	return !utf8.ValidString(s)
}

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
