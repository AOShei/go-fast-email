package gofastemail_test

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"image"
	"image/png"
	"sort"
	"strings"
	"testing"

	"github.com/AOShei/go-fast-core"
	"github.com/AOShei/go-fast-email"
)

// dump writes the blocks of a part with their kind, text, place and
// attributes, one string each.
func dump(src string, p *gofastcore.Part) []string {
	var out []string
	for _, b := range p.Blocks {
		s := b.Kind.String() + ": " + b.Text
		if b.Loc.Length > 0 {
			s += fmt.Sprintf(" @%q", src[b.Loc.Offset:b.Loc.Offset+b.Loc.Length])
		}
		if b.Loc.Anchor != "" {
			s += " #" + b.Loc.Anchor
		}
		keys := make([]string, 0, len(b.Attrs))
		for k := range b.Attrs {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			s += fmt.Sprintf(" [%s=%q]", k, b.Attrs[k])
		}
		out = append(out, s)
	}
	return out
}

func wlist(ws []gofastcore.Warning) []string {
	var out []string
	for _, w := range ws {
		out = append(out, w.Code+" "+w.Severity.String()+": "+w.Message)
	}
	return out
}

const calendar = "BEGIN:VCALENDAR\r\n" +
	"PRODID:-//Example//Cal 1.0//EN\r\n" +
	"VERSION:2.0\r\n" +
	"METHOD:PUBLISH\r\n" +
	"X-WR-CALNAME:Team\r\n" +
	"X-WR-CALDESC:One\r\n" +
	"X-WR-CALDESC:Two\r\n" +
	"BEGIN:VTIMEZONE\r\n" +
	"TZID:Europe/Paris\r\n" +
	"BEGIN:STANDARD\r\nDTSTART:19701025T030000\r\nTZOFFSETTO:+0100\r\nEND:STANDARD\r\n" +
	"END:VTIMEZONE\r\n" +
	"BEGIN:VEVENT\r\n" +
	"UID:ev-1@example.com\r\n" +
	"DTSTAMP:20261120T090000Z\r\n" +
	"SUMMARY:Planning \\; review\r\n" +
	"DTSTART;TZID=Europe/Paris:20261125T100000\r\n" +
	"DTEND:20261125T110000Z\r\n" +
	"RRULE:FREQ=WEEKLY;COUNT=3\r\n" +
	"LOCATION:Room 4\\, floor 2\r\n" +
	"ORGANIZER;CN=Ann Lee:mailto:ann@example.com\r\n" +
	"ATTENDEE;CN=\"Bob, B.\";PARTSTAT=ACCEPTED:mailto:bob@example.com\r\n" +
	"ATTENDEE:MAILTO:cy@example.com\r\n" +
	"CATEGORIES:Work,Plans\r\n" +
	"CATEGORIES:Q4\r\n" +
	"STATUS:CONFIRMED\r\n" +
	"DESCRIPTION:First paragraph\\nsecond line.\\n\\nSecond para\r\n" +
	" graph.\r\n" +
	"COMMENT:A comment\r\n" +
	"X-MICROSOFT-CDO-BUSYSTATUS:BUSY\r\n" +
	"EXDATE:20261202T100000\r\n" +
	"EXDATE:20261209T100000\r\n" +
	"BEGIN:VALARM\r\nACTION:DISPLAY\r\nTRIGGER:-PT15M\r\nDESCRIPTION:Soon\r\nEND:VALARM\r\n" +
	"END:VEVENT\r\n" +
	"BEGIN:VTODO\r\nDUE;VALUE=DATE:20261201\r\nSTATUS:NEEDS-ACTION\r\nEND:VTODO\r\n" +
	"BEGIN:VTODO\r\nUID:bare\r\nEND:VTODO\r\n" +
	"END:VCALENDAR\r\n"

