package gofastemail

import (
	"bytes"
	"fmt"
	"io"
	"slices"
	"strconv"
	"strings"

	"github.com/AOShei/go-fast-core"
	"github.com/AOShei/go-fast-email/internal/charset"
	"github.com/AOShei/go-fast-email/internal/clean"
	"github.com/AOShei/go-fast-email/internal/rfc822"
	"github.com/AOShei/go-fast-email/internal/textbody"
	"github.com/AOShei/go-fast-email/internal/vobj"
)

// Bounds of calendars (.ics) and contact lists (.vcf).
const (
	// MaxComponentDepth bounds how deeply the components of a calendar or
	// contact list nest. Deeper components are skipped with everything in
	// them, and reported with limit.exceeded.
	MaxComponentDepth = vobj.MaxDepth
	// MaxContentLineBytes bounds one content line after unfolding; past
	// it the value is cut, with limit.exceeded.
	MaxContentLineBytes = vobj.MaxLine
	// MaxComponentItems bounds the properties and components held in one
	// event, to-do, card or other component while it is read (and those
	// of the calendar itself); the rest are not read, with
	// limit.exceeded.
	MaxComponentItems = 100_000
)

// WarnComponentWithoutText: components of a calendar or contact list
// have no text at all (no summary, no details, no description, no
// attachment), so they give no block and their properties, which would be
// in the attributes of their first block, are not in the output. The
// message gives the count and the kinds. Info.
const WarnComponentWithoutText = "email.component_without_text"

// Keys of Block.Attrs this library sets on the first block of a
// component of a calendar or a card.
const (
	// AttrComponent: the component's kind in lower case ("vevent",
	// "vtodo", "vcard", ...).
	AttrComponent = "component"
)

// vobjFormat decides from the first bytes of a file whether it is a
// calendar ("ics") or a contact list ("vcf"): after a byte order mark and
// white space, BEGIN:VCALENDAR or BEGIN:VCARD, in any case.
func vobjFormat(head []byte) string {
	head = bytes.TrimPrefix(head, []byte("\xEF\xBB\xBF"))
	head = bytes.TrimLeft(head, " \t\r\n")
	for _, f := range []struct{ begin, format string }{{"BEGIN:VCALENDAR", "ics"}, {"BEGIN:VCARD", "vcf"}} {
		if len(head) < len(f.begin) || !strings.EqualFold(string(head[:len(f.begin)]), f.begin) {
			continue
		}
		if rest := head[len(f.begin):]; len(rest) == 0 || bytes.IndexByte([]byte(" \t\r\n"), rest[0]) >= 0 {
			return f.format
		}
	}
	return ""
}

// comp is a component while it is read.
type comp struct {
	name           string
	props          []*vobj.Property
	subs           []*comp
	offset, length int
}

// vstate is the state of one calendar or contact list while it is read.
type vstate struct {
	r      *reader
	m      *message
	cal    bool // a calendar; else a contact list
	stack  []*comp
	held   int // properties and components held in the open item
	calN   int // properties held for the metadata
	over   int // properties and components not read: past MaxComponentItems
	meta   []*vobj.Property
	tzids  []string
	cards  int
	alts   int // components with a DESCRIPTION and an HTML one
	bare   int // components with no text
	kinds  []string
	undec  bool // the decode budget is spent
}

