package gofastemail_test

import (
	"strings"
	"testing"

	"github.com/AOShei/go-fast-core"
)

// TestVobjParameters: every parameter is kept under <property>.<name>,
// repeats joined by "\n", except ENCODING, CHARSET and the TYPE values
// written in the details; a grouped property keeps its group; a property
// whose key would be one of the library's is under "property.".
func TestVobjParameters(t *testing.T) {
	card := "BEGIN:VCARD\r\nVERSION:3.0\r\nFN:A\r\nTEL;TYPE=cell;X-A=1;X-A=2:1\r\n" +
		"NOTE;CHARSET=UTF-8;ENCODING=QUOTED-PRINTABLE;TYPE=x:n=C3=A9\r\n" +
		"item1.X-ABRELATEDNAMES:Bo\r\nitem1.X-ABLabel:_$!<Friend>!$_\r\nEND:VCARD\r\n"
	doc := extract(t, card, nil)
	got := doc.Parts[0].Blocks[0].Attrs
	want := map[string]string{"component": "vcard", "version": "3.0", "tel.x-a": "1\n2", "note.type": "x",
		"item1.x-abrelatednames": "Bo", "item1.x-ablabel": "_$!<Friend>!$_"}
	if len(got) != len(want) {
		t.Errorf("attrs %q, want %q", got, want)
	}
	for k, v := range want {
		if got[k] != v {
			t.Errorf("attrs[%q] = %q, want %q (all: %q)", k, got[k], v, got)
		}
	}

	cal := "BEGIN:VCALENDAR\r\nTIMEZONES:mine\r\nX-WR-CALNAME;X-LANG=fr:Cal\r\nBEGIN:VTIMEZONE\r\nTZID:Europe/Paris\r\nEND:VTIMEZONE\r\nEND:VCALENDAR\r\n"
	doc = extract(t, cal, nil)
	extra := doc.Metadata.Extra
	if extra["timezones"] != "Europe/Paris" || extra["property.timezones"] != "mine" || extra["x-wr-calname.x-lang"] != "fr" {
		t.Errorf("extra %q", extra)
	}
	list := "BEGIN:VCARD\r\nFN:A\r\nEND:VCARD\r\nCARDS:3\r\n"
	doc = extract(t, list, nil)
	if extra = doc.Metadata.Extra; extra["cards"] != "1" || extra["property.cards"] != "3" {
		t.Errorf("extra %q", extra)
	}
}

// TestVobjAltDescOnly: an event whose only text is its HTML description
// gives the details paragraph naming it, with the attributes.
func TestVobjAltDescOnly(t *testing.T) {
	src := "BEGIN:VCALENDAR\r\nBEGIN:VEVENT\r\nUID:u\r\nX-ALT-DESC;FMTTYPE=text/html:<p>x</p>\r\nEND:VEVENT\r\nEND:VCALENDAR\r\n"
	var got []seen
	doc := extract(t, src, &gofastcore.Options{Child: dispatcher(&got)})
	b := doc.Parts[0].Blocks
	if len(b) != 1 || b[0].Text != "Attachments: (text/html)" || b[0].Attrs["uid"] != "u" || b[0].Attrs["children"] != "0" {
		t.Errorf("blocks %+v", b)
	}
	if hasWarning(doc, "email.component_without_text") {
		t.Errorf("warnings %q", wlist(doc.Parts[0].Warnings))
	}
	// With a DESCRIPTION, the HTML description is not named.
	src = strings.Replace(src, "UID:u\r\n", "UID:u\r\nDESCRIPTION:d\r\n", 1)
	doc = extract(t, src, &gofastcore.Options{Child: dispatcher(&got)})
	if b = doc.Parts[0].Blocks; len(b) != 1 || b[0].Text != "d" {
		t.Errorf("blocks %+v", b)
	}
}
