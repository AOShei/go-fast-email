package gofastemail

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"image"
	"image/gif"
	"image/jpeg"
	"image/png"
	"io"
	"net/mail"
	"strconv"
	"strings"

	"github.com/AOShei/go-fast-core"
	"github.com/AOShei/go-fast-core/rfc822"
	"github.com/AOShei/go-fast-email/internal/clean"
	"github.com/AOShei/go-fast-email/internal/textbody"
)

// Keys of Block.Attrs this library sets.
const (
	// AttrRole: "headers" on the block of the headers a mail program
	// shows; "signature" on a block of the signature.
	AttrRole = gofastcore.AttrRole
	// AttrChildren: on the headers block, the indexes in
	// Document.Children of the documents that came from this message,
	// comma separated.
	AttrChildren = "children"
)

// Values of Attrs["role"] and of the note type this library uses. They are
// core's values (gofastcore.RoleHeaders, NoteHeaders and so on).
const (
	RoleHeaders   = gofastcore.RoleHeaders
	RoleSignature = gofastcore.RoleSignature // the value of textbody.RoleSignature
	// NoteHeaders: a note block that holds the headers a mail program does
	// not show, one "Name: value" per line (in a mailbox). No display
	// reason: the format declares none.
	NoteHeaders = gofastcore.NoteHeaders
	// NotePreamble and NoteEpilogue: the text of a multipart body before
	// its first part and after its last, which no mail program shows. No
	// display reason: the format declares none.
	NotePreamble = gofastcore.NoteMIMEPreamble
	NoteEpilogue = gofastcore.NoteMIMEEpilogue
)

// maxNameBytes bounds a part name, made from the subject (which is in
// full in the heading). A subject of 90 Chinese characters is 270 bytes:
// the bound is well above what mail holds.
const maxNameBytes = 4096

// shown lists the headers a mail program shows above a message, in the
// order they are written in the headers block. The subject is the heading.
var shown = []struct{ key, label string }{
	{"from", "From"}, {"to", "To"}, {"cc", "Cc"}, {"bcc", "Bcc"}, {"date", "Date"},
}

// reader holds what is shared while one file is read.
type reader struct {
	e    *Extractor
	ctx  context.Context
	opts *gofastcore.Options
	text *gofastcore.TextBudget
	dec  *gofastcore.DecodeBudget
	doc  *gofastcore.Document
}

// message is the state of one message while it is read.
type message struct {
	// part is the message's part. Its blocks are the heading and the
	// headers, then the body's blocks as they are read.
	part     gofastcore.Part
	cut      bool // the text budget is spent
	undecode bool // the decode budget is spent
	parts    int  // MIME parts seen
	children []int
	attached []string
	warned   map[string]bool
	groups   map[string]*group
	head     map[string]rfc822.Cuts // see headCuts
	props    int                    // property readers made, in an Outlook message or TNEF stream
	order    []*group               // groups in the order of their warnings
	inline   bool                   // a single message: its headers go to the metadata
	controls int                    // control characters removed from its text
	def      string                 // Extractor.Charset: the set of text that declares none
}

// decoding is how the message's header text is decoded.
func (m *message) decoding() *rfc822.Decoding {
	return &rfc822.Decoding{Default: m.def, Report: m.charsetReport()}
}

// text is clean.Text for text that is output: it counts the control
// characters removed.
func (m *message) text(s string) string {
	t, n := clean.TextN(s)
	m.controls += n
	return t
}

// line is clean.Line for text that is output (a header value, a
// subject, a name): it counts the control characters removed.
func (m *message) line(s string) string {
	t, n := clean.LineN(s)
	m.controls += n
	return t
}

func (m *message) warn(sev gofastcore.Severity, code, msg string) {
	key := code + "\x00" + msg
	if m.warned[key] {
		return
	}
	if m.warned == nil {
		m.warned = map[string]bool{}
	}
	m.warned[key] = true
	m.part.Warn(sev, code, msg)
}

// The bounds of a warning that stands for many values.
const (
	maxListed = 8      // values named in its message
	maxSeen   = 10_000 // distinct values counted
)

// group is one warning of a part that stands for many occurrences: the
// distinct values of one kind (character sets not decoded, transfer
// encodings not known, ...) or the items one bound cut. It is written
// when the first occurs, in the order of the part's warnings, and its
// message is set to cover the others when the part is finished.
type group struct {
	at    int // index of its warning in part.Warnings, if it was kept
	code  string
	first string // the message it was written with
	n     int    // distinct values, or items counted
	over  bool
	vals  []string // the first maxListed values, as they are named
	seen  map[string]struct{}
	raw   bool // the values are written as they are, joined by "; "
	many  func(count, list string) string
	count func(n int) string
}

// list names the values: the first maxListed, then how many more.
func (g *group) list() string {
	var b strings.Builder
	for i, v := range g.vals {
		switch {
		case i > 0 && g.raw:
			b.WriteString("; ")
		case i > 0:
			b.WriteString(", ")
		}
		if g.raw {
			b.WriteString(v)
		} else {
			b.WriteString(strconv.Quote(v))
		}
	}
	switch {
	case g.over:
		b.WriteString(" and others")
	case g.n > len(g.vals):
		fmt.Fprintf(&b, " and %d more", g.n-len(g.vals))
	}
	return b.String()
}