// vobject reads a calendar or contact list.
func (r *reader) vobject(src io.ReaderAt, size int64, format string) (*gofastcore.Document, error) {
	if !r.opts.Parts.Contains(1) {
		return nil, fmt.Errorf("gofastemail: %w", gofastcore.ErrNoPartsSelected)
	}
	n := min(size, r.e.maxMessage())
	raw := getBuffer(n)
	defer putBuffer(raw)
	if got, err := src.ReadAt(raw, 0); int64(got) < n {
		return nil, fmt.Errorf("gofastemail: reading: %v: %w", err, gofastcore.ErrMalformed)
	}
	mime, what := "text/vcard", "contact list"
	if format == "ics" {
		mime, what = "text/calendar", "calendar"
	}
	r.doc = gofastcore.New(format)
	r.doc.Source = gofastcore.Source{Format: format, Name: r.opts.Name, MIME: mime, Size: size}
	m := &message{def: r.e.Charset}
	m.part.Kind, m.part.Number = gofastcore.PartBody, 1
	if size > n {
		m.warn(gofastcore.SeverityError, gofastcore.WarnLimitExceeded, fmt.Sprintf("%s of %d bytes read to %d", what, size, n))
	}
	v := &vstate{r: r, m: m, cal: format == "ics"}
	p := vobj.NewParser(raw)
	for i := 0; ; i++ {
		if i&255 == 0 && r.ctx.Err() != nil {
			return nil, r.ctx.Err()
		}
		e, ok := p.Next()
		if !ok {
			break
		}
		v.event(e)
	}
	if err := r.ctx.Err(); err != nil {
		return nil, err
	}
	v.finish(p.Problems, what)
	r.doc.Parts = append(r.doc.Parts, m.part)
	return r.finish(), nil
}

// container reports whether a component holds items rather than being
// one: a calendar.
func container(name string) bool { return name == "VCALENDAR" }

func (v *vstate) event(e vobj.Event) {
	switch e.Kind {
	case vobj.Begin:
		c := &comp{name: e.Name, offset: e.Offset}
		if len(v.stack) > 0 && !container(v.stack[len(v.stack)-1].name) && !container(e.Name) {
			// Inside an item: held until the item ends.
			if v.held >= MaxComponentItems {
				v.over++
			}
			v.held++
		}
		v.stack = append(v.stack, c)
	case vobj.Prop:
		if len(v.stack) == 0 || container(v.stack[len(v.stack)-1].name) {
			if v.calN >= MaxComponentItems {
				v.over++
				return
			}
			v.calN++
			v.meta = append(v.meta, e.Prop)
			return
		}
		if v.held >= MaxComponentItems {
			v.over++
			return
		}
		v.held++
		top := v.stack[len(v.stack)-1]
		top.props = append(top.props, e.Prop)
	case vobj.End:
		c := v.stack[len(v.stack)-1]
		v.stack = v.stack[:len(v.stack)-1]
		c.length = e.Length
		switch {
		case container(c.name):
		case len(v.stack) == 0 || container(v.stack[len(v.stack)-1].name):
			v.item(c)
			v.held = 0
		case v.held <= MaxComponentItems:
			parent := v.stack[len(v.stack)-1]
			parent.subs = append(parent.subs, c)
		}
	}
}

// item reads a component that stands for itself: an event, to-do,
// journal entry, free/busy time, availability or card, and those nested
// in it.
func (v *vstate) item(c *comp) {
	if c.name == "VTIMEZONE" && v.cal {
		// Rules for computing times: only their names are kept.
		for _, p := range c.props {
			if p.Name == "TZID" {
				if t := v.line(v.text(p)); t != "" {
					v.tzids = append(v.tzids, t)
				}
			}
		}
		return
	}
	if v.r.ctx.Err() != nil {
		return
	}
	if v.cal {
		v.event1(c)
	} else {
		v.card(c)
	}
}

// decoded returns the bytes of a property's value: quoted-printable and
// base64 decoded, within the decode budget; nil when the budget is spent.
func (v *vstate) decoded(p *vobj.Property) []byte {
	var out []byte
	switch p.Encoding() {
	case "QUOTED-PRINTABLE":
		out = rfc822.QuotedPrintable([]byte(p.Value))
	case "B":
		out = rfc822.Base64([]byte(p.Value))
		if rfc822.Base64Bad([]byte(p.Value)) {
			v.m.warn(gofastcore.SeverityInfo, gofastcore.WarnInputRepaired, "base64 with characters outside its alphabet; they are skipped")
		}
	default:
		return []byte(p.Value)
	}
	if v.undec || !v.r.dec.Take(int64(len(out))) {
		if !v.undec {
			v.undec = true
			v.m.warn(gofastcore.SeverityError, gofastcore.WarnLimitExceeded, "decode limit reached; the rest of the encoded values are not decoded")
		}
		return nil
	}
	return out
}

