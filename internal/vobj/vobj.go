// Package vobj reads the content lines of iCalendar (RFC 5545, section
// 3.1) and vCard (RFC 6350, section 3.2) files, and of the vCard 2.1 and
// vCalendar 1.0 files phones and Outlook write.
//
// A Parser gives the lines of a file as events, in file order: the BEGIN
// and END of each component, and each property between them. It holds one
// logical line at a time, never the file's tree: the caller builds what it
// needs from the events. Every property and component keeps the byte span
// of its lines in the input.
//
// The parser never fails. What does not follow the grammar is read by a
// recovery path and counted in Problems: an END that matches no open
// component is skipped; a component with no END ends where the END of a
// component around it is, or at the end of the input; a line that is not
// a content line (no colon, or no name before it) continues the property
// before it; a quoted-printable soft line break does not continue onto a
// BEGIN line or the END line of an open component. Two bounds hold: components nested deeper than MaxDepth are
// skipped, and a logical line longer than MaxLine is cut.
package vobj

import "strings"

// MaxDepth bounds how deeply components nest. A component that would be
// deeper is skipped with everything in it, and counted in
// Problems.TooDeep.
const MaxDepth = 32

// MaxLine bounds one logical line, after unfolding: past it the line is
// cut, the property is marked Cut, and it is counted in
// Problems.LongLines.
const MaxLine = 1 << 20

// Param is one parameter of a property.
type Param struct {
	// Name is the parameter's name in upper case. A vCard 2.1 parameter
	// written without a name (TEL;CELL) is a TYPE, or an ENCODING where
	// it is one of the encodings (NOTE;QUOTED-PRINTABLE).
	Name string
	// Values are the parameter's values, unquoted, with the caret
	// escapes of RFC 6868 decoded.
	Values []string
}

// Property is one content line, unfolded.
type Property struct {
	Group  string // the group before the name ("item1" in item1.TEL), as written
	Name   string // the name in upper case; "" for a line that is not a content line and follows no property
	Params []Param
	// Value is the value as written, unfolded: not decoded, not
	// unescaped. Lines that continue it (see the package comment) are
	// joined to it with "\n".
	Value string
	// Offset and Length are the byte span of the property's lines in the
	// input, from the first byte of its name to the last byte before the
	// line end of its last line.
	Offset, Length int
	// Cut: the line was longer than MaxLine and was cut there.
	Cut bool
}

// Param returns the first value of the named parameter (upper case), or
// "".
func (p *Property) Param(name string) string {
	for _, q := range p.Params {
		if q.Name == name && len(q.Values) > 0 {
			return q.Values[0]
		}
	}
	return ""
}

// ParamValues returns every value of the named parameter (upper case),
// in order, over every time the parameter is given.
func (p *Property) ParamValues(name string) []string {
	var out []string
	for _, q := range p.Params {
		if q.Name == name {
			out = append(out, q.Values...)
		}
	}
	return out
}

// HasParam reports whether the property has the named parameter (upper
// case).
func (p *Property) HasParam(name string) bool {
	for _, q := range p.Params {
		if q.Name == name {
			return true
		}
	}
	return false
}

// Encoding returns the property's encoding in upper case: "QUOTED-PRINTABLE",
// "B" (also for BASE64 and b), or "" for none or another.
func (p *Property) Encoding() string {
	switch e := strings.ToUpper(p.Param("ENCODING")); e {
	case "QUOTED-PRINTABLE":
		return e
	case "B", "BASE64":
		return "B"
	}
	return ""
}

// Kind is the kind of an Event.
type Kind uint8

const (
	Prop  Kind = iota // a property
	Begin             // the BEGIN of a component
	End               // the end of a component
)

// Event is one thing the parser read.
type Event struct {
	Kind Kind
	// Name is the component's name in upper case (Begin, End).
	Name string
	// Prop is the property (Prop).
	Prop *Property
	// Offset and Length: for Begin, the BEGIN line; for End, the whole
	// component from the first byte of its BEGIN line to the last byte of
	// its END line before the line end (where it has no END, to the last
	// byte of the last line in it).
	Offset, Length int
	// Depth is the component's depth (Begin, End), 1 for one at the top
	// level, or for a property the depth of the component it is in, 0
	// outside any.
	Depth int
	// Missing: the component has no END line (End).
	Missing bool
}