func (m *message) group(key string) (*group, bool) {
	g, ok := m.groups[key]
	if !ok {
		if m.groups == nil {
			m.groups = map[string]*group{}
		}
		g = &group{at: len(m.part.Warnings)}
		m.groups[key] = g
		m.order = append(m.order, g)
	}
	return g, ok
}

// warnEach warns once per part of a kind of warning that can be given
// for many distinct values (a character set, a transfer encoding, a
// media type). The first value gives the warning, with the message one;
// if more distinct values follow, its message is many(count, list) once
// the part is finished, the list naming each value as named gives it.
// raw: the names are written as they are, not quoted.
func (m *message) warnEach(sev gofastcore.Severity, code, key, value, named, one string, raw bool, many func(count, list string) string) {
	g, ok := m.group(code + "\x00" + key)
	if !ok {
		g.seen, g.raw, g.many = map[string]struct{}{value: {}}, raw, many
		g.n, g.vals = 1, []string{named}
		g.code, g.first = code, one
		m.part.Warn(sev, code, one)
		return
	}
	if _, dup := g.seen[value]; dup || g.over {
		return
	}
	if len(g.seen) >= maxSeen {
		g.over = true
		return
	}
	g.seen[value] = struct{}{}
	g.n++
	if len(g.vals) < maxListed {
		g.vals = append(g.vals, named)
	}
}

// warnCount warns once per part that a bound cut n items; the message is
// text(total) once the part is finished.
func (m *message) warnCount(code, key string, n int, text func(total int) string) {
	g, ok := m.group(code + "\x00" + key)
	g.n += n
	if !ok {
		g.count, g.code, g.first = text, code, text(g.n)
		m.part.Warn(gofastcore.SeverityError, code, g.first)
	}
}

// finish sets the messages of the grouped warnings and finishes the part.
// A grouped warning is rewritten only where it is still at its place: a
// part may keep only so many warnings, and then it was never written.
func (m *message) finish() {
	if m.controls > 0 {
		msg := strconv.Itoa(m.controls) + " control characters with no meaning as text were removed from the message"
		if m.controls == 1 {
			msg = "1 control character with no meaning as text was removed from the message"
		}
		m.part.Warn(gofastcore.SeverityInfo, gofastcore.WarnTextControlCharactersRemoved, msg)
		m.controls = 0
	}
	ws := m.part.Warnings
	for _, g := range m.order {
		if g.at >= len(ws) || ws[g.at].Code != g.code || ws[g.at].Message != g.first {
			continue
		}
		switch {
		case g.count != nil:
			ws[g.at].Message = g.count(g.n)
		case g.n > 1 || g.over:
			count := strconv.Itoa(g.n)
			if g.over {
				count = fmt.Sprintf("more than %d", maxSeen)
			}
			ws[g.at].Message = g.many(count, g.list())
		}
	}
	m.order, m.groups = nil, nil
	m.part.Finish()
}

// things writes a count of a noun: "1 word", "2 words", "3 entries".
func things(n int, noun string) string {
	switch {
	case n == 1:
		return "1 " + noun
	case strings.HasSuffix(noun, "y"):
		return strconv.Itoa(n) + " " + noun[:len(noun)-1] + "ies"
	}
	return strconv.Itoa(n) + " " + noun + "s"
}

func (m *message) charsetReport() rfc822.CharsetFunc {
	return func(label string, res rfc822.CharsetResult) {
		if res.Detected != "" {
			m.warnEach(gofastcore.SeverityWarning, gofastcore.WarnTextEncodingDetected, "detected", res.Detected, res.Detected,
				fmt.Sprintf("text that declares no character set and is not UTF-8 was read as %s, recognised from its bytes; if that is wrong, give the set with Extractor.Charset (command: -charset)", res.Detected), false,
				func(count, list string) string {
					return fmt.Sprintf("text that declares no character set and is not UTF-8 was read in %s sets recognised from its bytes (%s); if that is wrong, give the set with Extractor.Charset (command: -charset)", count, list)
				})
			if res.Replaced {
				m.warn(gofastcore.SeverityWarning, gofastcore.WarnTextUnmappedGlyphs, "bytes not valid in text recognised as "+res.Detected)
			}
			return
		}
		switch {
		case res.Undetermined:
			m.warn(gofastcore.SeverityWarning, gofastcore.WarnTextUnmappedGlyphs,
				"bytes outside ASCII in text that declares no character set are U+FFFD: the text is not UTF-8 and no character set is certain from its bytes; give the set with Extractor.Charset (command: -charset)")
		case label == "" && res.Replaced:
			m.warn(gofastcore.SeverityWarning, gofastcore.WarnTextUnmappedGlyphs, "bytes that are not UTF-8 in text that declares no character set")
		case !res.Known && res.Replaced:
			// All-ASCII text in such a set loses nothing: no warning.
			named := clean.Cut(clean.Line(label), 64)
			m.warnEach(gofastcore.SeverityWarning, gofastcore.WarnTextUnmappedGlyphs, "unknown", label, named,
				fmt.Sprintf("character set %q is not decoded; bytes outside ASCII are U+FFFD", named), false,
				func(count, list string) string {
					return fmt.Sprintf("%s character sets are not decoded (%s); bytes outside ASCII in them are U+FFFD", count, list)
				})
		case res.Replaced:
			named := clean.Cut(clean.Line(label), 64)
			m.warnEach(gofastcore.SeverityWarning, gofastcore.WarnTextUnmappedGlyphs, "invalid", label, named,
				fmt.Sprintf("bytes not valid in character set %q", named), false,
				func(count, list string) string {
					return fmt.Sprintf("bytes not valid in %s character sets (%s)", count, list)
				})
		}
	}
}