// str returns a property's value as text, decoded and in its character
// set, still escaped.
func (v *vstate) str(p *vobj.Property) string {
	if p.Encoding() == "" && p.Param("CHARSET") == "" && utf8Valid(p.Value) {
		return p.Value
	}
	b := v.decoded(p)
	if b == nil {
		return ""
	}
	label := p.Param("CHARSET")
	text, res := charset.DecodeDefault(label, v.m.def, b)
	if !res.Known || res.Replaced || res.Detected != "" {
		if label == "" {
			label = "us-ascii"
		}
		v.m.charsetReport()(charset.Normalize(label), res)
	}
	return text
}

// text returns a property's value as text, unescaped, with control
// characters removed (and counted). Line breaks are kept.
func (v *vstate) text(p *vobj.Property) string {
	t, n := clean.RawN(vobj.Unescape(v.str(p)))
	v.m.controls += n
	return t
}

// pieces returns a structured value split at sep, each piece unescaped
// and made one line.
func (v *vstate) pieces(p *vobj.Property, sep byte) []string {
	parts := vobj.Split(v.str(p), sep)
	for i, s := range parts {
		parts[i] = v.line(vobj.Unescape(s))
	}
	return parts
}

// line makes text one line, counting the control characters removed.
func (v *vstate) line(s string) string { return v.m.line(s) }

func utf8Valid(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] >= 0x80 {
			return strings.ToValidUTF8(s, "�") == s
		}
	}
	return true
}

// attrs gathers the properties of a component that are not used in its
// text, by lower-case name, a repeated name joined by "\n".
type attrs struct {
	keys []string
	vals map[string][]string
}

func (a *attrs) add(key, value string) {
	if a.vals == nil {
		a.vals = map[string][]string{}
	}
	if _, ok := a.vals[key]; !ok {
		a.keys = append(a.keys, key)
	}
	a.vals[key] = append(a.vals[key], value)
}

// prop adds a property as written, after unescaping (base64 data stays as
// written).
func (v *vstate) attr(a *attrs, prefix string, p *vobj.Property) {
	key := strings.ToLower(p.Name)
	if key == "" {
		key = "unnamed"
	}
	value := p.Value
	if p.Encoding() != "B" {
		value = v.text(p)
	} else {
		value = clean.Raw(value)
	}
	a.add(prefix+key, value)
}

// span is the Loc of a property or a component.
func vloc(offset, length int, anchor string) gofastcore.Location {
	return gofastcore.Location{Offset: int64(offset), Length: int64(length), Anchor: anchor}
}

// block adds a block of text, within the text budget, and reports whether
// it was added.
func (v *vstate) block(b gofastcore.Block) bool {
	if b.Text == "" || !v.r.take(v.m, gofastcore.BlockCost+len(b.Text)) {
		return false
	}
	v.m.part.Blocks = append(v.m.part.Blocks, b)
	return true
}

// paragraphs adds the paragraphs of a text value (DESCRIPTION, COMMENT,
// NOTE), as a plain body's, each with the property's place.
func (v *vstate) paragraphs(p *vobj.Property, anchor string) {
	text := gofastcore.ExpandLigatures(v.text(p))
	from := len(v.m.part.Blocks)
	v.m.part.Blocks, _ = textbody.Blocks(v.m.part.Blocks, text, textbody.Options{}, func(n int) bool { return v.r.take(v.m, n) })
	for i := from; i < len(v.m.part.Blocks); i++ {
		v.m.part.Blocks[i].Loc = vloc(p.Offset, p.Length, anchor)
	}
}

// external reports a reference to content outside the file (an ATTACH,
// PHOTO or LOGO given by a URI); nothing is fetched.
func (v *vstate) external(name, uri string) {
	quoted := v.m.reference(clean.Line(uri))
	v.m.warnEach(gofastcore.SeverityWarning, gofastcore.WarnInputExternalReference, "external value", name+"\x00"+uri,
		name+" "+quoted, fmt.Sprintf("a %s property refers to content outside the file: %s", name, quoted), true,
		func(count, list string) string {
			return fmt.Sprintf("properties refer to %s places outside the file for their content: %s", count, list)
		})
}

