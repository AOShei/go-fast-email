package gofastemail_test

// Findings of the adversarial review of the calendar and contact readers.
// Each test fails today and passes once its defect is fixed.

import (
	"context"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/AOShei/go-fast-core"
	"github.com/AOShei/go-fast-email"
)

// allText gathers the text and attribute values of every block of a
// document, and its metadata, to check that a value is somewhere.
func allText(doc *gofastcore.Document) string {
	var b strings.Builder
	b.WriteString(doc.Metadata.Title + "\n" + doc.Metadata.Application + "\n")
	for _, v := range doc.Metadata.Extra {
		b.WriteString(v + "\n")
	}
	for _, p := range doc.Parts {
		for _, bl := range p.Blocks {
			b.WriteString(bl.Text + "\n")
			for _, v := range bl.Attrs {
				b.WriteString(v + "\n")
			}
		}
	}
	return b.String()
}

// Found by FuzzExtract (60 s): a component name with a control
// character goes uncleaned into the email.component_without_text
// message (vobject.go, finishItem: v.kinds gets strings.ToLower(c.name))
// and Validate fails ("warnings[...].message contains control character
// U+000B"). It also goes uncleaned into Attrs["component"].
func TestReviewVobjControlCharacterInComponentName(t *testing.T) {
	for _, src := range []string{
		"BEGIN:VCALENDAR\r\nBEGIN:X\x0bY\r\nUID:u\r\nEND:X\x0bY\r\nEND:VCALENDAR\r\n",
		"BEGIN:VCALENDAR\r\nBEGIN:X\x0bY\r\nSUMMARY:s\r\nEND:X\x0bY\r\nEND:VCALENDAR\r\n",
	} {
		doc, err := gofastemail.Extract(context.Background(), strings.NewReader(src), int64(len(src)), nil)
		if err != nil {
			t.Fatal(err)
		}
		if err := doc.Validate(); err != nil {
			t.Errorf("%q: Validate: %v", src, err)
		}
		for _, b := range doc.Parts[0].Blocks {
			if c := b.Attrs[gofastemail.AttrComponent]; strings.ContainsRune(c, '\x0b') {
				t.Errorf("%q: Attrs[component] %q", src, c)
			}
		}
	}
}

// A calendar nested inside an event resets the count of items the event
// holds (vobject.go, event: v.held = 0 when an item inside the nested
// VCALENDAR ends), so MaxComponentItems no longer bounds what the outer
// event holds in memory. The outer event below holds 120,000 properties
// and no limit.exceeded is raised.
func TestReviewVobjItemBoundResetByNestedCalendar(t *testing.T) {
	var b strings.Builder
	b.WriteString("BEGIN:VCALENDAR\nBEGIN:VEVENT\nSUMMARY:outer\n")
	half := gofastemail.MaxComponentItems*6/10 + 1
	for i := 0; i < half; i++ {
		b.WriteString("X:1\n")
	}
	b.WriteString("BEGIN:VCALENDAR\nBEGIN:VEVENT\nSUMMARY:inner\nEND:VEVENT\nEND:VCALENDAR\n")
	for i := 0; i < half; i++ {
		b.WriteString("X:1\n")
	}
	b.WriteString("END:VEVENT\nEND:VCALENDAR\n")
	doc := extract(t, b.String(), nil)
	if !hasWarning(doc, gofastcore.WarnLimitExceeded) {
		t.Errorf("an event holding %d properties gives no limit.exceeded: %q", 2*half, wlist(doc.Parts[0].Warnings))
	}
}

// An escaped comma inside a component of N is content, not a separator
// (RFC 6350, 3.4 and 6.2.2): the heading splits the unescaped component
// at commas (vobject.go, card: strings.Split(n[k], ",")), so "O\,Brien"
// becomes "O Brien".
func TestReviewVobjEscapedCommaInName(t *testing.T) {
	src := "BEGIN:VCARD\r\nVERSION:3.0\r\nN:O\\,Brien;Pat;;;\r\nEND:VCARD\r\n"
	doc := extract(t, src, nil)
	got := texts(&doc.Parts[0])
	if len(got) == 0 || got[0] != "heading: Pat O,Brien" {
		t.Errorf("blocks %q, want the heading \"Pat O,Brien\"", got)
	}
}

// An empty first SUMMARY stops the search for the heading (vobject.go,
// event1: break after the first SUMMARY whatever its text), so a second,
// non-empty SUMMARY is neither the heading nor anywhere else: the event is
// counted as having no text and "Real" is dropped.
func TestReviewVobjEmptyFirstSummaryDropsSecond(t *testing.T) {
	src := "BEGIN:VCALENDAR\r\nBEGIN:VEVENT\r\nUID:u1\r\nSUMMARY:\r\nSUMMARY:Real\r\nEND:VEVENT\r\nEND:VCALENDAR\r\n"
	doc := extract(t, src, nil)
	if !strings.Contains(allText(doc), "Real") {
		t.Errorf("the second SUMMARY is not in the output: %q, %q", dump(src, &doc.Parts[0]), wlist(doc.Parts[0].Warnings))
	}
}