// cuts warns of what a bound of the MIME reader left unread or
// undecoded, once per part and bound, with the count.
func (m *message) cuts(c rfc822.Cuts) {
	if c.LongWords > 0 {
		m.warnCount(gofastcore.WarnLimitExceeded, "long words", c.LongWords, func(n int) string {
			return fmt.Sprintf("encoded words that do not end within %d bytes are left as written, not decoded (%s)", rfc822.MaxWord, things(n, "word"))
		})
	}
	if c.LongLabels > 0 {
		m.warnCount(gofastcore.WarnLimitExceeded, "long labels", c.LongLabels, func(n int) string {
			return fmt.Sprintf("encoded words whose character set name is longer than %d bytes are left as written, not decoded (%s)", rfc822.MaxLabel, things(n, "word"))
		})
	}
	if c.Pieces > 0 {
		m.warnCount(gofastcore.WarnLimitExceeded, "pieces", c.Pieces, func(n int) string {
			return fmt.Sprintf("RFC 2231 parameter pieces numbered above %d are not read (%s)", rfc822.MaxPiece, things(n, "piece"))
		})
	}
}

// headCuts returns the cuts to count for a header whose parameters are
// read. In the message's own header (depth 0) it was decoded as a field
// too, where words outside the parameters read are seen, and the
// parameters where pieces are: each bound counts the more it found of the
// two, so that a word is counted once.
func (m *message) headCuts(depth int, key string, c rfc822.Cuts) rfc822.Cuts {
	if depth > 0 {
		return c
	}
	h := m.head[key]
	return rfc822.Cuts{
		LongWords:  max(c.LongWords, h.LongWords),
		LongLabels: max(c.LongLabels, h.LongLabels),
		Pieces:     max(c.Pieces, h.Pieces),
	}
}

// maxFileNameBytes bounds an attachment's file name.
const maxFileNameBytes = 4096

// maxImageText bounds the description of an image.
const maxImageText = 4096

// maxReference bounds a URL or path of content outside the message, as a
// warning names it. The text of a warning is not output: the cut is noted
// in the warning itself, not reported as a limit.
const maxReference = 4096

// reference returns a URL or path of content outside the message quoted,
// cut to maxReference bytes with a note of the cut.
func (m *message) reference(s string) string {
	if len(s) <= maxReference {
		return strconv.Quote(s)
	}
	return fmt.Sprintf("%q (cut at %d of its %d bytes)", clean.Cut(s, maxReference), maxReference, len(s))
}

// fileName returns an attachment's file name as one line, cut in the
// middle where it is longer than maxFileNameBytes so that its extension
// is kept; a cut is reported.
func (m *message) fileName(s string) string {
	s = clean.Line(s)
	if len(s) <= maxFileNameBytes {
		return s
	}
	m.warnCount(gofastcore.WarnLimitExceeded, "file names", 1, func(n int) string {
		return fmt.Sprintf("attachment file names longer than %d bytes are cut to %d by leaving out the middle and keeping the extension (%s)", maxFileNameBytes, maxFileNameBytes, things(n, "name"))
	})
	ext := ""
	if i := strings.LastIndexByte(s, '.'); i >= 0 && len(s)-i <= 255 {
		ext = s[i:]
	}
	const mark = "\u2026" // HORIZONTAL ELLIPSIS
	return clean.Cut(s, maxFileNameBytes-len(ext)-len(mark)) + mark + ext
}

// take takes n bytes from the text budget, and warns once when it is
// spent.
func (r *reader) take(m *message, n int) bool {
	if m.cut {
		return false
	}
	if !r.text.Take(n) {
		m.cut = true
		m.warn(gofastcore.SeverityError, gofastcore.WarnLimitExceeded, "text limit reached")
		return false
	}
	return true
}