// finishItem sets the attributes of a component's first block (from, if
// it has one), or counts the component as one with no text.
func (v *vstate) finishItem(c *comp, from int, a *attrs, children []int) {
	if from >= len(v.m.part.Blocks) {
		if !v.m.cut {
			v.bare++
			if k := strings.ToLower(c.name); !slices.Contains(v.kinds, k) && len(v.kinds) < maxListed {
				v.kinds = append(v.kinds, k)
			}
		}
		return
	}
	b := &v.m.part.Blocks[from]
	out := make(map[string]string, len(a.keys)+2)
	cost := 0
	for _, k := range a.keys {
		out[k] = strings.Join(a.vals[k], "\n")
		cost += len(k) + len(out[k])
	}
	if !v.r.take(v.m, cost) {
		out = map[string]string{}
	}
	if b.Attrs != nil {
		for k, x := range b.Attrs {
			out[k] = x
		}
	}
	out[AttrComponent] = strings.ToLower(c.name)
	if len(children) > 0 {
		idx := make([]string, len(children))
		for i, n := range children {
			idx[i] = strconv.Itoa(n)
		}
		out[AttrChildren] = strings.Join(idx, ",")
	}
	b.Attrs = out
}

// uid returns a component's UID, as one line.
func (v *vstate) uid(c *comp) string {
	for _, p := range c.props {
		if p.Name == "UID" {
			return clean.Line(vobj.Unescape(v.str(p)))
		}
	}
	return ""
}

// calDate writes an iCalendar DATE or DATE-TIME in ISO 8601: 20261125 as
// 2026-11-25, 20261125T100000Z as 2026-11-25T10:00:00Z, and a local time
// with its TZID after it in parentheses. Anything else stays as written.
func calDate(value, tzid string) string {
	s := strings.TrimSpace(value)
	digits := func(s string) bool {
		for i := 0; i < len(s); i++ {
			if s[i] < '0' || s[i] > '9' {
				return false
			}
		}
		return true
	}
	switch {
	case len(s) == 8 && digits(s):
		return s[:4] + "-" + s[4:6] + "-" + s[6:]
	case (len(s) == 15 || len(s) == 16 && s[15] == 'Z') && digits(s[:8]) && s[8] == 'T' && digits(s[9:15]):
		out := s[:4] + "-" + s[4:6] + "-" + s[6:8] + "T" + s[9:11] + ":" + s[11:13] + ":" + s[13:15]
		if len(s) == 16 {
			return out + "Z"
		}
		if tzid != "" {
			out += " (" + tzid + ")"
		}
		return out
	}
	return value
}

// person writes an ORGANIZER or ATTENDEE: "CN <address>", or the address,
// with mailto: removed.
func person(value, cn string) string {
	addr := strings.TrimSpace(value)
	if len(addr) >= 7 && strings.EqualFold(addr[:7], "mailto:") {
		addr = addr[7:]
	}
	switch {
	case cn == "":
		return addr
	case addr == "":
		return cn
	}
	return cn + " <" + addr + ">"
}

// The properties of a calendar component written in its details
// paragraph, with their labels, in order.
var calLabels = []struct{ name, label, join string }{
	{"DTSTART", "Start", ""}, {"DTEND", "End", ""}, {"DUE", "Due", ""}, {"DURATION", "Duration", ""},
	{"COMPLETED", "Completed", ""}, {"RRULE", "Repeats", "; "}, {"LOCATION", "Location", ""},
	{"ORGANIZER", "Organizer", ""}, {"ATTENDEE", "Attendees", ", "}, {"STATUS", "Status", ""},
	{"CATEGORIES", "Categories", ", "}, {"URL", "URL", ""}, {"FREEBUSY", "Free/busy", ", "},
}