func TestCalendar(t *testing.T) {
	doc := extract(t, calendar, nil)
	if doc.Source.Format != "ics" || doc.Source.MIME != "text/calendar" || len(doc.Parts) != 1 || doc.Parts[0].Kind != gofastcore.PartBody {
		t.Fatalf("source %+v, %d parts", doc.Source, len(doc.Parts))
	}
	md := doc.Metadata
	if md.Title != "Team" || md.Application != "-//Example//Cal 1.0//EN" {
		t.Errorf("title %q, application %q", md.Title, md.Application)
	}
	wantExtra := map[string]string{"version": "2.0", "method": "PUBLISH", "x-wr-caldesc": "One\nTwo", "timezones": "Europe/Paris"}
	if fmt.Sprint(md.Extra) != fmt.Sprint(wantExtra) {
		t.Errorf("extra %q", md.Extra)
	}
	equal(t, "blocks", dump(calendar, &doc.Parts[0]), []string{
		`heading: Planning ; review @"SUMMARY:Planning \\; review" #ev-1@example.com [alarm.action="DISPLAY"] [component="vevent"] [dtstamp="20261120T090000Z"] [exdate="20261202T100000\n20261209T100000"] [uid="ev-1@example.com"] [x-microsoft-cdo-busystatus="BUSY"]`,
		"paragraph: Start: 2026-11-25T10:00:00 (Europe/Paris)\nEnd: 2026-11-25T11:00:00Z\nRepeats: FREQ=WEEKLY;COUNT=3\nLocation: Room 4, floor 2\n" +
			"Organizer: Ann Lee <ann@example.com>\nAttendees: Bob, B. <bob@example.com>, cy@example.com\nStatus: CONFIRMED\nCategories: Work, Plans, Q4\nAlarm: -PT15M Soon" +
			fmt.Sprintf(" @%q #ev-1@example.com", calendar[strings.Index(calendar, "BEGIN:VEVENT"):strings.Index(calendar, "END:VEVENT")+10]),
		`paragraph: First paragraph` + "\n" + `second line. @"DESCRIPTION:First paragraph\\nsecond line.\\n\\nSecond para\r\n graph." #ev-1@example.com`,
		`paragraph: Second paragraph. @"DESCRIPTION:First paragraph\\nsecond line.\\n\\nSecond para\r\n graph." #ev-1@example.com`,
		`paragraph: A comment @"COMMENT:A comment" #ev-1@example.com`,
		"paragraph: Due: 2026-12-01\nStatus: NEEDS-ACTION @\"BEGIN:VTODO\\r\\nDUE;VALUE=DATE:20261201\\r\\nSTATUS:NEEDS-ACTION\\r\\nEND:VTODO\" [component=\"vtodo\"]",
	})
	equal(t, "warnings", wlist(doc.Parts[0].Warnings), []string{
		"email.component_without_text info: components with no text give no block, so their properties are not in the output: 1 (vtodo)",
	})
}

const invitation = "BEGIN:VCALENDAR\r\n" +
	"PRODID:-//Microsoft Corporation//Outlook 16.0 MIMEDIR//EN\r\n" +
	"VERSION:2.0\r\n" +
	"METHOD:REQUEST\r\n" +
	"BEGIN:VEVENT\r\n" +
	"CLASS:PUBLIC\r\n" +
	"DESCRIPTION:Agenda attached.\\n\r\n" +
	"DTSTART:20261201T090000Z\r\n" +
	"SUMMARY:Kick-off\r\n" +
	"UID:040000008200E00074C5B7101A82E008\r\n" +
	"ATTACH;ENCODING=BASE64;VALUE=BINARY;FMTTYPE=text/plain;X-FILENAME=agenda.txt:SXRlbSAxCg==\r\n" +
	"ATTACH:https://example.com/files/plan.pdf\r\n" +
	"X-ALT-DESC;FMTTYPE=text/html:<html><body><p>Agenda\\, attached.</p></body></html>\r\n" +
	"END:VEVENT\r\n" +
	"END:VCALENDAR\r\n"

func TestOutlookInvitation(t *testing.T) {
	var got []seen
	doc := extract(t, invitation, &gofastcore.Options{Child: dispatcher(&got)})
	uid := " #040000008200E00074C5B7101A82E008"
	equal(t, "blocks", dump(invitation, &doc.Parts[0]), []string{
		`heading: Kick-off @"SUMMARY:Kick-off"` + uid + ` [attach="https://example.com/files/plan.pdf"] [children="0,1"] [class="PUBLIC"] [component="vevent"] [uid="040000008200E00074C5B7101A82E008"]`,
		"paragraph: Start: 2026-12-01T09:00:00Z\nAttachments: agenda.txt" +
			fmt.Sprintf(" @%q", invitation[strings.Index(invitation, "BEGIN:VEVENT"):strings.Index(invitation, "END:VEVENT")+10]) + uid,
		`paragraph: Agenda attached. @"DESCRIPTION:Agenda attached.\\n"` + uid,
	})
	if len(got) != 2 || got[0].name != "agenda.txt" || got[0].mime != "text/plain" || string(got[0].data) != "Item 1\n" ||
		got[1].mime != "text/html; charset=utf-8" || string(got[1].data) != "<html><body><p>Agenda, attached.</p></body></html>" {
		t.Errorf("children %q", got)
	}
	if len(doc.Children) != 2 || doc.Children[0].Source.Name != "agenda.txt" {
		t.Errorf("%d children", len(doc.Children))
	}
	equal(t, "warnings", wlist(doc.Parts[0].Warnings), []string{
		`input.external_reference warning: a ATTACH property refers to content outside the file: "https://example.com/files/plan.pdf"`,
		"email.alternatives warning: 1 component has a DESCRIPTION and an HTML description (X-ALT-DESC); the blocks are the DESCRIPTION, the HTML one is an embedded document",
	})
}