// eml reads a file of one message.
func (r *reader) eml(src io.ReaderAt, size int64) (*gofastcore.Document, error) {
	if !r.opts.Parts.Contains(1) {
		return nil, fmt.Errorf("gofastemail: %w", gofastcore.ErrNoPartsSelected)
	}
	limit := r.e.maxMessage()
	n := min(size, limit)
	raw := getBuffer(n)
	defer putBuffer(raw)
	if got, err := src.ReadAt(raw, 0); int64(got) < n {
		return nil, fmt.Errorf("gofastemail: reading: %v: %w", err, gofastcore.ErrMalformed)
	}
	raw = bytes.TrimPrefix(raw, []byte("\xEF\xBB\xBF"))
	// The header is read once: for whether this is a message at all, and
	// for the message. It is read until the message is.
	s := getScratch()
	defer putScratch(s)
	h, bodyAt, prob := s.ParseHeader(raw, r.e.maxHeader())
	looks := len(h) > 0 && s.First() < lookLimit
	if len(h) == 0 && r.e.maxHeader() < lookLimit {
		// The limit on the header is below the look's: the look may
		// find a field this reading did not reach.
		looks = looksLikeMessage(raw)
	}
	if !looks {
		return nil, fmt.Errorf("gofastemail: not an email message: %w", gofastcore.ErrMalformed)
	}
	r.doc = gofastcore.New("eml")
	r.doc.Source = gofastcore.Source{Format: "eml", Name: r.opts.Name, MIME: "message/rfc822", Size: size}
	m := &message{inline: true, def: r.e.Charset}
	if size > n {
		m.warn(gofastcore.SeverityError, gofastcore.WarnLimitExceeded, fmt.Sprintf("message of %d bytes read to %d", size, n))
	}
	r.readMessage(m, raw, 1, "", h, bodyAt, prob)
	if err := r.ctx.Err(); err != nil {
		return nil, err
	}
	r.doc.Parts = append(r.doc.Parts, m.part)
	return r.finish(), nil
}

// finish sets what is set once the parts are read.
func (r *reader) finish() *gofastcore.Document {
	return r.doc
}

// lookLimit is how far into a file looksLikeMessage looks for a header
// field.
const lookLimit = 64 << 10

// looksLikeMessage reports whether b begins with a header field.
func looksLikeMessage(b []byte) bool {
	s := getScratch()
	defer putScratch(s)
	h, _, _ := s.ParseHeader(b, lookLimit)
	return len(h) > 0
}

// message reads one message into m.part. envelope is the "From " line of
// a mailbox, without "From ".
func (r *reader) message(m *message, raw []byte, number int, envelope string) {
	// The header is read until the message is: its values are made into
	// strings where they are kept.
	s := getScratch()
	defer putScratch(s)
	h, bodyAt, prob := s.ParseHeader(raw, r.e.maxHeader())
	r.readMessage(m, raw, number, envelope, h, bodyAt, prob)
}

// readMessage reads one message, whose header has been read, into m.part.
func (r *reader) readMessage(m *message, raw []byte, number int, envelope string, h rfc822.Header, bodyAt int, prob rfc822.Problems) {
	m.part.Kind, m.part.Number = gofastcore.PartMessage, number
	r.headerProblems(m, prob)
	report := m.decoding()

	// The header fields as text, in order.
	type field struct{ key, name, value string }
	fields := make([]field, 0, len(h))
	for _, f := range h {
		d, c := rfc822.DecodeText(f.Value, report)
		key := strings.ToLower(f.Name)
		if _, held := m.head[key]; !held && (key == "content-type" || key == "content-disposition") {
			// The one whose parameters are read: counted there, with what
			// reading the parameters finds (see entity).
			if m.head == nil {
				m.head = map[string]rfc822.Cuts{}
			}
			m.head[key] = c
		} else {
			m.cuts(c)
		}
		v := m.line(d)
		fields = append(fields, field{key, clean.Line(f.Name), v})
	}
	get := func(key string) []string {
		var out []string
		for _, f := range fields {
			if f.key == key && f.value != "" {
				out = append(out, f.value)
			}
		}
		return out
	}
	subject := strings.Join(get("subject"), " ")
	var lines []string
	for _, s := range shown {
		for _, v := range get(s.key) {
			lines = append(lines, s.label+": "+v)
		}
	}
	headersAt := r.head(m, subject, lines)

	if m.inline {
		md := &r.doc.Metadata
		md.Title = gofastcore.ExpandLigatures(subject)
		if from := get("from"); len(from) > 0 {
			md.Authors = []string{gofastcore.ExpandLigatures(from[0])}
		}
		if d := get("date"); len(d) > 0 {
			if t, err := mail.ParseDate(d[0]); err == nil {
				md.Created = t
			}
		}
		md.Extra = joinFields(md.Extra, len(fields), func(yield func(k, v string)) {
			for _, f := range fields {
				yield(f.key, f.value)
			}
		})
	}
	if !m.inline {
		// In a mailbox the metadata is the file's, so the headers that are
		// not shown are a note of the message's part.
		var unshown []headerField
		if envelope != "" {
			unshown = append(unshown, headerField{"From ", "From " + envelope, envelope})
		}
		for _, f := range fields {
			if !isShown(f.key) {
				unshown = append(unshown, headerField{f.name, f.name + ": " + f.value, f.value})
			}
		}
		r.headersNote(m, unshown)
	}

	r.entity(m, h, raw[bodyAt:], 0, false, "text/plain")
	r.tail(m, headersAt)
}