// event1 reads one component of a calendar, then the components nested in
// it other than alarms.
func (v *vstate) event1(c *comp) {
	m := v.m
	anchor := v.uid(c)
	from := len(m.part.Blocks)
	var a attrs
	used := make([]bool, len(c.props))

	// The heading: the first SUMMARY.
	for i, p := range c.props {
		if p.Name != "SUMMARY" {
			continue
		}
		if t := gofastcore.ExpandLigatures(v.line(v.text(p))); t != "" {
			used[i] = true
			v.block(gofastcore.Block{Kind: gofastcore.KindHeading, Level: 1, Text: t, Loc: vloc(p.Offset, p.Length, anchor)})
		}
		break
	}

	// The details.
	var lines []string
	for _, l := range calLabels {
		var vals []string
		for i, p := range c.props {
			if p.Name != l.name {
				continue
			}
			var t string
			switch p.Name {
			case "DTSTART", "DTEND", "DUE", "COMPLETED":
				t = calDate(v.line(v.text(p)), v.line(p.Param("TZID")))
			case "ORGANIZER", "ATTENDEE":
				t = person(v.line(v.text(p)), v.line(p.Param("CN")))
			case "CATEGORIES":
				t = strings.Join(filled(v.pieces(p, ',')), ", ")
			case "RRULE":
				t = v.line(v.str(p))
			default:
				t = v.line(v.text(p))
			}
			if t == "" {
				continue // an empty value: kept in the attributes
			}
			used[i] = true
			if l.join == "" {
				lines = append(lines, l.label+": "+t)
			} else {
				vals = append(vals, t)
			}
		}
		if len(vals) > 0 {
			lines = append(lines, l.label+": "+strings.Join(vals, l.join))
		}
	}
	var nested []*comp
	for _, s := range c.subs {
		if s.name != "VALARM" {
			nested = append(nested, s)
			continue
		}
		var trigger, desc string
		for _, p := range s.props {
			switch p.Name {
			case "TRIGGER":
				if trigger == "" {
					if trigger = v.line(v.str(p)); trigger != "" {
						continue
					}
				}
			case "DESCRIPTION", "SUMMARY":
				if desc == "" {
					if desc = v.line(v.text(p)); desc != "" {
						continue
					}
				}
			}
			v.attr(&a, "alarm.", p)
		}
		nested = append(nested, s.subs...)
		if t := strings.TrimSpace(trigger + " " + desc); t != "" {
			lines = append(lines, "Alarm: "+t)
		}
	}
	detailsAt := -1
	if len(lines) > 0 {
		if v.block(gofastcore.Block{Kind: gofastcore.KindParagraph, Text: gofastcore.ExpandLigatures(strings.Join(lines, "\n")),
			Loc: vloc(c.offset, c.length, anchor)}) {
			detailsAt = len(m.part.Blocks) - 1
		}
	}

	// The descriptions and comments.
	hasDesc := false
	for i, p := range c.props {
		if p.Name == "DESCRIPTION" || p.Name == "COMMENT" {
			used[i] = true
			hasDesc = hasDesc || p.Name == "DESCRIPTION"
			v.paragraphs(p, anchor)
		}
	}

	// Embedded documents: the HTML description, attachments.
	first := len(v.r.doc.Children)
	m.attached = m.attached[:0]
	for i, p := range c.props {
		switch {
		case p.Name == "X-ALT-DESC" && strings.EqualFold(strings.TrimSpace(p.Param("FMTTYPE")), "text/html"):
			used[i] = true
			if hasDesc {
				v.alts++
			}
			v.r.child(m, "", "text/html; charset=utf-8", []byte(vobj.Unescape(v.str(p))), false)
		case p.Name == "ATTACH" && (p.Encoding() == "B" || strings.EqualFold(p.Param("VALUE"), "BINARY")):
			used[i] = true
			name := p.Param("X-FILENAME")
			if name == "" {
				name = p.Param("FILENAME")
			}
			mt := strings.ToLower(strings.TrimSpace(p.Param("FMTTYPE")))
			if mt == "" || !strings.Contains(mt, "/") {
				mt = "application/octet-stream"
			}
			v.r.child(m, m.fileName(name), mt, v.decoded(p), true)
		case p.Name == "ATTACH":
			v.external("ATTACH", strings.TrimSpace(p.Value))
		}
	}
	var children []int
	for n := first; n < len(v.r.doc.Children); n++ {
		children = append(children, n)
	}
	if len(m.attached) > 0 {
		extra := "Attachments: " + strings.Join(m.attached, ", ")
		switch {
		case detailsAt >= 0:
			if v.r.take(m, len(extra)+1) {
				m.part.Blocks[detailsAt].Text += "\n" + extra
			}
		case v.r.take(m, gofastcore.BlockCost+len(extra)):
			at := from
			if at < len(m.part.Blocks) && m.part.Blocks[at].Kind == gofastcore.KindHeading {
				at++
			}
			m.part.Blocks = slices.Insert(m.part.Blocks, at, gofastcore.Block{Kind: gofastcore.KindParagraph, Text: extra, Loc: vloc(c.offset, c.length, anchor)})
		}
		m.attached = m.attached[:0]
	}

	for i, p := range c.props {
		if !used[i] {
			v.attr(&a, "", p)
		}
	}
	v.finishItem(c, from, &a, children)
	for _, s := range nested {
		v.event1(s)
	}
}

