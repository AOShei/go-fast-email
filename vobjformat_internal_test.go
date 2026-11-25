package gofastemail

import "testing"

func TestVobjFormat(t *testing.T) {
	for in, want := range map[string]string{
		"BEGIN:VCALENDAR\r\n":           "ics",
		"\xEF\xBB\xBF \nbegin:vevent\n": "ics",
		"BEGIN:VTIMEZONE\n":             "ics",
		"home.begin:vcard\n":            "vcf",
		"BEGIN:VCARD":                   "vcf",
		"BEGIN:VCARDS\n":                "",
		"BEGIN:X-MYCOMP\n":              "",
		"a.b.BEGIN:VCARD\n":             "",
		"Subject: BEGIN:VCARD\n":        "",
	} {
		if got := vobjFormat([]byte(in)); got != want {
			t.Errorf("vobjFormat(%q) = %q, want %q", in, got, want)
		}
	}
}