// joinFields adds header values to extra (made if nil and there are any)
// by key, a key given more than once with its values joined by "\n" in
// order. The values of a key are joined once, not one at a time, which
// would copy what is joined so far for each value.
func joinFields(extra map[string]string, n int, fields func(yield func(k, v string))) map[string]string {
	byKey := make(map[string][]string, min(n, 64))
	var order []string
	fields(func(k, v string) {
		if k == "" {
			return
		}
		if _, ok := byKey[k]; !ok {
			order = append(order, k)
		}
		byKey[k] = append(byKey[k], v)
	})
	if len(order) > 0 && extra == nil {
		extra = map[string]string{}
	}
	for _, k := range order {
		extra[k] = strings.Join(byKey[k], "\n")
	}
	return extra
}

// headerField is a header a mail program does not show: its name, the
// line as written in a note ("Name: value") and its value.
type headerField struct{ name, line, value string }

// headersNote adds, in a mailbox, the note of the headers a mail program
// does not show (one "Name: value" per line). A header is a header
// whatever it holds: it keeps its provenance in the note's type, and no
// display reason is set (the format declares none).
func (r *reader) headersNote(m *message, fields []headerField) {
	lines := make([]string, len(fields))
	for i, f := range fields {
		lines[i] = f.line
	}
	if t := gofastcore.ExpandLigatures(strings.TrimSpace(strings.Join(lines, "\n"))); t != "" && r.take(m, gofastcore.BlockCost+len(t)) {
		m.part.Blocks = append(m.part.Blocks, gofastcore.Block{Kind: gofastcore.KindNote, Text: t,
			Attrs: map[string]string{gofastcore.AttrNoteType: NoteHeaders}})
	}
}

// head adds the heading that holds the subject and the block of the
// headers a mail program shows, and returns the index of that block, or
// -1 if there is none.
func (r *reader) head(m *message, subject string, lines []string) int {
	if subject != "" {
		m.part.Name = gofastcore.ExpandLigatures(clean.Cut(subject, maxNameBytes))
		if len(subject) > maxNameBytes {
			m.warn(gofastcore.SeverityError, gofastcore.WarnLimitExceeded, fmt.Sprintf("the part name, made from a subject of %d bytes, is cut to %d bytes (the heading has the subject in full)", len(subject), maxNameBytes))
		}
		heading := gofastcore.Block{Kind: gofastcore.KindHeading, Level: 1, Text: gofastcore.ExpandLigatures(subject)}
		if r.take(m, gofastcore.BlockCost+len(heading.Text)) {
			m.part.Blocks = append(m.part.Blocks, heading)
		}
	}
	text := gofastcore.ExpandLigatures(strings.Join(lines, "\n"))
	if len(lines) == 0 || !r.take(m, gofastcore.BlockCost+len(text)) {
		return -1
	}
	m.part.Blocks = append(m.part.Blocks, gofastcore.Block{Kind: gofastcore.KindParagraph, Text: text,
		Attrs: map[string]string{AttrRole: RoleHeaders}})
	return len(m.part.Blocks) - 1
}

// tail adds the names of the attachments to the headers block, and
// finishes the part. The body's blocks are in the part already: nothing
// is added to it between the headers and the body.
func (r *reader) tail(m *message, headersAt int) {
	if headersAt >= 0 && (len(m.attached) > 0 || len(m.children) > 0) {
		b := &m.part.Blocks[headersAt]
		if len(m.attached) > 0 {
			extra := "\nAttachments: " + strings.Join(m.attached, ", ")
			if r.take(m, len(extra)) {
				b.Text += extra
			}
		}
		idx := make([]string, len(m.children))
		for i, c := range m.children {
			idx[i] = strconv.Itoa(c)
		}
		b.Attrs[AttrChildren] = strings.Join(idx, ",")
	}
	if len(m.part.Blocks) == 0 {
		m.warn(gofastcore.SeverityInfo, gofastcore.WarnPartNoText, "the message has no text")
	}
	m.finish()
}

func isShown(key string) bool {
	if key == "subject" {
		return true
	}
	for _, s := range shown {
		if s.key == key {
			return true
		}
	}
	return false
}

func (r *reader) headerProblems(m *message, p rfc822.Problems) {
	r.headerLimits(m, p)
	if p.Malformed {
		m.warn(gofastcore.SeverityInfo, gofastcore.WarnInputRepaired, "a header section ends at a line that is not a header field; the body is taken to begin there")
	}
}

// headerLimits warns where a header section was cut by a bound.
func (r *reader) headerLimits(m *message, p rfc822.Problems) {
	if p.Truncated {
		m.warn(gofastcore.SeverityError, gofastcore.WarnLimitExceeded, fmt.Sprintf("a header section is longer than %d bytes; the rest of it is not read", r.e.maxHeader()))
	}
	if p.TooMany {
		m.warn(gofastcore.SeverityError, gofastcore.WarnLimitExceeded, fmt.Sprintf("a header section has more than %d fields; the rest are not read", rfc822.MaxFields))
	}
}