// An event whose only text is its HTML description (Outlook's
// X-ALT-DESC) gives the embedded document, but no block: the child is
// linked from no block's Attrs["children"], its UID and other
// properties are dropped, and the event is reported as having "no text"
// (vobject.go, finishItem with no block from the component).
func TestReviewVobjAltDescOnlyEventLosesLink(t *testing.T) {
	src := "BEGIN:VCALENDAR\r\nBEGIN:VEVENT\r\nUID:only-html\r\nDTSTAMP:20261120T090000Z\r\n" +
		"X-ALT-DESC;FMTTYPE=text/html:<p>Agenda</p>\r\nEND:VEVENT\r\nEND:VCALENDAR\r\n"
	var got []seen
	doc := extract(t, src, &gofastcore.Options{Child: dispatcher(&got)})
	if len(doc.Children) != 1 {
		t.Fatalf("%d children", len(doc.Children))
	}
	linked := false
	for _, b := range doc.Parts[0].Blocks {
		if b.Attrs[gofastemail.AttrChildren] == "0" && b.Attrs["uid"] == "only-html" {
			linked = true
		}
	}
	if !linked {
		t.Errorf("no block links the HTML description or keeps the UID: %q, %q", dump(src, &doc.Parts[0]), wlist(doc.Parts[0].Warnings))
	}
}

// A quoted-printable value whose last line ends with "=" (a soft break
// with nothing after it, which phone exporters write) swallows the END
// line after it (internal/vobj readLine continues a soft break "whatever
// the next line begins with"). In an address book where every card has
// such a NOTE, each card nests in the one before it; past 32 the cards
// are skipped by the depth bound. All 40 people should be read.
func TestReviewVobjQuotedPrintableSwallowsEnd(t *testing.T) {
	var b strings.Builder
	for i := 0; i < 40; i++ {
		b.WriteString("BEGIN:VCARD\r\nVERSION:2.1\r\nFN:Person\r\nNOTE;ENCODING=QUOTED-PRINTABLE:caf=C3=A9=\r\nEND:VCARD\r\n")
	}
	doc := extract(t, b.String(), nil)
	headings := 0
	for _, bl := range doc.Parts[0].Blocks {
		if bl.Kind == gofastcore.KindHeading {
			headings++
		}
	}
	if headings != 40 || hasWarning(doc, gofastcore.WarnLimitExceeded) {
		t.Errorf("%d of 40 cards read; warnings %q", headings, wlist(doc.Parts[0].Warnings))
	}
}

// Parameters that carry the author's text are dropped (vobject.go: "The
// parameters of a property are not kept"): Apple's structured location
// keeps the address only in X-ADDRESS and X-TITLE (the value is a geo:
// URI), and a vCard 4 ADR keeps its formatted label in LABEL. Neither
// text is anywhere in the output.
func TestReviewVobjParameterTextDropped(t *testing.T) {
	src := "BEGIN:VCALENDAR\r\nBEGIN:VEVENT\r\nUID:u1\r\nSUMMARY:Sync\r\n" +
		"X-APPLE-STRUCTURED-LOCATION;VALUE=URI;X-ADDRESS=\"Roadstar 16\\nHappyville\";X-TITLE=Roadstar:geo:52.38,7.52\r\n" +
		"END:VEVENT\r\nEND:VCALENDAR\r\n"
	doc := extract(t, src, nil)
	if all := allText(doc); !strings.Contains(all, "Happyville") {
		t.Errorf("X-ADDRESS is not in the output: %q", dump(src, &doc.Parts[0]))
	}
	card := "BEGIN:VCARD\r\nVERSION:4.0\r\nFN:A\r\nADR;LABEL=\"Suite 4, 1 Main St\\nTown\":;;1 Main St;Town;;;\r\nEND:VCARD\r\n"
	doc = extract(t, card, nil)
	if all := allText(doc); !strings.Contains(all, "Suite 4") {
		t.Errorf("ADR's LABEL is not in the output: %q", dump(card, &doc.Parts[0]))
	}
}

// The attributes of a property share their keys with the library's own:
// a property named COMPONENT is overwritten by Attrs["component"] and
// lost, and one named CHILDREN stays as Attrs["children"], an index into
// Document.Children of a child that does not exist (vobject.go,
// finishItem). The same holds for Metadata.Extra["cards"] and
// ["timezones"], where a property of that name is merged with the
// library's value.
func TestReviewVobjAttributeKeysCollide(t *testing.T) {
	src := "BEGIN:VCALENDAR\r\nBEGIN:VEVENT\r\nSUMMARY:S\r\nCOMPONENT:mine\r\nCHILDREN:7\r\nEND:VEVENT\r\nEND:VCALENDAR\r\n"
	doc := extract(t, src, nil)
	if !strings.Contains(allText(doc), "mine") {
		t.Errorf("the COMPONENT property is lost: %q", dump(src, &doc.Parts[0]))
	}
	if c, ok := doc.Parts[0].Blocks[0].Attrs[gofastemail.AttrChildren]; ok && len(doc.Children) == 0 {
		t.Errorf("Attrs[children] = %q with no children", c)
	}
}

// The details of a card collect TYPE values with slices.Contains for
// each one (vobject.go, card): quadratic in the number of values on one
// line. One 1 MiB line (150,000 values) takes about 12 s; a 256 MiB file
// of such lines, most of an hour. Four times the values should cost
// about four times the time, not sixteen.
func TestReviewVobjTypeValuesQuadratic(t *testing.T) {
	run := func(n int) time.Duration {
		var b strings.Builder
		b.WriteString("BEGIN:VCARD\r\nFN:A\r\nTEL;TYPE=")
		for i := 0; i < n; i++ {
			b.WriteString("t" + strconv.Itoa(i) + ",")
		}
		b.WriteString(":1\r\nEND:VCARD\r\n")
		start := time.Now()
		extract(t, b.String(), nil)
		return time.Since(start)
	}
	run(1000) // warm up
	// The fastest of three runs each, so that a busy machine's pauses do
	// not decide the ratio.
	best := func(n int) time.Duration {
		d := run(n)
		for range 2 {
			d = min(d, run(n))
		}
		return d
	}
	small, large := best(10_000), best(40_000)
	if large > 8*small {
		t.Errorf("10,000 TYPE values: %v; 40,000: %v (%.1f times)", small, large, float64(large)/float64(small))
	}
}