// Problems counts what the parser read by a recovery path or cut.
type Problems struct {
	UnmatchedEnd int // END lines that match no open component, skipped
	MissingEnd   int // components with no END line
	NotLines     int // lines that are not content lines (no colon, or no name before it)
	TooDeep      int // components nested deeper than MaxDepth, skipped
	LongLines    int // logical lines longer than MaxLine, cut
	// SoftBreakEnds counts quoted-printable values whose last line ends
	// with a soft line break ("=") before a BEGIN line or the END line of
	// an open component: the value ends there.
	SoftBreakEnds int
}

type open struct {
	name   string
	offset int
}

// Parser reads content lines. Make one with NewParser.
type Parser struct {
	src     []byte
	pos     int
	stack   []open
	skip    int       // depth inside a skipped component, 0 when none
	pending *Property // the last property, until the next line shows whether it continues
	queue   []Event
	qi      int
	last    int // end of the content of the last logical line read
	buf     []byte
	done    bool
	// Problems counts what was read by a recovery path or cut so far.
	Problems Problems
}

// NewParser returns a parser of src. A UTF-8 byte order mark at its start
// is skipped; offsets count it.
func NewParser(src []byte) *Parser {
	p := &Parser{src: src}
	if len(src) >= 3 && src[0] == 0xEF && src[1] == 0xBB && src[2] == 0xBF {
		p.pos, p.last = 3, 3
	}
	return p
}

// Next returns the next event, and false at the end of the input.
func (p *Parser) Next() (Event, bool) {
	for {
		if p.qi < len(p.queue) {
			e := p.queue[p.qi]
			p.queue[p.qi] = Event{}
			p.qi++
			return e, true
		}
		p.queue, p.qi = p.queue[:0], 0
		if p.done {
			return Event{}, false
		}
		p.step()
	}
}

// step reads one logical line and queues the events it gives.
func (p *Parser) step() {
	line, start, end, cut, ok := p.readLine()
	if !ok {
		p.flush()
		// Every component still open has no END line, those skipped
		// too.
		p.Problems.MissingEnd += len(p.stack) + p.skip
		p.closeTo(0, p.last, true)
		p.done = true
		return
	}
	defer func() { p.last = end }()
	if isBlank(line) {
		return
	}
	prop, ok := parseLine(line)
	if cut {
		p.Problems.LongLines++
	}
	if !ok {
		if p.skip > 0 {
			return
		}
		p.Problems.NotLines++
		if p.pending != nil {
			p.pending.Value += "\n" + string(line)
			p.pending.Length = end - p.pending.Offset
			p.pending.Cut = p.pending.Cut || cut
			return
		}
		p.flush()
		p.queue = append(p.queue, Event{Kind: Prop, Depth: len(p.stack),
			Prop: &Property{Value: string(line), Offset: start, Length: end - start, Cut: cut}})
		return
	}
	prop.Offset, prop.Length, prop.Cut = start, end-start, cut
	switch prop.Name {
	case "BEGIN":
		p.flush()
		name := strings.ToUpper(strings.TrimSpace(prop.Value))
		switch {
		case p.skip > 0:
			p.skip++
		case len(p.stack) >= MaxDepth:
			p.skip = 1
			p.Problems.TooDeep++
		default:
			p.stack = append(p.stack, open{name, start})
			p.queue = append(p.queue, Event{Kind: Begin, Name: name, Offset: start, Length: end - start, Depth: len(p.stack)})
		}
	case "END":
		p.flush()
		if p.skip > 0 {
			p.skip--
			return
		}
		name := strings.ToUpper(strings.TrimSpace(prop.Value))
		j := len(p.stack) - 1
		for j >= 0 && p.stack[j].name != name {
			j--
		}
		if j < 0 {
			p.Problems.UnmatchedEnd++
			return
		}
		p.Problems.MissingEnd += len(p.stack) - 1 - j
		p.closeTo(j+1, p.last, true)
		p.closeTo(j, end, false)
	default:
		if p.skip > 0 {
			return
		}
		p.flush()
		p.pending = prop
	}
}

// closeTo closes the open components down to depth n, each ending at end.
func (p *Parser) closeTo(n, end int, missing bool) {
	for len(p.stack) > n {
		o := p.stack[len(p.stack)-1]
		p.queue = append(p.queue, Event{Kind: End, Name: o.name, Offset: o.offset, Length: max(end-o.offset, 0), Depth: len(p.stack), Missing: missing})
		p.stack = p.stack[:len(p.stack)-1]
	}
}

// flush queues the pending property.
func (p *Parser) flush() {
	if p.pending != nil {
		p.queue = append(p.queue, Event{Kind: Prop, Prop: p.pending, Depth: len(p.stack)})
		p.pending = nil
	}
}