// entity reads one MIME entity: its header h and its body. alt is set
// inside an alternative that does not give the message's blocks: there
// every text part is extracted as an embedded document.
func (r *reader) entity(m *message, h rfc822.Header, body []byte, depth int, alt bool, defaultType string) {
	if r.ctx.Err() != nil {
		return
	}
	m.parts++
	if m.parts > r.e.maxParts() {
		m.warn(gofastcore.SeverityError, gofastcore.WarnLimitExceeded, fmt.Sprintf("the message has more than %d MIME parts; the rest are not read", r.e.maxParts()))
		return
	}
	report := m.decoding()
	mediatype, params := defaultType, map[string]string{}
	if v, ok := h.Get("Content-Type"); ok {
		t, p, c := rfc822.Params(v, report)
		m.cuts(m.headCuts(depth, "content-type", c))
		if strings.Contains(t, "/") {
			mediatype, params = t, p
		} else {
			params = p
		}
	}
	var disposition string
	dparams := map[string]string{}
	if v, ok := h.Get("Content-Disposition"); ok {
		var c rfc822.Cuts
		disposition, dparams, c = rfc822.Params(v, report)
		m.cuts(m.headCuts(depth, "content-disposition", c))
	}
	name := dparams["filename"]
	if name == "" {
		name = params["name"]
	}
	name = m.fileName(name)
	cte := ""
	if v, ok := h.Get("Content-Transfer-Encoding"); ok {
		cte = strings.ToLower(strings.TrimSpace(rfc822.DecodeRaw(v, nil)))
	}
	attachment := disposition == "attachment"

	if strings.HasPrefix(mediatype, "multipart/") {
		if depth >= r.e.maxDepth() {
			m.warn(gofastcore.SeverityError, gofastcore.WarnLimitExceeded, fmt.Sprintf("multipart bodies nest more than %d deep; the deeper ones are not read", r.e.maxDepth()))
			return
		}
		boundary := params["boundary"]
		if cte == "base64" || cte == "quoted-printable" {
			// Not allowed for a multipart body, and written all the same.
			var ok bool
			if body, ok = r.decode(m, cte, body); !ok {
				return
			}
		}
		if boundary == "" {
			m.warnEach(gofastcore.SeverityWarning, gofastcore.WarnInputRepaired, "no boundary", mediatype, mediatype, mediatype+" body with no boundary; read as text", false,
				func(count, list string) string {
					return fmt.Sprintf("bodies of %s media types with no boundary (%s); read as text", count, list)
				})
			r.leaf(m, "text/plain", params, name, false, alt, body)
			return
		}
		r.multipart(m, mediatype, boundary, body, depth, alt)
		return
	}
	if mediatype == "message/external-body" {
		// The content is somewhere else, named by the parameters. It is
		// passed on as it is and never fetched.
		where := params["url"]
		if where == "" {
			where = params["name"]
			if s := params["site"]; s != "" {
				where = s + ":" + where
			}
		}
		access := clean.Cut(clean.Line(params["access-type"]), 64)
		quoted := m.reference(clean.Line(where))
		m.warnEach(gofastcore.SeverityWarning, gofastcore.WarnInputExternalReference, "external body", params["access-type"]+"\x00"+where,
			fmt.Sprintf("%s (access type %q)", quoted, access),
			fmt.Sprintf("a part whose content is outside the message (access type %q): %s", access, quoted), true,
			func(count, list string) string {
				return fmt.Sprintf("parts refer to %s places outside the message for their content: %s", count, list)
			})
	}
	data, _ := r.decode(m, cte, body) // nil when over the limit
	if strings.HasPrefix(mediatype, "image/") && !attachment && !alt {
		// A picture a mail program shows in the message: a block at its
		// place, and an embedded document as well.
		var desc string
		if v, ok := h.Get("Content-Description"); ok {
			var c rfc822.Cuts
			desc, c = rfc822.DecodeText(v, report)
			if depth > 0 {
				m.cuts(c) // the message's own header is counted where it is read
			}
		}
		r.image(m, mediatype, name, desc, data)
	}
	r.leaf(m, mediatype, params, name, attachment, alt, data)
}

// image adds an image block for a picture shown in the message: its
// description as text, its file name in Attrs["name"], an ID from its
// bytes, its pixel size where the format is one the standard library
// reads, and with ImagesData the bytes.
func (r *reader) image(m *message, mediatype, name, desc string, data []byte) {
	if r.opts.Images == gofastcore.ImagesNone {
		return
	}
	desc, controls := clean.LineN(desc)
	text := gofastcore.ExpandLigatures(clean.Cut(desc, maxImageText))
	if !r.take(m, gofastcore.BlockCost+len(text)) {
		return
	}
	if text != "" {
		m.controls += controls
	}
	if len(desc) > maxImageText {
		m.warnCount(gofastcore.WarnLimitExceeded, "image text", 1, func(n int) string {
			return fmt.Sprintf("image descriptions longer than %d bytes are cut to %d (%s)", maxImageText, maxImageText, things(n, "description"))
		})
	}
	img := &gofastcore.Image{MIME: mediatype}
	if len(data) > 0 {
		sum := sha256.Sum256(data)
		img.ID = "sha256-" + hex.EncodeToString(sum[:12])
		img.Width, img.Height = imageSize(mediatype, data)
		if r.opts.Images == gofastcore.ImagesData {
			// A copy: data may be the bytes of the message, whose buffer
			// is reused once the message is read.
			img.Data = bytes.Clone(data)
		}
	}
	b := gofastcore.Block{Kind: gofastcore.KindImage, Text: text, Image: img}
	if name != "" {
		b.Attrs = map[string]string{gofastcore.AttrName: name}
	}
	m.part.Blocks = append(m.part.Blocks, b)
}

