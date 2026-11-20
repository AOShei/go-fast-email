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
	"unicode"

	"github.com/AOShei/go-fast-core"
	"github.com/AOShei/go-fast-email/internal/charset"
	"github.com/AOShei/go-fast-email/internal/clean"
	"github.com/AOShei/go-fast-email/internal/rfc822"
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

// Values of Attrs["role"] and of the note type this library uses.
const (
	RoleHeaders   = "headers"
	RoleSignature = "signature" // the value of textbody.RoleSignature
	// NoteHeaders: a note block that holds the headers a mail program does
	// not show, one "Name: value" per line; flagged invisible.
	NoteHeaders = "headers"
	// NotePreamble and NoteEpilogue: the text of a multipart body before
	// its first part and after its last, which no mail program shows;
	// flagged invisible.
	NotePreamble = "mime_preamble"
	NoteEpilogue = "mime_epilogue"
)

// maxNameBytes bounds a part name and an attachment name.
const maxNameBytes = 256

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
	part     gofastcore.Part
	body     []gofastcore.Block
	cut      bool // the text budget is spent
	undecode bool // the decode budget is spent
	parts    int  // MIME parts seen
	children []int
	attached []string
	warned   map[string]bool
	inline   bool // a single message: its headers go to the metadata
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

func (m *message) charsetReport() rfc822.CharsetFunc {
	return func(label string, res charset.Result) {
		switch {
		case label == "" && res.Replaced:
			m.warn(gofastcore.SeverityWarning, gofastcore.WarnTextUnmappedGlyphs, "bytes that are not UTF-8 in text that declares no character set")
		case !res.Known:
			m.warn(gofastcore.SeverityWarning, gofastcore.WarnTextUnmappedGlyphs, fmt.Sprintf("character set %q is not decoded; bytes outside ASCII are U+FFFD", clean.Cut(clean.Line(label), 64)))
		case res.Replaced:
			m.warn(gofastcore.SeverityWarning, gofastcore.WarnTextUnmappedGlyphs, fmt.Sprintf("bytes not valid in character set %q", clean.Cut(clean.Line(label), 64)))
		}
	}
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
	raw := make([]byte, n)
	if got, err := src.ReadAt(raw, 0); int64(got) < n {
		return nil, fmt.Errorf("gofastemail: reading: %v: %w", err, gofastcore.ErrMalformed)
	}
	raw = bytes.TrimPrefix(raw, []byte("\xEF\xBB\xBF"))
	if !looksLikeMessage(raw) {
		return nil, fmt.Errorf("gofastemail: not an email message: %w", gofastcore.ErrMalformed)
	}
	r.doc = gofastcore.New("eml")
	r.doc.Source = gofastcore.Source{Format: "eml", Name: r.opts.Name, MIME: "message/rfc822", Size: size}
	m := &message{inline: true}
	if size > n {
		m.warn(gofastcore.SeverityError, gofastcore.WarnLimitExceeded, fmt.Sprintf("message of %d bytes read to %d", size, n))
	}
	r.message(m, raw, 1, "")
	if err := r.ctx.Err(); err != nil {
		return nil, err
	}
	r.doc.Parts = append(r.doc.Parts, m.part)
	return r.finish(), nil
}

// finish sets what is set once the parts are read.
func (r *reader) finish() *gofastcore.Document {
	r.doc.HiddenChecked = gofastcore.HiddenChars | gofastcore.HiddenInvisible
	if hiddenInMetadata(&r.doc.Metadata) {
		r.doc.Warn(gofastcore.SeverityWarning, gofastcore.WarnTextHidden, "metadata contains invisible characters")
	}
	return r.doc
}

// looksLikeMessage reports whether b begins with a header field.
func looksLikeMessage(b []byte) bool {
	h, _, p := rfc822.ParseHeader(b, 64<<10)
	return len(h) > 0 && !(p.Malformed && len(h) == 0)
}

