package rfc822

import (
	"sort"
	"strconv"
	"strings"

	"github.com/AOShei/go-fast-email/internal/charset"
)

// MaxParams bounds the parameters read from one header. Past it only the
// parameters this library reads (Used) are kept.
const MaxParams = 64

// MaxPiece is the highest number of an RFC 2231 piece ("name*999") read.
const MaxPiece = 999

// Used lists the parameters the reader of a message uses: past MaxParams
// these are still read, the others are not.
var Used = []string{"boundary", "charset", "name", "filename", "format", "delsp", "access-type", "url", "site"}

func used(name string) bool {
	for _, u := range Used {
		if u == name {
			return true
		}
	}
	return false
}

// Cuts counts what a bound of this package left unread or undecoded.
type Cuts struct {
	// LongWords: encoded words that do not end within MaxWord bytes, left
	// as written.
	LongWords int
	// LongLabels: encoded words whose character set name is longer than
	// MaxLabel bytes, left as written.
	LongLabels int
	// Pieces: RFC 2231 pieces numbered above MaxPiece, of a parameter
	// in Used, not read.
	Pieces int
}

// Add adds the counts of o to c.
func (c *Cuts) Add(o Cuts) {
	c.LongWords += o.LongWords
	c.LongLabels += o.LongLabels
	c.Pieces += o.Pieces
}

// Params reads a header that has the form of Content-Type or
// Content-Disposition: a value, then parameters "; name=value". It returns
// the value in lowercase and the parameters by lowercase name.
//
// It is lenient where mail is: a missing or doubled ";", a value that is
// neither a token nor a quoted string, a parameter given twice (the first
// is kept). Parameters split by RFC 2231 into "name*0", "name*1", ... are
// joined, and the extended form "name*=charset'lang'%XX" is decoded. An
// encoded word inside a quoted value (which RFC 2047 forbids and Outlook
// writes) is decoded too.
//
// Past MaxParams parameters only those in Used are read, to the end of
// the header: the others are not used, so nothing is lost. RFC 2231
// pieces past MaxPiece and encoded words left as written are counted in
// the Cuts returned.
func Params(raw []byte, d *Decoding) (string, map[string]string, Cuts) {
	var cuts Cuts
	s := DecodeRaw(raw, d)
	value, rest, _ := strings.Cut(s, ";")
	value = strings.ToLower(strings.TrimSpace(value))
	if i := strings.IndexAny(value, " \t("); i >= 0 {
		value = value[:i]
	}
	if !Token(value) {
		value = ""
	}
	type piece struct {
		n       int
		ext     bool
		content string
	}
	pieces := map[string][]piece{}
	plain := map[string]string{}
	// The names in the order the header gives them: the plain parameters
	// are decoded in that order, then the pieced ones (RFC 2231) in theirs,
	// so that what report is told comes in an order that does not change
	// from run to run.
	var plainOrder, pieceOrder []string
	count := 0
	for rest != "" {
		rest = strings.TrimLeft(rest, " \t;")
		eq := strings.IndexByte(rest, '=')
		if eq < 0 {
			break
		}
		name := strings.ToLower(strings.TrimSpace(rest[:eq]))
		rest = strings.TrimLeft(rest[eq+1:], " \t")
		var v string
		if strings.HasPrefix(rest, `"`) {
			v, rest = quoted(rest)
		} else {
			end := strings.IndexByte(rest, ';')
			if end < 0 {
				end = len(rest)
			}
			v, rest = strings.TrimSpace(rest[:end]), rest[end:]
		}
		count++
		if name == "" || strings.ContainsAny(name, " \t\"") {
			continue
		}
		star := strings.IndexByte(name, '*')
		if count > MaxParams {
			base := name
			if star >= 0 {
				base = name[:star]
			}
			if !used(base) {
				continue
			}
		}
		// RFC 2231: name*, name*0, name*0*, name*1, ...
		if star >= 0 {
			base, suffix := name[:star], name[star+1:]
			ext := strings.HasSuffix(suffix, "*") || suffix == ""
			suffix = strings.TrimSuffix(suffix, "*")
			n := 0
			if suffix != "" {
				var err error
				if n, err = strconv.Atoi(suffix); err != nil || n < 0 || n > MaxPiece {
					if digits(suffix) && used(base) {
						cuts.Pieces++ // a number past the bound, not a malformed one, of a parameter read
					}
					continue
				}
			}
			if _, seen := pieces[base]; !seen {
				pieceOrder = append(pieceOrder, base)
			}
			pieces[base] = append(pieces[base], piece{n, ext, v})
			continue
		}
		if _, dup := plain[name]; !dup {
			plain[name] = v
			plainOrder = append(plainOrder, name)
		}
	}
	params := map[string]string{}
	for _, name := range plainOrder {
		var c Cuts
		params[name], c = decodeWordsIn(plain[name], d)
		cuts.Add(c)
	}
	for _, name := range pieceOrder {
		ps := pieces[name]
		sort.SliceStable(ps, func(i, j int) bool { return ps[i].n < ps[j].n })
		label := ""
		var raw []byte
		for i, p := range ps {
			if i > 0 && ps[i-1].n == p.n {
				continue // a piece given twice: the first is kept
			}
			content := p.content
			if p.ext {
				if i == 0 {
					// charset'language'value
					if a := strings.IndexByte(content, '\''); a >= 0 {
						if b := strings.IndexByte(content[a+1:], '\''); b >= 0 {
							label, content = content[:a], content[a+1+b+1:]
						}
					}
				}
				raw = append(raw, percent(content)...)
			} else {
				raw = append(raw, content...)
			}
		}
		text, res := charset.Decode(label, raw)
		if !res.Known || res.Replaced || res.Detected != "" {
			d.report(charset.Normalize(label), res)
		}
		// The RFC 2231 form wins over a plain parameter of the same name,
		// as it is the one written for programs that read it.
		params[name] = text
	}
	return value, params, cuts
}