// readLine reads one logical line: its bytes (valid until the next call),
// the offsets of its first byte and of the end of its last line's content,
// whether it was cut at MaxLine, and false at the end of the input.
func (p *Parser) readLine() (line []byte, start, end int, cut, ok bool) {
	if p.pos >= len(p.src) {
		return nil, 0, 0, false, false
	}
	p.buf = p.buf[:0]
	start = p.pos
	qp := -1 // whether the line is quoted-printable: -1 not yet known
	for {
		i := p.pos
		for i < len(p.src) && p.src[i] != '\r' && p.src[i] != '\n' {
			i++
		}
		content := p.src[p.pos:i]
		eol := 0
		if i < len(p.src) {
			eol = 1
			if p.src[i] == '\r' && i+1 < len(p.src) && p.src[i+1] == '\n' {
				eol = 2
			}
		}
		if room := MaxLine - len(p.buf); len(content) > room {
			p.buf = append(p.buf, content[:max(room, 0)]...)
			cut = true
		} else {
			p.buf = append(p.buf, content...)
		}
		end = i
		p.pos = i + eol
		if eol == 0 || p.pos >= len(p.src) {
			break
		}
		if trimmed := trimRight(content); len(trimmed) > 0 && trimmed[len(trimmed)-1] == '=' {
			if qp < 0 {
				qp = 0
				if isQuotedPrintable(p.buf) {
					qp = 1
				}
			}
			if qp == 1 {
				if p.boundary(p.pos) {
					// A soft break before a BEGIN line, or the END line
					// of an open component: phone exporters end a value
					// with "=". The line ends the value.
					p.Problems.SoftBreakEnds++
					break
				}
				// A soft line break: the next line continues the value.
				// The break is kept for the decoder, which removes it.
				if len(p.buf) < MaxLine {
					p.buf = append(p.buf, '\n')
				}
				continue
			}
		}
		if c := p.src[p.pos]; c == ' ' || c == '\t' {
			p.pos++ // folding: the line end and one white space character
			continue
		}
		break
	}
	return p.buf, start, end, cut, true
}

// boundary reports whether the line at i begins, in any case, with
// "BEGIN:", or is "END:" and the name of an open component (any name
// inside a skipped component, whose names are not kept).
func (p *Parser) boundary(i int) bool {
	j := i
	for j < len(p.src) && p.src[j] != '\r' && p.src[j] != '\n' {
		j++
	}
	l := p.src[i:j]
	if len(l) >= 6 && strings.EqualFold(string(l[:6]), "BEGIN:") {
		return true
	}
	if len(l) < 4 || !strings.EqualFold(string(l[:4]), "END:") {
		return false
	}
	if p.skip > 0 {
		return true
	}
	name := strings.TrimSpace(string(l[4:]))
	for _, o := range p.stack {
		if strings.EqualFold(o.name, name) {
			return true
		}
	}
	return false
}

func trimRight(b []byte) []byte {
	for len(b) > 0 && (b[len(b)-1] == ' ' || b[len(b)-1] == '\t') {
		b = b[:len(b)-1]
	}
	return b
}

func isBlank(b []byte) bool {
	for _, c := range b {
		if c != ' ' && c != '\t' {
			return false
		}
	}
	return true
}

// isQuotedPrintable reports whether the name and parameters of a line,
// before its first colon outside quotes, declare quoted-printable.
func isQuotedPrintable(b []byte) bool {
	quoted := false
	for i, c := range b {
		switch {
		case c == '"':
			quoted = !quoted
		case c == ':' && !quoted:
			return containsFold(b[:i], "QUOTED-PRINTABLE")
		}
	}
	return false
}

func containsFold(b []byte, s string) bool {
	for i := 0; i+len(s) <= len(b); i++ {
		if strings.EqualFold(string(b[i:i+len(s)]), s) {
			return true
		}
	}
	return false
}

// isNameByte: the characters of a name (RFC 5545 and 6350: letters,
// digits and "-"; "_" and "/" too, as some programs write them).
func isNameByte(c byte) bool {
	return c >= 'A' && c <= 'Z' || c >= 'a' && c <= 'z' || c >= '0' && c <= '9' || c == '-' || c == '_' || c == '/'
}

// encodings are the vCard 2.1 values of ENCODING, which may be written
// without the parameter's name.
var encodings = map[string]bool{"QUOTED-PRINTABLE": true, "BASE64": true, "B": true, "8BIT": true, "7BIT": true}