func filled(s []string) []string {
	out := s[:0]
	for _, x := range s {
		if x != "" {
			out = append(out, x)
		}
	}
	return out
}

// The properties of a card written in its details paragraph, with their
// labels, in order.
var cardLabels = []struct{ name, label string }{
	{"ORG", "Organization"}, {"TITLE", "Title"}, {"ROLE", "Role"}, {"NICKNAME", "Nickname"},
	{"EMAIL", "Email"}, {"TEL", "Phone"}, {"ADR", "Address"}, {"URL", "URL"}, {"BDAY", "Birthday"},
	{"ANNIVERSARY", "Anniversary"}, {"GENDER", "Gender"}, {"CATEGORIES", "Categories"}, {"IMPP", "IMPP"},
}

// appleLabel reads the label Apple gives a group (item1.X-ABLabel):
// _$!<Mobile>!$_ is "mobile"; a label of the user's own is as written.
func appleLabel(s string) string {
	if t, ok := strings.CutPrefix(s, "_$!<"); ok {
		if t, ok = strings.CutSuffix(t, ">!$_"); ok {
			return strings.ToLower(t)
		}
	}
	return s
}

// card reads one card, then the cards nested in it (a vCard 2.1 AGENT).
func (v *vstate) card(c *comp) {
	m := v.m
	v.cards++
	anchor := v.uid(c)
	from := len(m.part.Blocks)
	var a attrs
	used := make([]bool, len(c.props))

	// Apple's labels of groups.
	labels := map[string]string{}
	labelAt := map[string]int{}
	for i, p := range c.props {
		if p.Name == "X-ABLABEL" && p.Group != "" {
			if t := v.line(v.text(p)); t != "" {
				if _, ok := labels[strings.ToLower(p.Group)]; !ok {
					labels[strings.ToLower(p.Group)] = appleLabel(t)
					labelAt[strings.ToLower(p.Group)] = i
				}
			}
		}
	}

	// The heading: FN, else N, else ORG.
	head := func(i int, t string) {
		if t = gofastcore.ExpandLigatures(t); t != "" {
			used[i] = true
			p := c.props[i]
			v.block(gofastcore.Block{Kind: gofastcore.KindHeading, Level: 1, Text: t, Loc: vloc(p.Offset, p.Length, anchor)})
		}
	}
	heading := func() bool {
		for _, name := range []string{"FN", "N", "ORG"} {
			for i, p := range c.props {
				if p.Name != name {
					continue
				}
				var t string
				switch name {
				case "FN":
					t = v.line(v.text(p))
				case "N":
					n := v.pieces(p, ';')
					var words []string
					for _, k := range []int{3, 1, 2, 0, 4} {
						if k < len(n) && n[k] != "" {
							words = append(words, strings.Join(filled(strings.Split(n[k], ",")), " "))
						}
					}
					t = strings.Join(words, " ")
				case "ORG":
					if n := v.pieces(p, ';'); len(n) > 0 {
						t = n[0]
					}
				}
				if t != "" {
					head(i, t)
					if name == "ORG" {
						used[i] = false // ORG is in the details too
					}
					return true
				}
			}
		}
		return false
	}
	heading()

	// The details.
	var lines []string
	for _, l := range cardLabels {
		for i, p := range c.props {
			if p.Name != l.name {
				continue
			}
			var t string
			switch p.Name {
			case "ORG", "ADR", "GENDER":
				t = strings.Join(filled(v.pieces(p, ';')), ", ")
			case "NICKNAME", "CATEGORIES":
				t = strings.Join(filled(v.pieces(p, ',')), ", ")
			default:
				t = v.line(v.text(p))
			}
			if t == "" {
				continue
			}
			used[i] = true
			var types []string
			for _, ty := range p.ParamValues("TYPE") {
				for _, x := range strings.Split(ty, ",") {
					if x = strings.ToLower(v.line(x)); x != "" && !slices.Contains(types, x) {
						types = append(types, x)
					}
				}
			}
			if g := strings.ToLower(p.Group); g != "" {
				if lb, ok := labels[g]; ok {
					used[labelAt[g]] = true
					if !slices.Contains(types, lb) {
						types = append(types, lb)
					}
				}
			}
			if len(types) > 0 {
				t += " (" + strings.Join(types, ", ") + ")"
			}
			lines = append(lines, l.label+": "+t)
		}
	}
	if len(lines) > 0 {
		v.block(gofastcore.Block{Kind: gofastcore.KindParagraph, Text: gofastcore.ExpandLigatures(strings.Join(lines, "\n")),
			Loc: vloc(c.offset, c.length, anchor)})
	}

	// The notes.
	for i, p := range c.props {
		if p.Name == "NOTE" {
			used[i] = true
			v.paragraphs(p, anchor)
		}
	}

	// The photo and logo.
	for i, p := range c.props {
		if p.Name != "PHOTO" && p.Name != "LOGO" {
			continue
		}
		mt, data, inline := v.picture(p)
		if !inline {
			if uri := strings.TrimSpace(p.Value); uri != "" {
				v.external(p.Name, uri)
			}
			continue
		}
		used[i] = true
		at := len(m.part.Blocks)
		v.r.image(m, mt, "", "", data)
		if len(m.part.Blocks) > at {
			m.part.Blocks[at].Loc = vloc(p.Offset, p.Length, anchor)
		}
	}

	for i, p := range c.props {
		if !used[i] {
			v.attr(&a, "", p)
		}
	}
	v.finishItem(c, from, &a, nil)
	for _, s := range c.subs {
		v.card(s)
	}
}