// imageSize returns the pixel size of a PNG, JPEG or GIF image from its
// header, or zeros.
func imageSize(mediatype string, data []byte) (int, int) {
	var decode func(io.Reader) (image.Config, error)
	switch mediatype {
	case "image/png":
		decode = png.DecodeConfig
	case "image/jpeg", "image/jpg", "image/pjpeg":
		decode = jpeg.DecodeConfig
	case "image/gif":
		decode = gif.DecodeConfig
	default:
		return 0, 0
	}
	c, err := decode(bytes.NewReader(data))
	if err != nil || c.Width < 0 || c.Height < 0 {
		return 0, 0
	}
	return c.Width, c.Height
}

// leaf handles a part that is not multipart. data is nil when its content
// could not be decoded within the limits; it is then listed only.
func (r *reader) leaf(m *message, mediatype string, params map[string]string, name string, attachment, alt bool, data []byte) {
	switch {
	case mediatype == "text/plain" && !attachment && !alt:
		r.textBody(m, params, data)
	case mediatype == "text/html" && !attachment:
		r.child(m, name, m.withCharset(mediatype, params), data, false)
	case mediatype == "text/plain" && alt:
		r.child(m, name, m.withCharset(mediatype, params), data, false)
	case isTNEF(mediatype) || strings.EqualFold(name, "winmail.dat"):
		r.tnefChild(m, name, mediatype, data, true)
	case strings.HasPrefix(mediatype, "message/"):
		r.child(m, name, mediatype, data, true)
	case strings.HasPrefix(mediatype, "text/"):
		r.child(m, name, m.withCharset(mediatype, params), data, attachment || name != "")
	default:
		r.child(m, name, mediatype, data, true)
	}
}

// maxCharsetLabel bounds the character set name passed on in the media
// type of an embedded document. The longest registered name is 45 bytes.
const maxCharsetLabel = 63

// withCharset returns the media type with its charset parameter, which the
// embedded document's extractor needs to read text. A name longer than
// maxCharsetLabel is not passed on, and that is reported.
func (m *message) withCharset(mediatype string, params map[string]string) string {
	cs := rfc822.NormalizeCharset(params["charset"])
	if cs == "" || !rfc822.Token(cs) {
		return mediatype
	}
	if len(cs) > maxCharsetLabel {
		m.warnCount(gofastcore.WarnLimitExceeded, "charset labels", 1, func(n int) string {
			return fmt.Sprintf("character set names longer than %d bytes are not passed on in the media type of an embedded document (%s)", maxCharsetLabel, things(n, "name"))
		})
		return mediatype
	}
	return mediatype + "; charset=" + cs
}

// textBody makes blocks of a text/plain body.
func (r *reader) textBody(m *message, params map[string]string, data []byte) {
	if data == nil {
		return
	}
	label := params["charset"]
	text, res := rfc822.DecodeCharsetDefault(label, m.def, data)
	if !res.Known || res.Replaced || res.Detected != "" {
		if label == "" {
			label = "us-ascii"
		}
		m.charsetReport()(rfc822.NormalizeCharset(label), res)
	}
	text = m.text(text)
	o := textbody.Options{
		Flowed: strings.EqualFold(params["format"], "flowed"),
		DelSp:  strings.EqualFold(params["delsp"], "yes"),
	}
	m.part.Blocks, _ = textbody.Blocks(m.part.Blocks, text, o, func(n int) bool { return r.take(m, n) })
}