// parseLine reads [group.]NAME[;PARAM[=value[,value]...]]...:value. It
// returns false for a line that is not a content line.
func parseLine(b []byte) (*Property, bool) {
	i := 0
	for i < len(b) && b[i] != ';' && b[i] != ':' {
		i++
	}
	if i == len(b) {
		return nil, false
	}
	full := strings.TrimSpace(string(b[:i]))
	group, name := "", full
	if dot := strings.LastIndexByte(full, '.'); dot >= 0 {
		group, name = full[:dot], full[dot+1:]
	}
	if name == "" {
		return nil, false
	}
	for j := 0; j < len(name); j++ {
		if !isNameByte(name[j]) {
			return nil, false
		}
	}
	for j := 0; j < len(group); j++ {
		if !isNameByte(group[j]) && group[j] != '.' {
			return nil, false
		}
	}
	p := &Property{Group: group, Name: strings.ToUpper(name)}
	for i < len(b) && b[i] == ';' {
		i++
		at := i
		for i < len(b) && b[i] != '=' && b[i] != ';' && b[i] != ':' {
			i++
		}
		pname := strings.ToUpper(strings.TrimSpace(string(b[at:i])))
		if i < len(b) && b[i] == '=' {
			i++
			var vals []string
			for {
				var v []byte
				if i < len(b) && b[i] == '"' {
					if q := indexByte(b[i+1:], '"'); q >= 0 {
						v = b[i+1 : i+1+q]
						i += q + 2
						// Anything up to the next separator belongs to
						// the value too.
						at := i
						for i < len(b) && b[i] != ',' && b[i] != ';' && b[i] != ':' {
							i++
						}
						v = append(v[:len(v):len(v)], b[at:i]...)
					}
				}
				if v == nil {
					at := i
					for i < len(b) && b[i] != ',' && b[i] != ';' && b[i] != ':' {
						i++
					}
					v = b[at:i]
				}
				vals = append(vals, caret(v))
				if i < len(b) && b[i] == ',' {
					i++
					continue
				}
				break
			}
			p.Params = append(p.Params, Param{Name: pname, Values: vals})
			continue
		}
		if pname == "" {
			continue
		}
		if encodings[pname] {
			p.Params = append(p.Params, Param{Name: "ENCODING", Values: []string{pname}})
		} else {
			p.Params = append(p.Params, Param{Name: "TYPE", Values: []string{pname}})
		}
	}
	if i >= len(b) || b[i] != ':' {
		return nil, false
	}
	p.Value = string(b[i+1:])
	return p, true
}

func indexByte(b []byte, c byte) int {
	for i, x := range b {
		if x == c {
			return i
		}
	}
	return -1
}

// caret decodes the escapes of RFC 6868 in a parameter value: ^n a line
// break, ^^ a caret, ^' a double quote. A caret before anything else is
// kept.
func caret(b []byte) string {
	if indexByte(b, '^') < 0 {
		return string(b)
	}
	var sb strings.Builder
	for i := 0; i < len(b); i++ {
		if b[i] == '^' && i+1 < len(b) {
			switch b[i+1] {
			case 'n', 'N':
				sb.WriteByte('\n')
				i++
				continue
			case '^':
				sb.WriteByte('^')
				i++
				continue
			case '\'':
				sb.WriteByte('"')
				i++
				continue
			}
		}
		sb.WriteByte(b[i])
	}
	return sb.String()
}

// Unescape undoes the escapes of a TEXT value: \\ \; \, and \n or \N (a
// line break), and \: (vCard 3.0 writers such as Apple's escape the colon
// of an address). A backslash before anything else is kept as written.
func Unescape(s string) string {
	if strings.IndexByte(s, '\\') < 0 {
		return s
	}
	var sb strings.Builder
	sb.Grow(len(s))
	for i := 0; i < len(s); i++ {
		if s[i] == '\\' && i+1 < len(s) {
			switch s[i+1] {
			case '\\', ';', ',', ':':
				sb.WriteByte(s[i+1])
				i++
				continue
			case 'n', 'N':
				sb.WriteByte('\n')
				i++
				continue
			}
		}
		sb.WriteByte(s[i])
	}
	return sb.String()
}

// Split splits a value at each sep that no backslash escapes. The pieces
// are as written, still escaped.
func Split(s string, sep byte) []string {
	var out []string
	at := 0
	for i := 0; i < len(s); i++ {
		switch s[i] {
		case '\\':
			i++
		case sep:
			out = append(out, s[at:i])
			at = i + 1
		}
	}
	return append(out, s[at:])
}