const contacts = "BEGIN:VCARD\r\n" +
	"VERSION:3.0\r\n" +
	"PRODID:-//Apple Inc.//iOS 5.0.1//EN\r\n" +
	"N:Doe;John;Richter,James;Mr.;Sr.\r\n" +
	"FN:Mr. John Richter James Doe Sr.\r\n" +
	"ORG:IBM;Accounting\r\n" +
	"TITLE:Money Counter\r\n" +
	"item1.EMAIL;type=INTERNET;type=pref:john.doe@ibm.com\r\n" +
	"TEL;type=CELL;type=VOICE;type=pref:905-555-1234\r\n" +
	"item2.TEL:905-222-1234\r\n" +
	"item2.X-ABLabel:_$!<AssistantPhone>!$_\r\n" +
	"item3.ADR;type=HOME:;;Silicon Alley 5;New York;NY;12345;USA\r\n" +
	"item4.URL:http://example.com\r\n" +
	"item4.X-ABLabel:My site\r\n" +
	"BDAY;value=date:2012-06-06\r\n" +
	"NOTE:Met at the fair.\r\n" +
	"UID:card-1\r\n" +
	"X-SOCIALPROFILE;type=twitter:x\r\n" +
	"END:VCARD\r\n" +
	"BEGIN:VCARD\r\n" +
	"VERSION:4.0\r\n" +
	"N:Smith;Jane;;;\r\n" +
	"EMAIL:jane@example.com\r\n" +
	"PHOTO:http://example.com/jane.jpg\r\n" +
	"END:VCARD\r\n" +
	"BEGIN:VCARD\r\n" +
	"VERSION:4.0\r\n" +
	"ORG:Acme\\, Inc.;Sales\r\n" +
	"END:VCARD\r\n"

func TestContacts(t *testing.T) {
	doc := extract(t, contacts, nil)
	if doc.Source.Format != "vcf" || doc.Source.MIME != "text/vcard" || doc.Metadata.Extra["cards"] != "3" || doc.Metadata.Title != "" {
		t.Errorf("source %+v, metadata %+v", doc.Source, doc.Metadata)
	}
	card := func(i int) string {
		at := 0
		for ; i > 0; i-- {
			at += strings.Index(contacts[at:], "END:VCARD") + 11
		}
		return fmt.Sprintf(" @%q", contacts[at:at+strings.Index(contacts[at:], "END:VCARD")+9])
	}
	equal(t, "blocks", dump(contacts, &doc.Parts[0]), []string{
		`heading: Mr. John Richter James Doe Sr. @"FN:Mr. John Richter James Doe Sr." #card-1 [component="vcard"] [n="Doe;John;Richter,James;Mr.;Sr."] [prodid="-//Apple Inc.//iOS 5.0.1//EN"] [uid="card-1"] [version="3.0"] [x-socialprofile="x"]`,
		"paragraph: Organization: IBM, Accounting\nTitle: Money Counter\nEmail: john.doe@ibm.com (internet, pref)\nPhone: 905-555-1234 (cell, voice, pref)\nPhone: 905-222-1234 (assistantphone)\n" +
			"Address: Silicon Alley 5, New York, NY, 12345, USA (home)\nURL: http://example.com (My site)\nBirthday: 2012-06-06" + card(0) + " #card-1",
		`paragraph: Met at the fair. @"NOTE:Met at the fair." #card-1`,
		`heading: Jane Smith @"N:Smith;Jane;;;" [component="vcard"] [photo="http://example.com/jane.jpg"] [version="4.0"]`,
		"paragraph: Email: jane@example.com" + card(1),
		`heading: Acme, Inc. @"ORG:Acme\\, Inc.;Sales" [component="vcard"] [version="4.0"]`,
		"paragraph: Organization: Acme, Inc., Sales" + card(2),
	})
	equal(t, "warnings", wlist(doc.Parts[0].Warnings), []string{
		`input.external_reference warning: a PHOTO property refers to content outside the file: "http://example.com/jane.jpg"`,
	})
}