// picture returns the media type and bytes of a PHOTO or LOGO given in
// the file (base64, or a data: URI), and false for one given by a URI.
func (v *vstate) picture(p *vobj.Property) (string, []byte, bool) {
	mt := ""
	for _, t := range append(p.ParamValues("MEDIATYPE"), p.ParamValues("TYPE")...) {
		t = strings.ToLower(strings.TrimSpace(t))
		switch {
		case strings.HasPrefix(t, "image/"):
			mt = t
		case t != "" && !strings.Contains(t, "/") && rfc822.Token(t):
			mt = "image/" + t
		}
		if mt != "" {
			break
		}
	}
	if p.Encoding() == "B" {
		if mt == "" {
			mt = "application/octet-stream"
		}
		return mt, v.decoded(p), true
	}
	value := strings.TrimSpace(p.Value)
	if len(value) < 5 || !strings.EqualFold(value[:5], "data:") {
		return "", nil, false
	}
	head, data, ok := strings.Cut(value[5:], ",")
	if !ok {
		return "", nil, false
	}
	t, params, _ := strings.Cut(head, ";")
	if t = strings.ToLower(strings.TrimSpace(t)); t != "" {
		mt = t
	} else if mt == "" {
		mt = "text/plain"
	}
	var out []byte
	if strings.Contains(strings.ToLower(params), "base64") {
		out = rfc822.Base64([]byte(data))
	} else {
		out = []byte(percentDecode(data))
	}
	if v.undec || !v.r.dec.Take(int64(len(out))) {
		if !v.undec {
			v.undec = true
			v.m.warn(gofastcore.SeverityError, gofastcore.WarnLimitExceeded, "decode limit reached; the rest of the encoded values are not decoded")
		}
		out = nil
	}
	return mt, out, true
}