// digits reports whether s is made only of ASCII digits.
func digits(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] < '0' || s[i] > '9' {
			return false
		}
	}
	return s != ""
}

// Token reports whether s is made only of printable ASCII other than
// space, quotes, backslash and ";": a media type or a charset label that
// can be written into a header as it is.
func Token(s string) bool {
	for i := 0; i < len(s); i++ {
		if c := s[i]; c <= ' ' || c >= 0x7F || c == '"' || c == '\\' || c == ';' {
			return false
		}
	}
	return true
}

// DecodeRaw returns header bytes as UTF-8 without decoding encoded words.
func DecodeRaw(raw []byte, d *Decoding) string {
	s, res := d.undeclared(raw)
	if res.Replaced || res.Detected != "" {
		d.report("", res)
	}
	return s
}

func decodeWordsIn(v string, d *Decoding) (string, Cuts) {
	if strings.Contains(v, "=?") {
		return DecodeText([]byte(v), d)
	}
	return v, Cuts{}
}

// quoted reads the quoted string at the start of s and returns its content
// and what follows. A quoted string with no end runs to the end of s.
func quoted(s string) (string, string) {
	var sb strings.Builder
	for i := 1; i < len(s); i++ {
		switch c := s[i]; c {
		case '\\':
			if i+1 < len(s) {
				i++
				sb.WriteByte(s[i])
			}
		case '"':
			return sb.String(), s[i+1:]
		default:
			sb.WriteByte(c)
		}
	}
	return sb.String(), ""
}

// percent decodes the %XX escapes of an RFC 2231 extended value.
func percent(s string) []byte {
	out := make([]byte, 0, len(s))
	for i := 0; i < len(s); i++ {
		if s[i] == '%' && i+2 < len(s) && isHex(s[i+1]) && isHex(s[i+2]) {
			out = append(out, unhex(s[i+1])<<4|unhex(s[i+2]))
			i += 2
			continue
		}
		out = append(out, s[i])
	}
	return out
}