func TestVCard21(t *testing.T) {
	src := "BEGIN:VCARD\r\nVERSION:2.1\r\n" +
		"N;CHARSET=ISO-8859-1;ENCODING=QUOTED-PRINTABLE:M=FCller;J=FCrgen\r\n" +
		"TEL;CELL;PREF:+49 1\r\n" +
		"NOTE;ENCODING=QUOTED-PRINTABLE;CHARSET=ISO-8859-1:Gr=FC=DFe=0D=0A=\r\naus K=F6ln\r\n" +
		"END:VCARD\r\n"
	doc := extract(t, src, nil)
	equal(t, "blocks", dump(src, &doc.Parts[0]), []string{
		`heading: J` + "ü" + `rgen M` + "ü" + `ller @"N;CHARSET=ISO-8859-1;ENCODING=QUOTED-PRINTABLE:M=FCller;J=FCrgen" [component="vcard"] [version="2.1"]`,
		"paragraph: Phone: +49 1 (cell, pref)" + fmt.Sprintf(" @%q", strings.TrimSuffix(src, "\r\n")),
		"paragraph: Grüße\naus Köln @\"NOTE;ENCODING=QUOTED-PRINTABLE;CHARSET=ISO-8859-1:Gr=FC=DFe=0D=0A=\\r\\naus K=F6ln\"",
	})
	if len(doc.Parts[0].Warnings) != 0 {
		t.Errorf("warnings %q", wlist(doc.Parts[0].Warnings))
	}
	// Without a CHARSET, text that is not UTF-8 is read as text that
	// declares none: in the set given, else in the one its bytes are in.
	src = "BEGIN:VCARD\nFN:Ren\xe9\nEND:VCARD\n"
	doc = extract(t, src, nil)
	if got := doc.Parts[0].Blocks[0].Text; got != "René" || !hasWarning(doc, gofastcore.WarnTextEncodingDetected) {
		t.Errorf("%q %q", got, wlist(doc.Parts[0].Warnings))
	}
	doc, err := (&gofastemail.Extractor{Charset: "windows-1251"}).Extract(context.Background(), strings.NewReader(src), int64(len(src)), nil)
	if err != nil || doc.Parts[0].Blocks[0].Text != "Renй" || hasWarning(doc, gofastcore.WarnTextEncodingDetected) {
		t.Errorf("Charset: %v %q", err, doc.Parts[0].Blocks[0].Text)
	}
}

func TestContactPhoto(t *testing.T) {
	var buf bytes.Buffer
	if err := png.Encode(&buf, image.NewGray(image.Rect(0, 0, 3, 2))); err != nil {
		t.Fatal(err)
	}
	b64 := base64.StdEncoding.EncodeToString(buf.Bytes())
	src := "BEGIN:VCARD\nVERSION:3.0\nFN:A\nPHOTO;ENCODING=b;TYPE=PNG:" + b64[:20] + "\n " + b64[20:] + "\n" +
		"LOGO:data:image/png;base64," + b64 + "\nEND:VCARD\n"
	doc := extract(t, src, &gofastcore.Options{Images: gofastcore.ImagesData})
	bs := doc.Parts[0].Blocks
	if len(bs) != 3 || bs[1].Kind != gofastcore.KindImage || bs[2].Kind != gofastcore.KindImage {
		t.Fatalf("blocks %q", dump(src, &doc.Parts[0]))
	}
	for _, b := range bs[1:] {
		img := b.Image
		if img.MIME != "image/png" || img.Width != 3 || img.Height != 2 || !bytes.Equal(img.Data, buf.Bytes()) || img.ID == "" {
			t.Errorf("image %+v", img)
		}
	}
	if got := src[bs[1].Loc.Offset : bs[1].Loc.Offset+bs[1].Loc.Length]; !strings.HasPrefix(got, "PHOTO;") || !strings.HasSuffix(got, b64[20:]) {
		t.Errorf("photo at %q", got)
	}
	doc = extract(t, src, &gofastcore.Options{Images: gofastcore.ImagesNone})
	if len(doc.Parts[0].Blocks) != 1 {
		t.Errorf("ImagesNone: %q", dump(src, &doc.Parts[0]))
	}
}