// message reads one message into m.part. envelope is the "From " line of
// a mailbox, without "From ".
func (r *reader) message(m *message, raw []byte, number int, envelope string) {
	m.part.Kind, m.part.Number = gofastcore.PartMessage, number
	h, bodyAt, prob := rfc822.ParseHeader(raw, r.e.maxHeader())
	r.headerProblems(m, prob)
	report := m.charsetReport()

	// The header fields as text, in order.
	type field struct{ key, name, value string }
	fields := make([]field, 0, len(h))
	for _, f := range h {
		v := clean.Line(rfc822.DecodeText(f.Value, report))
		fields = append(fields, field{strings.ToLower(f.Name), clean.Line(f.Name), v})
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
	var unshown []headerField
	if envelope != "" {
		unshown = append(unshown, headerField{"From ", "From " + envelope, envelope})
	}
	for _, f := range fields {
		if !isShown(f.key) {
			unshown = append(unshown, headerField{f.name, f.name + ": " + f.value, f.value})
		}
	}
	// In a mailbox the metadata is the file's, so the headers that are
	// not shown are a note of the message's part.
	r.unshownHeaders(m, unshown, !m.inline)

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

// machineHeaders are headers that servers, filters and mail programs
// write, long by nature, whose values are signatures, encoded reports and
// tokens, lists of message ids, keys, or trace data: on ordinary delivered
// mail they exceed core's character limit. Their machine tokens are left
// out before core's outlier rule is applied (see machineTokensOut), and
// each comment in parentheses is an item of its own, so prose in them
// still counts. Names are lowercase; machineHeaderPrefixes are prefixes,
// each the namespace of one vendor's filter or service.
var machineHeaders = map[string]bool{
	"received": true, "x-received": true, "received-spf": true,
	"dkim-signature": true, "domainkey-signature": true,
	"arc-seal": true, "arc-message-signature": true, "arc-authentication-results": true,
	"authentication-results": true, "authentication-results-original": true,
	"references": true, "in-reply-to": true, "autocrypt": true, "list-unsubscribe": true,
	"msip_labels": true, "thread-index": true,
	"x-instance-identity": true, "x-asf-dkim-sig": true, "x-authority-analysis": true,
	"x-sg-eid": true, "x-sg-id": true, "x-cmae-envelope": true,
}

var machineHeaderPrefixes = []string{
	"x-ms-", "x-microsoft-", "x-forefront-", "x-exchange-", // Microsoft Exchange and Outlook.com
	"x-gm-", "x-google-", // Gmail
	"x-spam-",                                                     // SpamAssassin, amavis and other filters
	"x-proofpoint-", "x-ironport-", "x-barracuda-", "x-mimecast-", // filtering services
	"x-ymail-", "x-yahoo-", "x-qq-", // Yahoo, QQ
}

func isMachineHeader(name string) bool {
	name = strings.ToLower(name)
	if machineHeaders[name] {
		return true
	}
	for _, p := range machineHeaderPrefixes {
		if strings.HasPrefix(name, p) {
			return true
		}
	}
	return false
}

// machineTokensOut returns s without its machine tokens: a token (with
// punctuation trimmed from its ends) that is empty, longer than 40 bytes,
// or holds "=", "@", ";", ":" or "/" (a rule of dashes, a signature, an
// encoded blob, a key=value pair, an address, a URL). What is left is
// prose, to which core's rule applies in full.
func machineTokensOut(s string) string {
	return tokensOut(s, func(t string) bool { return len(t) > 40 || strings.ContainsAny(t, "=@;:/") })
}

// linkTokensOut returns s without the tokens that are links, addresses or
// punctuation alone (the rules and URLs of a mailing list's footer). An
// encoded payload stays, so that it counts by its length.
func linkTokensOut(s string) string {
	return tokensOut(s, func(t string) bool { return strings.Contains(t, "://") || strings.Contains(t, "@") })
}

func tokensOut(s string, drop func(string) bool) string {
	var keep []string
	for tok := range strings.FieldsSeq(s) {
		t := strings.TrimFunc(tok, func(r rune) bool { return unicode.IsPunct(r) || unicode.IsSymbol(r) })
		if t == "" || drop(t) {
			continue
		}
		keep = append(keep, tok)
	}
	return strings.Join(keep, " ")
}

// machineItems splits the value of a machine header into the items core's
// rule is applied to: each comment in parentheses (RFC 5322 CFWS; a
// Received header's TLS details, an SPF explanation) and the rest, each
// with its machine tokens left out. Unbalanced parentheses end the
// comment at the end of the value.
func machineItems(v string) []string {
	var items []string
	var rest, comment strings.Builder
	depth := 0
	for _, c := range v {
		switch {
		case c == '(':
			if depth > 0 {
				comment.WriteRune(' ')
			}
			depth++
		case c == ')' && depth > 0:
			depth--
			if depth == 0 {
				items = append(items, machineTokensOut(comment.String()))
				comment.Reset()
				rest.WriteRune(' ')
			} else {
				comment.WriteRune(' ')
			}
		case depth > 0:
			comment.WriteRune(c)
		default:
			rest.WriteRune(c)
		}
	}
	if comment.Len() > 0 {
		items = append(items, machineTokensOut(comment.String()))
	}
	return append(items, machineTokensOut(rest.String()))
}

// headerOutlier applies core's rule to a header a mail program does not
// show.
func headerOutlier(name, value string) bool {
	if !isMachineHeader(name) {
		return gofastcore.UnshownOutlier(value)
	}
	for _, item := range machineItems(value) {
		if gofastcore.UnshownOutlier(item) {
			return true
		}
	}
	return false
}

// unshownHeaders adds the headers a mail program does not show. Those are
// routine (every message has them) and are not flagged: with all set (a
// mailbox, whose metadata is the file's) they are a note. A header whose
// value is out of proportion by core's rule (gofastcore.UnshownOutlier;
// for a machine header, with its machine tokens left out) is a note of
// its own in any case, flagged invisible, with a warning.
func (r *reader) unshownHeaders(m *message, fields []headerField, all bool) {
	var routine []string
	for _, f := range fields {
		if headerOutlier(f.name, f.value) {
			r.outlier(m, &m.part.Blocks, NoteHeaders, strings.TrimSpace(f.line), fmt.Sprintf("the header %q, which a mail program does not show, holds more than %d words or %d characters",
				clean.Cut(f.name, 64), gofastcore.UnshownOutlierWords, gofastcore.UnshownOutlierChars))
		} else if all {
			routine = append(routine, f.line)
		}
	}
	if t := strings.TrimSpace(strings.Join(routine, "\n")); t != "" && r.take(m, gofastcore.BlockCost+len(t)) {
		m.part.Blocks = append(m.part.Blocks, gofastcore.Block{Kind: gofastcore.KindNote, Text: t,
			Attrs: map[string]string{gofastcore.AttrNoteType: NoteHeaders}})
	}
}

// outlier adds a note of unshown data that is out of proportion for its
// kind, flagged invisible, and the warning that says so.
func (r *reader) outlier(m *message, to *[]gofastcore.Block, kind, text, what string) {
	m.warn(gofastcore.SeverityWarning, gofastcore.WarnTextUnshownOutlier, what)
	if text == "" || !r.take(m, gofastcore.BlockCost+len(text)) {
		return
	}
	*to = append(*to, gofastcore.Block{Kind: gofastcore.KindNote, Text: text, Hidden: gofastcore.HiddenInvisible,
		Attrs: map[string]string{gofastcore.AttrNoteType: kind}})
}

// head adds the heading that holds the subject and the block of the
// headers a mail program shows, and returns the index of that block, or
// -1 if there is none.
func (r *reader) head(m *message, subject string, lines []string) int {
	if subject != "" {
		m.part.Name = gofastcore.ExpandLigatures(clean.Cut(subject, maxNameBytes))
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

// tail adds the body's blocks after the headers, the names of the
// attachments to the headers block, and finishes the part.
func (r *reader) tail(m *message, headersAt int) {
	m.part.Blocks = append(m.part.Blocks, m.body...)
	if headersAt >= 0 && (len(m.attached) > 0 || len(m.children) > 0) {
		b := &m.part.Blocks[headersAt]
		if len(m.attached) > 0 {
			extra := "\nAttachments: " + strings.Join(m.attached, ", ")
			if !m.cut && r.text.Take(len(extra)) {
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
	m.part.MarkHidden()
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
	if p.Truncated {
		m.warn(gofastcore.SeverityError, gofastcore.WarnLimitExceeded, fmt.Sprintf("a header section is longer than %d bytes; the rest of it is not read", r.e.maxHeader()))
	}
	if p.TooMany {
		m.warn(gofastcore.SeverityError, gofastcore.WarnLimitExceeded, fmt.Sprintf("a header section has more than %d fields; the rest are not read", rfc822.MaxFields))
	}
	if p.Malformed {
		m.warn(gofastcore.SeverityInfo, gofastcore.WarnInputRepaired, "a header section ends at a line that is not a header field; the body is taken to begin there")
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
	report := m.charsetReport()
	mediatype, params := defaultType, map[string]string{}
	if v, ok := h.Get("Content-Type"); ok {
		if t, p := rfc822.Params(v, report); strings.Contains(t, "/") {
			mediatype, params = t, p
		} else {
			params = p
		}
	}
	var disposition string
	dparams := map[string]string{}
	if v, ok := h.Get("Content-Disposition"); ok {
		disposition, dparams = rfc822.Params(v, report)
	}
	name := dparams["filename"]
	if name == "" {
		name = params["name"]
	}
	name = clean.Cut(clean.Line(name), maxNameBytes)
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
			m.warn(gofastcore.SeverityWarning, gofastcore.WarnInputRepaired, mediatype+" body with no boundary; read as text")
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
		m.warn(gofastcore.SeverityWarning, gofastcore.WarnInputExternalReference, fmt.Sprintf("a part whose content is outside the message (access type %q): %q",
			clean.Cut(clean.Line(params["access-type"]), 64), clean.Cut(clean.Line(where), 1024)))
	}
	data, _ := r.decode(m, cte, body) // nil when over the limit
	if strings.HasPrefix(mediatype, "image/") && !attachment && !alt {
		// A picture a mail program shows in the message: a block at its
		// place, and an embedded document as well.
		var desc string
		if v, ok := h.Get("Content-Description"); ok {
			desc = rfc822.DecodeText(v, report)
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
	text := gofastcore.ExpandLigatures(clean.Cut(clean.Line(desc), 4096))
	if !r.take(m, gofastcore.BlockCost+len(text)) {
		return
	}
	img := &gofastcore.Image{MIME: mediatype}
	if len(data) > 0 {
		sum := sha256.Sum256(data)
		img.ID = "sha256-" + hex.EncodeToString(sum[:12])
		img.Width, img.Height = imageSize(mediatype, data)
		if r.opts.Images == gofastcore.ImagesData {
			img.Data = data
		}
	}
	b := gofastcore.Block{Kind: gofastcore.KindImage, Text: text, Image: img}
	if name != "" {
		b.Attrs = map[string]string{gofastcore.AttrName: name}
	}
	m.body = append(m.body, b)
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
		r.child(m, name, withCharset(mediatype, params), data, false)
	case mediatype == "text/plain" && alt:
		r.child(m, name, withCharset(mediatype, params), data, false)
	case isTNEF(mediatype) || strings.EqualFold(name, "winmail.dat"):
		r.tnefChild(m, name, mediatype, data, true)
	case strings.HasPrefix(mediatype, "message/"):
		r.child(m, name, mediatype, data, true)
	case strings.HasPrefix(mediatype, "text/"):
		r.child(m, name, withCharset(mediatype, params), data, attachment || name != "")
	default:
		r.child(m, name, mediatype, data, true)
	}
}

// withCharset returns the media type with its charset parameter, which the
// embedded document's extractor needs to read text.
func withCharset(mediatype string, params map[string]string) string {
	if cs := charset.Normalize(params["charset"]); cs != "" && rfc822.Token(cs) && len(cs) < 64 {
		return mediatype + "; charset=" + cs
	}
	return mediatype
}

// textBody makes blocks of a text/plain body.
func (r *reader) textBody(m *message, params map[string]string, data []byte) {
	if data == nil {
		return
	}
	label := params["charset"]
	text, res := charset.Decode(label, data)
	if !res.Known || res.Replaced {
		if label == "" {
			label = "us-ascii"
		}
		m.charsetReport()(charset.Normalize(label), res)
	}
	text = clean.Text(text)
	o := textbody.Options{
		Flowed: strings.EqualFold(params["format"], "flowed"),
		DelSp:  strings.EqualFold(params["delsp"], "yes"),
	}
	blocks, _ := textbody.Blocks(text, o, func(n int) bool { return r.take(m, n) })
	m.body = append(m.body, blocks...)
}

// multipart reads a multipart body.
func (r *reader) multipart(m *message, mediatype, boundary string, body []byte, depth int, alt bool) {
	left := max(r.e.maxParts()-m.parts, 0)
	mp := rfc822.Split(body, boundary, left+1)
	if len(mp.Parts) == 0 {
		m.warn(gofastcore.SeverityWarning, gofastcore.WarnInputRepaired, mediatype+" body in which the boundary is never found; read as text")
		r.leaf(m, "text/plain", nil, "", false, alt, body)
		return
	}
	if !mp.Closed {
		m.warn(gofastcore.SeverityInfo, gofastcore.WarnInputRepaired, mediatype+" body with no closing boundary; the last part runs to its end")
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
				if mt, _ := rfc822.Params(v, nil); strings.Contains(mt, "/") {
					t = mt
				}
			}
			kinds = append(kinds, t)
			d, _ := s.h.Get("Content-Disposition")
			if dt, _ := rfc822.Params(d, nil); t == "text/plain" && dt != "attachment" && plain < 0 {
				plain = i
			}
		}
		if plain >= 0 && !alt {
			m.warn(gofastcore.SeverityWarning, WarnAlternatives, fmt.Sprintf("%d alternative bodies (%s); the blocks are the text/plain one, the others are embedded documents", len(subs), clean.Cut(clean.Line(strings.Join(kinds, ", ")), 512)))
		} else if !alt {
			m.warn(gofastcore.SeverityWarning, WarnAlternatives, fmt.Sprintf("%d alternative bodies (%s), none of them text/plain; each is an embedded document", len(subs), clean.Cut(clean.Line(strings.Join(kinds, ", ")), 512)))
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

// stored adds the text of a preamble or epilogue, which no mail program
// shows, as a note, flagged invisible only when it is an outlier.
func (r *reader) stored(m *message, kind string, raw []byte) {
	if len(bytes.TrimSpace(raw)) == 0 {
		return
	}
	text, _ := charset.Decode("", raw)
	text = strings.TrimSpace(clean.Text(text))
	if text == "" {
		return
	}
	// "This is a multi-part message in MIME format." is the usual
	// preamble: routine, not flagged; so is the footer a mailing list
	// appends after the closing boundary, once its rules, links and
	// addresses are left out. A long one is out of proportion.
	if gofastcore.UnshownOutlier(linkTokensOut(text)) {
		r.outlier(m, &m.body, kind, text, fmt.Sprintf("the %s of a multipart body, which no mail program shows, holds more than %d words or %d characters",
			strings.TrimPrefix(kind, "mime_"), gofastcore.UnshownOutlierWords, gofastcore.UnshownOutlierChars))
		return
	}
	if !r.take(m, gofastcore.BlockCost+len(text)) {
		return
	}
	m.body = append(m.body, gofastcore.Block{Kind: gofastcore.KindNote, Text: text,
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
		m.warn(gofastcore.SeverityWarning, WarnUnknownTransferEncoding, fmt.Sprintf("transfer encoding %q; the bytes are taken as they are", clean.Cut(clean.Line(cte), 64)))
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
