package charset

import (
	"sort"
	"strconv"
	"strings"
	"sync"
	"unicode/utf8"
)

// GB18030, EUC-JP and ISO-2022-JP, decoded as the WHATWG Encoding
// Standard's decoders do, from the tables tables/gb18030-2.txt,
// gb18030-4.txt, jis0208.txt and jis0212.txt that gen/gen_cjk_extra.py
// writes (see its header and NOTICE). Each table is parsed the first time
// a message uses it. A set whose tables are not in tables/ is not decoded:
// its labels are unknown.
//
// The labels gbk and gb2312 stay on code page 936 (dbcs.go): it has the
// two-byte characters of GB18030 and 0x80 as the euro sign, but not the
// four-byte sequences.

type cjkSet uint8

const (
	setGB18030 cjkSet = iota + 1
	setEUCJP
	setISO2022JP
)

var cjkLabels = map[string]cjkSet{
	"gb18030":             setGB18030,
	"euc-jp":              setEUCJP,
	"cseucpkdfmtjapanese": setEUCJP,
	"x-euc-jp":            setEUCJP,
	"iso-2022-jp":         setISO2022JP,
	"csiso2022jp":         setISO2022JP,
}

var (
	gb2Table   = &lazyTable{file: "tables/gb18030-2.txt"}
	jis0208    = &lazyTable{file: "tables/jis0208.txt"}
	jis0212    = &lazyTable{file: "tables/jis0212.txt"}
	gb4Once    sync.Once
	gb4Ranges  []gbRange
	gb4Present bool
)

// get returns the parsed table, or nil if its file is not present.
func (l *lazyTable) get() *dbcs {
	l.once.Do(func() {
		if b, err := dbcsFS.ReadFile(l.file); err == nil {
			l.t = parseDBCS(string(b))
		}
	})
	return l.t
}

// gbRange is a run of GB18030 four-byte pointers whose code points rise
// with them from cp.
type gbRange struct {
	ptr uint32
	cp  rune
}

// gb18030Ranges returns the four-byte ranges, sorted by pointer.
func gb18030Ranges() []gbRange {
	gb4Once.Do(func() {
		b, err := dbcsFS.ReadFile("tables/gb18030-4.txt")
		if err != nil {
			return
		}
		gb4Present = true
		for line := range strings.SplitSeq(string(b), "\n") {
			if line == "" || line[0] == '#' {
				continue
			}
			key, val, ok := strings.Cut(line, " ")
			if !ok {
				continue
			}
			p, err1 := strconv.ParseUint(key, 16, 32)
			v, err2 := strconv.ParseUint(strings.TrimSpace(val), 16, 32)
			if err1 != nil || err2 != nil || v > 0xFFFF {
				continue
			}
			gb4Ranges = append(gb4Ranges, gbRange{uint32(p), rune(v)})
		}
		sort.Slice(gb4Ranges, func(i, j int) bool { return gb4Ranges[i].ptr < gb4Ranges[j].ptr })
	})
	return gb4Ranges
}

// cjkReady reports whether the tables of set s are present.
func cjkReady(s cjkSet) bool {
	switch s {
	case setGB18030:
		return gb2Table.get() != nil && gb18030Ranges() != nil && gb4Present
	case setEUCJP:
		return jis0208.get() != nil && jis0212.get() != nil
	case setISO2022JP:
		return jis0208.get() != nil
	}
	return false
}

func decodeCJK(s cjkSet, b []byte) (string, Result) {
	switch s {
	case setGB18030:
		return decodeGB18030(b)
	case setEUCJP:
		return decodeEUCJP(b)
	}
	return decodeISO2022JP(b)
}

// gbFourByte returns the code point of four-byte pointer p, or 0, as the
// Encoding Standard's "index gb18030 ranges code point".
func gbFourByte(p uint32) rune {
	switch {
	case p > 39419 && p < 189000 || p > 1237575:
		return 0
	case p >= 189000:
		return 0x10000 + rune(p-189000)
	case p == 7457:
		return 0xE7C7
	}
	r := gb18030Ranges()
	i := sort.Search(len(r), func(i int) bool { return r[i].ptr > p }) - 1
	if i < 0 {
		return 0
	}
	return r[i].cp + rune(p-r[i].ptr)
}

func isDigit(c byte) bool { return c >= 0x30 && c <= 0x39 }

// decodeGB18030 is the Encoding Standard's gb18030 decoder.
func decodeGB18030(b []byte) (string, Result) {
	t := gb2Table.get()
	res := Result{Known: true}
	var sb strings.Builder
	sb.Grow(len(b) + len(b)/2)
	bad := func() {
		sb.WriteRune(utf8.RuneError)
		res.Replaced = true
	}
	for i := 0; i < len(b); {
		c := b[i]
		switch {
		case c < 0x80:
			sb.WriteByte(c)
			i++
			continue
		case c == 0x80:
			sb.WriteRune(0x20AC)
			i++
			continue
		case c == 0xFF:
			bad()
			i++
			continue
		}
		// c is a lead byte, 0x81 to 0xFE.
		if i+1 >= len(b) {
			bad()
			break
		}
		c2 := b[i+1]
		if !isDigit(c2) {
			if c == 0xA8 && c2 == 0xBC {
				// The Encoding Standard's index has U+1E3F here and
				// U+E7C7 at four-byte pointer 7457; the table, from
				// GB 18030-2000, has them the other way round.
				sb.WriteRune(0x1E3F)
				i += 2
				continue
			}
			if r := t.pair(c, c2); r != 0 && c2 != 0x7F && c2 != 0xFF {
				sb.WriteRune(r)
				i += 2
				continue
			}
			bad()
			if c2 < 0x80 {
				i++ // the ASCII byte is read again
			} else {
				i += 2
			}
			continue
		}
		// Four bytes.
		if i+2 >= len(b) {
			bad()
			break
		}
		c3 := b[i+2]
		if c3 < 0x81 || c3 > 0xFE {
			bad()
			i++ // the second and third bytes are read again
			continue
		}
		if i+3 >= len(b) {
			bad()
			break
		}
		c4 := b[i+3]
		if !isDigit(c4) {
			bad()
			i++ // the second, third and fourth bytes are read again
			continue
		}
		p := uint32(c-0x81)*12600 + uint32(c2-0x30)*1260 + uint32(c3-0x81)*10 + uint32(c4-0x30)
		if r := gbFourByte(p); r != 0 {
			sb.WriteRune(r)
		} else {
			bad()
		}
		i += 4
	}
	return sb.String(), res
}

