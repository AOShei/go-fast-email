package rfc822

import (
	"sort"
	"strconv"
	"strings"

	"github.com/AOShei/go-fast-email/internal/charset"
)

// maxParams bounds the parameters read from one header.
const maxParams = 64

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
func Params(raw []byte, report CharsetFunc) (string, map[string]string) {
	s := DecodeRaw(raw, report)
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
	count := 0
	for rest != "" && count < maxParams {
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
		// RFC 2231: name*, name*0, name*0*, name*1, ...
		if star := strings.IndexByte(name, '*'); star >= 0 {
			base, suffix := name[:star], name[star+1:]
			ext := strings.HasSuffix(suffix, "*") || suffix == ""
			suffix = strings.TrimSuffix(suffix, "*")
			n := 0
			if suffix != "" {
				var err error
				if n, err = strconv.Atoi(suffix); err != nil || n < 0 || n > 999 {
					continue
				}
			}
			pieces[base] = append(pieces[base], piece{n, ext, v})
			continue
		}
		if _, dup := plain[name]; !dup {
			plain[name] = v
		}
	}
	params := map[string]string{}
	for name, v := range plain {
		params[name] = decodeWordsIn(v, report)
	}
	for name, ps := range pieces {
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
		if (!res.Known || res.Replaced) && report != nil {
			report(charset.Normalize(label), res)
		}
		// The RFC 2231 form wins over a plain parameter of the same name,
		// as it is the one written for programs that read it.
		params[name] = text
	}
	return value, params
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
func DecodeRaw(raw []byte, report CharsetFunc) string {
	s, res := charset.Decode("", raw)
	if res.Replaced && report != nil {
		report("", res)
	}
	return s
}

func decodeWordsIn(v string, report CharsetFunc) string {
	if strings.Contains(v, "=?") {
		return DecodeText([]byte(v), report)
	}
	return v
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