func TestVobjDetection(t *testing.T) {
	for src, format := range map[string]string{
		"\xEF\xBB\xBF \r\n\tbegin:vcalendar\r\nEND:VCALENDAR": "ics",
		"Begin:VCard\nFN:a\nEnd:VCard":                        "vcf",
		"BEGIN:VCARDX\n\nbody":                                "eml",
		"BEGIN:VCALENDAR":                                     "ics",
	} {
		doc := extract(t, src, nil)
		if doc.Source.Format != format {
			t.Errorf("%q: %s, want %s", src, doc.Source.Format, format)
		}
	}
	e := &gofastemail.Extractor{}
	if f := e.Formats(); strings.Join(f, ",") != "eml,mbox,msg,tnef,ics,vcf" {
		t.Errorf("Formats %q", f)
	}
}

func TestVobjRepaired(t *testing.T) {
	src := "BEGIN:VCALENDAR\nBEGIN:VEVENT\nSUMMARY:a\nmore text\nEND:VTODO\n"
	doc := extract(t, src, nil)
	equal(t, "blocks", texts(&doc.Parts[0]), []string{"heading: a more text"})
	equal(t, "warnings", wlist(doc.Parts[0].Warnings), []string{
		"input.repaired info: 1 END line that matches no open component is skipped",
		"input.repaired info: 2 components have no END line; each ends where the component around it or the file ends",
		"input.repaired info: 1 line is not a content line (no colon, or no name before it); each such line is read as continuing the value of the property before it",
	})
	doc = extract(t, "BEGIN:VCALENDAR\r\nVERSION:2.0\r\nEND:VCALENDAR\r\n", nil)
	equal(t, "empty", wlist(doc.Parts[0].Warnings), []string{"part.no_text info: the calendar has no text"})

	var deep strings.Builder
	deep.WriteString("BEGIN:VCALENDAR\n")
	for i := 0; i < 40; i++ {
		deep.WriteString("BEGIN:VEVENT\nSUMMARY:s\n")
	}
	doc = extract(t, deep.String(), nil)
	if n := len(doc.Parts[0].Blocks); n != gofastemail.MaxComponentDepth-1 || !hasWarning(doc, gofastcore.WarnLimitExceeded) {
		t.Errorf("%d blocks, %q", n, wlist(doc.Parts[0].Warnings))
	}
}

func TestVobjOptions(t *testing.T) {
	ctx := context.Background()
	_, err := gofastemail.Extract(ctx, strings.NewReader(calendar), int64(len(calendar)), &gofastcore.Options{Parts: gofastcore.PartRange{First: 2, Last: 2}})
	if !errors.Is(err, gofastcore.ErrNoPartsSelected) {
		t.Errorf("Parts 2: %v", err)
	}
	doc := extract(t, calendar, &gofastcore.Options{Parts: gofastcore.PartRange{First: 1, Last: 1}})
	if len(doc.Parts) != 1 {
		t.Errorf("Parts 1: %d parts", len(doc.Parts))
	}
	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	if _, err := gofastemail.Extract(cancelled, strings.NewReader(calendar), int64(len(calendar)), nil); !errors.Is(err, context.Canceled) {
		t.Errorf("cancelled: %v", err)
	}
	doc = extract(t, calendar, &gofastcore.Options{Limits: gofastcore.Limits{MaxTextBytes: 300}})
	if n := len(doc.Parts[0].Blocks); n == 0 || n >= 6 || !doc.Parts[0].Has(gofastcore.WarnLimitExceeded) {
		t.Errorf("text limit: %d blocks, %q", n, wlist(doc.Parts[0].Warnings))
	}
	if hasWarning(doc, gofastemail.WarnComponentWithoutText) {
		t.Error("components cut by the text limit counted as having no text")
	}
	big := contacts + "BEGIN:VCARD\nPHOTO;ENCODING=b:" + strings.Repeat("QUFB", 1000) + "\nEND:VCARD\n"
	doc = extract(t, big, &gofastcore.Options{Limits: gofastcore.Limits{MaxDecodedBytes: 100}})
	if !hasWarning(doc, gofastcore.WarnLimitExceeded) {
		t.Errorf("decode limit: %q", wlist(doc.Parts[0].Warnings))
	}
	doc, err = (&gofastemail.Extractor{MaxMessageBytes: 100}).Extract(ctx, strings.NewReader(calendar), int64(len(calendar)), nil)
	if err != nil || !hasWarning(doc, gofastcore.WarnLimitExceeded) {
		t.Errorf("MaxMessageBytes: %v", err)
	}
}
