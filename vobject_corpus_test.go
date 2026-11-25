package gofastemail_test

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/AOShei/go-fast-core"
	"github.com/AOShei/go-fast-email"
	"github.com/AOShei/go-fast-email/internal/vobj"
)

// TestCorpusVobj runs the extractor over the calendars and contact lists
// of the public corpus, dev/ and heldout/ (held-out files are counted,
// never named or looked into). Every file must read without an error, a
// panic or a Validate failure. On every dev file two oracles hold:
//
//   - citation: every word of a block's text is in the bytes at its
//     Loc.Offset/Length, unfolded and unescaped (compared in lower case,
//     without "-" and ":", so that dates and labels of types compare),
//     the labels of the details paragraph aside. Blocks whose bytes are
//     encoded (quoted-printable, base64, a CHARSET or not UTF-8) or hold
//     a presentation-form ligature cannot be checked this way and are
//     counted as unchecked.
//   - nothing dropped: every property of each component is in its
//     first block's attributes, or read into its text (readElsewhere).
//     VTIMEZONE's properties are rules for computing times; only TZID is
//     read, into Metadata.Extra["timezones"].
var beginLine = regexp.MustCompile(`^(?i)(?:[a-z0-9-]+\.)?BEGIN:(VCARD|VCALENDAR|VEVENT|VTODO|VJOURNAL|VFREEBUSY|VAVAILABILITY|VTIMEZONE)[ \t\r\n]`)

func TestCorpusVobj(t *testing.T) {
	root := corpusRoot()
	if _, err := os.Stat(root); err != nil {
		t.Skipf("no corpus at %s", root)
	}
	for _, set := range []string{"dev", "heldout"} {
		for _, format := range []string{"ics", "vcf"} {
			dir := filepath.Join(root, set, format)
			entries, err := os.ReadDir(dir)
			if err != nil {
				continue
			}
			files, errs, invalid, unchecked, other := 0, 0, 0, 0, 0
			kinds, codes := map[string]int{}, map[string]int{}
			for _, ent := range entries {
				if ent.IsDir() {
					continue
				}
				files++
				label := ent.Name()
				if set == "heldout" {
					label = fmt.Sprintf("held-out file %d", files)
				}
				src, doc, err := extractCorpusFile(filepath.Join(dir, ent.Name()))
				if err != nil {
					errs++
					t.Errorf("%s: %v", label, err)
					continue
				}
				if err := doc.Validate(); err != nil {
					invalid++
					if set == "dev" {
						t.Errorf("%s: Validate: %v", label, err)
					} else {
						t.Errorf("%s: Validate failed", label)
					}
				}
				// The format is decided from the bytes: a BEGIN line of a
				// card, a calendar or a component a calendar holds (a
				// lone VEVENT from a library's tests), perhaps with a
				// group before it, makes a calendar or a contact list;
				// anything else is a message, and a vCard named .ics is a
				// vCard.
				want := "eml"
				head := strings.TrimLeft(strings.TrimPrefix(string(src[:min(len(src), 64)]), "\xEF\xBB\xBF"), " \t\r\n")
				if m := beginLine.FindStringSubmatch(head); m != nil {
					want = "ics"
					if strings.EqualFold(m[1], "vcard") {
						want = "vcf"
					}
				}
				if doc.Source.Format != want {
					t.Errorf("%s: read as %s", label, doc.Source.Format)
				}
				if want == "eml" {
					other++
				}
				for _, w := range doc.AllWarnings() {
					codes[w.Code]++
				}
				for _, b := range doc.AllBlocks() {
					kinds[b.Kind.String()]++
				}
				if set == "dev" && want != "eml" {
					unchecked += citations(t, label, src, doc)
					nothingDropped(t, label, src, doc)
				}
			}
			t.Logf("%s/%s: files=%d read_as_eml=%d errors=%d invalid=%d unchecked_blocks=%d\n  blocks:%s\n  warnings:%s",
				set, format, files, other, errs, invalid, unchecked, sorted(kinds), sorted(codes))
		}
	}
}