// multipart reads a multipart body.
func (r *reader) multipart(m *message, mediatype, boundary string, body []byte, depth int, alt bool) {
	left := max(r.e.maxParts()-m.parts, 0)
	mp := rfc822.Split(body, boundary, left+1)
	if len(mp.Parts) == 0 {
		m.warnEach(gofastcore.SeverityWarning, gofastcore.WarnInputRepaired, "boundary not found", mediatype, mediatype, mediatype+" body in which the boundary is never found; read as text", false,
			func(count, list string) string {
				return fmt.Sprintf("bodies of %s media types in which the boundary is never found (%s); read as text", count, list)
			})
		r.leaf(m, "text/plain", nil, "", false, alt, body)
		return
	}
	if !mp.Closed {
		m.warnEach(gofastcore.SeverityInfo, gofastcore.WarnInputRepaired, "not closed", mediatype, mediatype, mediatype+" body with no closing boundary; the last part runs to its end", false,
			func(count, list string) string {
				return fmt.Sprintf("bodies of %s media types with no closing boundary (%s); the last part of each runs to its end", count, list)
			})
	}
	r.stored(m, NotePreamble, mp.Preamble)
	defaultType := "text/plain"
	if mediatype == "multipart/digest" {
		defaultType = "message/rfc822"
	}
	type sub struct {
		h    rfc822.Header
		body []byte
	}
	subs := make([]sub, 0, len(mp.Parts))
	for _, p := range mp.Parts {
		h, at, prob := rfc822.ParseHeader(p, r.e.maxHeader())
		r.headerProblems(m, prob)
		subs = append(subs, sub{h, p[at:]})
	}
	if mediatype == "multipart/alternative" && len(subs) > 1 {
		plain := -1
		var kinds []string
		for i, s := range subs {
			t := defaultType
			if v, ok := s.h.Get("Content-Type"); ok {
				if mt, _, _ := rfc822.Params(v, nil); strings.Contains(mt, "/") {
					t = mt
				}
			}
			kinds = append(kinds, t)
			d, _ := s.h.Get("Content-Disposition")
			if dt, _, _ := rfc822.Params(d, nil); t == "text/plain" && dt != "attachment" && plain < 0 {
				plain = i
			}
		}
		if !alt {
			list := clean.Cut(clean.Line(strings.Join(kinds, ", ")), 512)
			one := fmt.Sprintf("%d alternative bodies (%s); the blocks are the text/plain one, the others are embedded documents", len(subs), list)
			if plain < 0 {
				one = fmt.Sprintf("%d alternative bodies (%s), none of them text/plain; each is an embedded document", len(subs), list)
			}
			value := fmt.Sprint(plain >= 0, "\x00", strings.Join(kinds, "\x00"))
			m.warnEach(gofastcore.SeverityWarning, WarnAlternatives, "", value, "("+list+")", one, true,
				func(count, list string) string {
					return fmt.Sprintf("%s sets of alternative bodies of different kinds: %s; in each, a text/plain body that is not an attachment gives the blocks, the others are embedded documents", count, list)
				})
		}
		for i, s := range subs {
			r.entity(m, s.h, s.body, depth+1, alt || plain >= 0 && i != plain, defaultType)
		}
	} else {
		for _, s := range subs {
			r.entity(m, s.h, s.body, depth+1, alt, defaultType)
		}
	}
	if mp.More {
		m.warn(gofastcore.SeverityError, gofastcore.WarnLimitExceeded, fmt.Sprintf("the message has more than %d MIME parts; the rest are not read", r.e.maxParts()))
	}
	r.stored(m, NoteEpilogue, mp.Epilogue)
}

// stored adds the text of a preamble or epilogue as a note whose type
// says where it came from. No display reason is set: the format declares
// none.
func (r *reader) stored(m *message, kind string, raw []byte) {
	if len(bytes.TrimSpace(raw)) == 0 {
		return
	}
	text, _ := rfc822.DecodeCharsetDefault("", m.def, raw)
	text = strings.TrimSpace(m.text(text))
	if text == "" {
		return
	}
	if !r.take(m, gofastcore.BlockCost+len(text)) {
		return
	}
	m.part.Blocks = append(m.part.Blocks, gofastcore.Block{Kind: gofastcore.KindNote, Text: text,
		Attrs: map[string]string{gofastcore.AttrNoteType: kind}})
}

// decode undoes a transfer encoding, within the decode budget.
func (r *reader) decode(m *message, cte string, body []byte) ([]byte, bool) {
	var out []byte
	switch cte {
	case "base64":
		out = rfc822.Base64(body)
		if rfc822.Base64Bad(body) {
			m.warn(gofastcore.SeverityInfo, gofastcore.WarnInputRepaired, "base64 with characters outside its alphabet; they are skipped")
		}
	case "quoted-printable":
		out = rfc822.QuotedPrintable(body)
	case "", "7bit", "8bit", "binary":
		out = body
	default:
		v := clean.Cut(clean.Line(cte), 64)
		m.warnEach(gofastcore.SeverityWarning, WarnUnknownTransferEncoding, "", cte, v, fmt.Sprintf("transfer encoding %q; the bytes are taken as they are", v), false,
			func(count, list string) string {
				return fmt.Sprintf("%s transfer encodings (%s); the bytes are taken as they are", count, list)
			})
		out = body
	}
	if m.undecode || !r.dec.Take(int64(len(out))) {
		if !m.undecode {
			m.undecode = true
			m.warn(gofastcore.SeverityError, gofastcore.WarnLimitExceeded, "decode limit reached; the rest of the message's parts are listed, not read")
		}
		return nil, false
	}
	return out, true
}

// child hands a part to the extractor of embedded documents. data nil
// lists it without content.
func (r *reader) child(m *message, name, mime string, data []byte, attachment bool) {
	var src io.ReaderAt
	size := int64(len(data))
	if data != nil && !m.cut {
		src = bytes.NewReader(data)
	}
	if r.ctx.Err() != nil {
		return
	}
	d := r.opts.ExtractChild(r.ctx, name, mime, src, size)
	m.children = append(m.children, len(r.doc.Children))
	r.doc.Children = append(r.doc.Children, d)
	if attachment {
		label := name
		if label == "" {
			label = "(" + mime + ")"
		}
		m.attached = append(m.attached, gofastcore.ExpandLigatures(label))
	}
}