// decodeEUCJP is the Encoding Standard's EUC-JP decoder.
func decodeEUCJP(b []byte) (string, Result) {
	t208, t212 := jis0208.get(), jis0212.get()
	res := Result{Known: true}
	var sb strings.Builder
	sb.Grow(len(b) + len(b)/2)
	bad := func() {
		sb.WriteRune(utf8.RuneError)
		res.Replaced = true
	}
	for i := 0; i < len(b); {
		c := b[i]
		if c < 0x80 {
			sb.WriteByte(c)
			i++
			continue
		}
		if c != 0x8E && c != 0x8F && (c < 0xA1 || c == 0xFF) {
			bad()
			i++
			continue
		}
		if i+1 >= len(b) {
			bad()
			break
		}
		c2 := b[i+1]
		switch {
		case c == 0x8E && c2 >= 0xA1 && c2 <= 0xDF:
			sb.WriteRune(0xFF61 + rune(c2-0xA1))
			i += 2
			continue
		case c == 0x8F && c2 >= 0xA1 && c2 <= 0xFE:
			if i+2 >= len(b) {
				bad()
				i = len(b)
				continue
			}
			c3 := b[i+2]
			if c3 >= 0xA1 && c3 <= 0xFE {
				if r := t212.pair(c2, c3); r != 0 {
					sb.WriteRune(r)
					i += 3
					continue
				}
			}
			bad()
			if c3 < 0x80 {
				i += 2
			} else {
				i += 3
			}
			continue
		case c >= 0xA1 && c <= 0xFE && c2 >= 0xA1 && c2 <= 0xFE:
			if r := t208.pair(c, c2); r != 0 {
				sb.WriteRune(r)
				i += 2
				continue
			}
		}
		bad()
		if c2 < 0x80 {
			i++
		} else {
			i += 2
		}
	}
	return sb.String(), res
}

// decodeISO2022JP is the Encoding Standard's ISO-2022-JP decoder: a state
// machine switched by escape sequences, starting in ASCII.
func decodeISO2022JP(b []byte) (string, Result) {
	const (
		ascii = iota
		roman
		katakana
		jis // lead byte of a JIS X 0208 pair
	)
	t := jis0208.get()
	res := Result{Known: true}
	var sb strings.Builder
	sb.Grow(len(b) + len(b)/2)
	bad := func() {
		sb.WriteRune(utf8.RuneError)
		res.Replaced = true
	}
	state, output := ascii, false
	for i := 0; i < len(b); {
		c := b[i]
		if c == 0x1B {
			// Escape start.
			if i+1 >= len(b) || b[i+1] != 0x24 && b[i+1] != 0x28 {
				output = false
				bad()
				i++ // what follows is read again in the current state
				continue
			}
			if i+2 >= len(b) {
				bad()
				i++ // the "$" or "(" is read again
				continue
			}
			next := -1
			switch l, e := b[i+1], b[i+2]; {
			case l == 0x28 && e == 0x42:
				next = ascii
			case l == 0x28 && e == 0x4A:
				next = roman
			case l == 0x28 && e == 0x49:
				next = katakana
			case l == 0x24 && (e == 0x40 || e == 0x42):
				next = jis
			}
			if next < 0 {
				bad()
				i++ // the two bytes after ESC are read again
				continue
			}
			state = next
			i += 3
			if output {
				bad()
			}
			output = true
			continue
		}
		switch state {
		case ascii, roman:
			switch {
			case c >= 0x80 || c == 0x0E || c == 0x0F:
				bad()
			case state == roman && c == 0x5C:
				sb.WriteRune(0xA5)
			case state == roman && c == 0x7E:
				sb.WriteRune(0x203E)
			default:
				sb.WriteByte(c)
			}
			output = false
			i++
		case katakana:
			if c >= 0x21 && c <= 0x5F {
				sb.WriteRune(0xFF61 + rune(c-0x21))
			} else {
				bad()
			}
			output = false
			i++
		case jis:
			output = false
			if c < 0x21 || c > 0x7E {
				bad()
				i++
				continue
			}
			if i+1 >= len(b) {
				bad()
				i++
				continue
			}
			c2 := b[i+1]
			if c2 == 0x1B {
				// An escape in place of the trail byte: the lead is an
				// error and the escape is read.
				bad()
				i++
				continue
			}
			if c2 >= 0x21 && c2 <= 0x7E {
				if r := t.pair(c+0x80, c2+0x80); r != 0 {
					sb.WriteRune(r)
				} else {
					bad()
				}
			} else {
				bad()
			}
			i += 2
		}
	}
	return sb.String(), res
}