// extractCorpusFile reads one file within a time limit, with moderate
// limits. A panic is an error.
func extractCorpusFile(path string) (src []byte, doc *gofastcore.Document, err error) {
	src, err = os.ReadFile(path)
	if err != nil {
		return nil, nil, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	type result struct {
		doc *gofastcore.Document
		err error
	}
	done := make(chan result, 1)
	go func() {
		defer func() {
			if p := recover(); p != nil {
				done <- result{err: fmt.Errorf("panic: %v", p)}
			}
		}()
		var got []seen
		opts := &gofastcore.Options{Child: dispatcher(&got), Limits: gofastcore.Limits{MaxTextBytes: 64 << 20, MaxDecodedBytes: 256 << 20}}
		d, err := gofastemail.Extract(ctx, bytes.NewReader(src), int64(len(src)), opts)
		done <- result{d, err}
	}()
	res := <-done
	return src, res.doc, res.err
}

// detailLabels are the labels of the lines of a details paragraph.
var detailLabels = []string{"Start", "End", "Due", "Duration", "Completed", "Repeats", "Location", "Organizer", "Attendees",
	"Status", "Categories", "URL", "Free/busy", "Alarm", "Attachments", "Organization", "Title", "Role", "Nickname", "Email",
	"Phone", "Address", "Birthday", "Anniversary", "Gender", "IMPP"}

func normalize(s string) string {
	for _, fold := range []string{"\r\n ", "\r\n\t", "\n ", "\n\t", "\r ", "\r\t"} {
		s = strings.ReplaceAll(s, fold, "")
	}
	s = strings.ToLower(vobj.Unescape(s))
	return strings.NewReplacer("-", "", ":", "").Replace(s)
}

// citations checks the citation oracle on a document and returns the
// number of blocks it could not check.
func citations(t *testing.T, label string, src []byte, doc *gofastcore.Document) int {
	unchecked := 0
	for _, b := range doc.Parts[0].Blocks {
		if b.Kind == gofastcore.KindImage {
			continue
		}
		if b.Loc.Length == 0 {
			t.Errorf("%s: %s block with no place: %q", label, b.Kind, b.Text)
			continue
		}
		raw := string(src[b.Loc.Offset : b.Loc.Offset+b.Loc.Length])
		up := strings.ToUpper(raw)
		if strings.Contains(up, "QUOTED-PRINTABLE") || strings.Contains(up, "ENCODING=B") || strings.Contains(up, "CHARSET=") ||
			!utf8.ValidString(raw) || strings.ContainsFunc(raw, func(r rune) bool { return r >= 0xFB00 && r <= 0xFB06 }) {
			unchecked++
			continue
		}
		// RRULE is written as it is, not unescaped: both forms count.
		span := normalize(raw) + "\n" + strings.ToLower(strings.NewReplacer("\r\n ", "", "\n ", "", "-", "", ":", "").Replace(raw))
		text := b.Text
		if b.Kind == gofastcore.KindParagraph && b.Attrs[gofastemail.AttrComponent] != "" || strings.Contains(text, ": ") {
			var lines []string
			for _, l := range strings.Split(text, "\n") {
				if name, rest, ok := strings.Cut(l, ": "); ok && contains(detailLabels, name) {
					l = rest
				}
				lines = append(lines, l)
			}
			text = strings.Join(lines, "\n")
		}
		for _, w := range strings.FieldsFunc(strings.ToLower(text), func(r rune) bool {
			return unicode.IsSpace(r) || strings.ContainsRune(",;()<>\"", r)
		}) {
			w = strings.NewReplacer("-", "", ":", "").Replace(w)
			if w != "" && !strings.Contains(span, w) {
				t.Errorf("%s: %s block: %q is not in its bytes %q", label, b.Kind, w, clip(raw))
				break
			}
		}
	}
	return unchecked
}

func contains(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}

func clip(s string) string {
	if len(s) > 200 {
		return s[:200] + "..."
	}
	return s
}

// readElsewhere says, for the properties read into a component's text,
// what in its blocks shows that they were.
var readElsewhere = map[string]string{
	"SUMMARY": "heading", "FN": "heading", "N": "heading",
	"DTSTART": "Start: ", "DTEND": "End: ", "DUE": "Due: ", "DURATION": "Duration: ", "COMPLETED": "Completed: ",
	"RRULE": "Repeats: ", "LOCATION": "Location: ", "ORGANIZER": "Organizer: ", "ATTENDEE": "Attendees: ",
	"STATUS": "Status: ", "CATEGORIES": "Categories: ", "URL": "URL: ", "FREEBUSY": "Free/busy: ",
	"ORG": "Organization: ", "TITLE": "Title: ", "ROLE": "Role: ", "NICKNAME": "Nickname: ", "EMAIL": "Email: ",
	"TEL": "Phone: ", "ADR": "Address: ", "BDAY": "Birthday: ", "ANNIVERSARY": "Anniversary: ", "GENDER": "Gender: ",
	"IMPP": "IMPP: ", "DESCRIPTION": "paragraph", "COMMENT": "paragraph", "NOTE": "paragraph",
	"PHOTO": "image", "LOGO": "image", "X-ALT-DESC": "children", "ATTACH": "children", "X-ABLABEL": "(",
}

// component is a component of a file as the oracle reads it.
type component struct {
	name           string
	props          []*vobj.Property
	subs           []*component
	offset, length int
}

// nothingDropped checks that every property of each component of the
// file is in the output.
func nothingDropped(t *testing.T, label string, src []byte, doc *gofastcore.Document) {
	// The components that stand for themselves, in the order of their
	// blocks: each, then those nested in it other than alarms.
	var items []*component
	var stack []*component
	var add func(c *component)
	add = func(c *component) {
		items = append(items, c)
		for _, s := range c.subs {
			if s.name != "VALARM" {
				add(s)
			}
		}
	}
	p := vobj.NewParser(src)
	for {
		e, ok := p.Next()
		if !ok {
			break
		}
		switch e.Kind {
		case vobj.Begin:
			stack = append(stack, &component{name: e.Name, offset: e.Offset})
		case vobj.Prop:
			if len(stack) > 0 {
				stack[len(stack)-1].props = append(stack[len(stack)-1].props, e.Prop)
			}
		case vobj.End:
			c := stack[len(stack)-1]
			stack = stack[:len(stack)-1]
			c.length = e.Length
			switch {
			case c.name == "VCALENDAR" || c.name == "VTIMEZONE":
			case len(stack) == 0 || stack[len(stack)-1].name == "VCALENDAR":
				add(c)
			default:
				stack[len(stack)-1].subs = append(stack[len(stack)-1].subs, c)
			}
		}
	}
	blocks := doc.Parts[0].Blocks
	bi := 0
	for _, c := range items {
		if bi >= len(blocks) || blocks[bi].Attrs[gofastemail.AttrComponent] != strings.ToLower(c.name) ||
			blocks[bi].Loc.Offset < int64(c.offset) || blocks[bi].Loc.Offset > int64(c.offset+c.length) {
			continue // a component with no text: counted in a warning
		}
		first := blocks[bi]
		mine := []gofastcore.Block{first}
		for bi++; bi < len(blocks) && blocks[bi].Attrs[gofastemail.AttrComponent] == ""; bi++ {
			mine = append(mine, blocks[bi])
		}
		has := func(what string) bool {
			for _, b := range mine {
				switch {
				case what == "heading" || what == "paragraph" || what == "image":
					if b.Kind.String() == what {
						return true
					}
				case what == "children":
					if b.Attrs[gofastemail.AttrChildren] != "" {
						return true
					}
				case strings.Contains(b.Text, what):
					return true
				}
			}
			return false
		}
		check := func(prefix string, p *vobj.Property) {
			key := strings.ToLower(p.Name)
			if key == "" {
				key = "unnamed"
			}
			if _, ok := first.Attrs[prefix+key]; ok {
				return
			}
			if prefix == "" {
				if what, ok := readElsewhere[p.Name]; ok && has(what) {
					return
				}
			} else if (p.Name == "TRIGGER" || p.Name == "DESCRIPTION" || p.Name == "SUMMARY") && has("Alarm: ") {
				return
			}
			t.Errorf("%s: %s: property %s at %d is not in the output", label, c.name, p.Name, p.Offset)
		}
		for _, p := range c.props {
			check("", p)
		}
		for _, s := range c.subs {
			if s.name == "VALARM" {
				for _, p := range s.props {
					check("alarm.", p)
				}
			}
		}
	}
	if bi != len(blocks) {
		t.Errorf("%s: %d blocks not matched to a component", label, len(blocks)-bi)
	}
}