// percentDecode undoes the %XX escapes of a data: URI.
func percentDecode(s string) string {
	if strings.IndexByte(s, '%') < 0 {
		return s
	}
	b := make([]byte, 0, len(s))
	for i := 0; i < len(s); i++ {
		if s[i] == '%' && i+2 < len(s) && isHexByte(s[i+1]) && isHexByte(s[i+2]) {
			n, _ := strconv.ParseUint(s[i+1:i+3], 16, 8)
			b = append(b, byte(n))
			i += 2
			continue
		}
		b = append(b, s[i])
	}
	return string(b)
}

func isHexByte(c byte) bool {
	return c >= '0' && c <= '9' || c >= 'a' && c <= 'f' || c >= 'A' && c <= 'F'
}

// finish sets the metadata, writes the warnings of the whole file, and
// finishes the part.
func (v *vstate) finish(pr vobj.Problems, what string) {
	m, md := v.m, &v.r.doc.Metadata
	var a attrs
	for _, p := range v.meta {
		switch {
		case v.cal && p.Name == "X-WR-CALNAME" && md.Title == "":
			md.Title = gofastcore.ExpandLigatures(v.line(v.text(p)))
			if md.Title != "" {
				continue
			}
		case v.cal && p.Name == "PRODID" && md.Application == "":
			md.Application = v.line(v.text(p))
			if md.Application != "" {
				continue
			}
		}
		v.attr(&a, "", p)
	}
	if len(v.tzids) > 0 {
		for _, t := range v.tzids {
			a.add("timezones", t)
		}
	}
	if !v.cal {
		a.add("cards", strconv.Itoa(v.cards))
	}
	for _, k := range a.keys {
		if md.Extra == nil {
			md.Extra = map[string]string{}
		}
		md.Extra[k] = strings.Join(a.vals[k], "\n")
	}

	if pr.UnmatchedEnd > 0 {
		m.warn(gofastcore.SeverityInfo, gofastcore.WarnInputRepaired, plural(pr.UnmatchedEnd, "END line that matches no open component is skipped", "END lines that match no open component are skipped"))
	}
	if pr.MissingEnd > 0 {
		m.warn(gofastcore.SeverityInfo, gofastcore.WarnInputRepaired, fmt.Sprintf("%s no END line; each ends where the component around it or the file ends", plural(pr.MissingEnd, "component has", "components have")))
	}
	if pr.NotLines > 0 {
		m.warn(gofastcore.SeverityInfo, gofastcore.WarnInputRepaired, fmt.Sprintf("%s (no colon, or no name before it); each such line is read as continuing the value of the property before it", plural(pr.NotLines, "line is not a content line", "lines are not content lines")))
	}
	if pr.TooDeep > 0 {
		m.warn(gofastcore.SeverityError, gofastcore.WarnLimitExceeded, fmt.Sprintf("components nested more than %d deep are not read (%s)", MaxComponentDepth, things(pr.TooDeep, "component")))
	}
	if pr.LongLines > 0 {
		m.warn(gofastcore.SeverityError, gofastcore.WarnLimitExceeded, fmt.Sprintf("content lines longer than %d bytes are cut there (%s)", MaxContentLineBytes, things(pr.LongLines, "line")))
	}
	if v.over > 0 {
		m.warn(gofastcore.SeverityError, gofastcore.WarnLimitExceeded, fmt.Sprintf("components with more than %d properties and components in them are not read past that (%s left out)", MaxComponentItems, things(v.over, "item")))
	}
	if v.alts > 0 {
		m.warn(gofastcore.SeverityWarning, WarnAlternatives, fmt.Sprintf("%s a DESCRIPTION and an HTML description (X-ALT-DESC); the blocks are the DESCRIPTION, the HTML one is an embedded document", plural(v.alts, "component has", "components have")))
	}
	if v.bare > 0 {
		m.warn(gofastcore.SeverityInfo, WarnComponentWithoutText, fmt.Sprintf("components with no text give no block, so their properties are not in the output: %d (%s)", v.bare, strings.Join(v.kinds, ", ")))
	}
	if len(m.part.Blocks) == 0 {
		m.warn(gofastcore.SeverityInfo, gofastcore.WarnPartNoText, "the "+what+" has no text")
	}
	m.finish()
}

// plural writes "1 component has" or "3 components have".
func plural(n int, one, many string) string {
	if n == 1 {
		return "1 " + one
	}
	return strconv.Itoa(n) + " " + many
}
