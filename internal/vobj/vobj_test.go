package vobj

import (
	"fmt"
	"reflect"
	"strings"
	"testing"
)

// events returns every event of src, written as one string each.
func events(src string) ([]string, []Event, Problems) {
	p := NewParser([]byte(src))
	var out []string
	var all []Event
	for {
		e, ok := p.Next()
		if !ok {
			break
		}
		all = append(all, e)
		switch e.Kind {
		case Begin:
			out = append(out, fmt.Sprintf("BEGIN %s %d", e.Name, e.Depth))
		case End:
			s := fmt.Sprintf("END %s %d [%d:%d]", e.Name, e.Depth, e.Offset, e.Offset+e.Length)
			if e.Missing {
				s += " missing"
			}
			out = append(out, s)
		default:
			pr := e.Prop
			s := pr.Name
			if pr.Group != "" {
				s = pr.Group + "." + s
			}
			for _, q := range pr.Params {
				s += ";" + q.Name + "=" + strings.Join(q.Values, ",")
			}
			s += fmt.Sprintf(":%q [%d:%d]", pr.Value, pr.Offset, pr.Offset+pr.Length)
			if pr.Cut {
				s += " cut"
			}
			out = append(out, s)
		}
	}
	return out, all, p.Problems
}

func check(t *testing.T, src string, want ...string) Problems {
	t.Helper()
	got, _, pr := events(src)
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Errorf("events of %q:\ngot\n  %s\nwant\n  %s", src, strings.Join(got, "\n  "), strings.Join(want, "\n  "))
	}
	return pr
}

func TestUnfolding(t *testing.T) {
	for _, nl := range []string{"\r\n", "\n", "\r"} {
		src := "BEGIN:A" + nl + "X:ab" + nl + " cd" + nl + "\tef" + nl + "END:A" + nl
		n := len(nl)
		check(t, src,
			"BEGIN A 1",
			fmt.Sprintf("X:%q [%d:%d]", "abcdef", 7+n, 7+n+4+n+3+n+3),
			fmt.Sprintf("END A 1 [0:%d]", len(src)-n))
	}
	// Only one white space character is removed.
	check(t, "X:a\r\n  b", `X:"a b" [0:8]`)
}

func TestQuotedPrintableSoftBreak(t *testing.T) {
	src := "NOTE;ENCODING=QUOTED-PRINTABLE:a=3D=\r\nb=\r\n c\r\nX:y\r\n"
	check(t, src,
		`NOTE;ENCODING=QUOTED-PRINTABLE:"a=3D=\nb=\n c" [0:44]`,
		`X:"y" [46:49]`)
	// A bare 2.1 encoding, and a line ending in "=" without
	// quoted-printable: no soft break.
	check(t, "NOTE;QUOTED-PRINTABLE;CHARSET=UTF-8:a=\nb\nX:a=\nY:b",
		`NOTE;ENCODING=QUOTED-PRINTABLE;CHARSET=UTF-8:"a=\nb" [0:40]`,
		`X:"a=" [41:45]`, `Y:"b" [46:49]`)
}

// A soft break before a BEGIN line or the END line of an open component
// ends the value; before the END of a component that is not open, it
// continues.
func TestQuotedPrintableSoftBreakBoundary(t *testing.T) {
	src := "BEGIN:VCARD\nNOTE;QUOTED-PRINTABLE:a=\nend:vcard\nBEGIN:VCARD\nNOTE;QUOTED-PRINTABLE:b=\nbegin:x\nN;QUOTED-PRINTABLE:c=\nEND:Y\nEND:X\nEND:VCARD\n"
	_, ev, pr := events(src)
	var got []string
	for _, e := range ev {
		if e.Kind == Prop {
			got = append(got, e.Prop.Value)
		}
	}
	if want := []string{"a=", "b=", "c=\nEND:Y"}; !reflect.DeepEqual(got, want) || pr.SoftBreakEnds != 2 {
		t.Errorf("values %q, SoftBreakEnds %d; want %q, 2", got, pr.SoftBreakEnds, want)
	}
}

func TestParameters(t *testing.T) {
	check(t, `item1.TEL;type=CELL;TYPE="a,b":1`+"\n"+`ATTENDEE;CN="Doe, J^'x^' ^^ ^n";x-y=a,"b:c",d:mailto:j@x`+"\n"+"TEL;CELL;PREF:2",
		`item1.TEL;TYPE=CELL;TYPE=a,b:"1" [0:32]`,
		`ATTENDEE;CN=Doe, J"x" ^ `+"\n"+`;X-Y=a,b:c,d:"mailto:j@x" [33:89]`,
		`TEL;TYPE=CELL;TYPE=PREF:"2" [90:105]`)
	_, ev, _ := events("TEL;TYPE=home;Type=pref,voice:3")
	pr := ev[0].Prop
	if got := pr.ParamValues("TYPE"); !reflect.DeepEqual(got, []string{"home", "pref", "voice"}) {
		t.Errorf("ParamValues %q", got)
	}
	if pr.Param("TYPE") != "home" || pr.Param("X") != "" || !pr.HasParam("TYPE") || pr.HasParam("X") {
		t.Error("Param, HasParam")
	}
	for v, want := range map[string]string{"b": "B", "BASE64": "B", "quoted-printable": "QUOTED-PRINTABLE", "8bit": ""} {
		_, ev, _ := events("X;ENCODING=" + v + ":y")
		if got := ev[0].Prop.Encoding(); got != want {
			t.Errorf("Encoding %s: %q", v, got)
		}
	}
}

func TestEscapes(t *testing.T) {
	if got := Unescape(`a\\b\;c\,d\ne\Nf\:g\`); got != "a\\b;c,d\ne\nf:g\\" {
		t.Errorf("Unescape %q", got)
	}
	if got := Split(`Doe\;x;John;;Mr.\\;`, ';'); !reflect.DeepEqual(got, []string{`Doe\;x`, "John", "", `Mr.\\`, ""}) {
		t.Errorf("Split %q", got)
	}
	if got := Split(`a\,b,c`, ','); !reflect.DeepEqual(got, []string{`a\,b`, "c"}) {
		t.Errorf("Split %q", got)
	}
}

func TestNesting(t *testing.T) {
	pr := check(t, "BEGIN:VCALENDAR\nBEGIN:VEVENT\nBEGIN:VALARM\nEND:VEVENT\nEND:X\nend:vcalendar\n",
		"BEGIN VCALENDAR 1", "BEGIN VEVENT 2", "BEGIN VALARM 3",
		"END VALARM 3 [29:41] missing", "END VEVENT 2 [16:52]", "END VCALENDAR 1 [0:72]")
	if pr != (Problems{UnmatchedEnd: 1, MissingEnd: 1}) {
		t.Errorf("problems %+v", pr)
	}
	pr = check(t, "BEGIN:VCARD\nFN:a\nBEGIN:VCARD\nFN:b",
		"BEGIN VCARD 1", `FN:"a" [12:16]`, "BEGIN VCARD 2", `FN:"b" [29:33]`,
		"END VCARD 2 [17:33] missing", "END VCARD 1 [0:33] missing")
	if pr != (Problems{MissingEnd: 2}) {
		t.Errorf("problems %+v", pr)
	}
}

func TestDepthBound(t *testing.T) {
	var b strings.Builder
	for i := 0; i < MaxDepth+3; i++ {
		fmt.Fprintf(&b, "BEGIN:C%d\nX:%d\n", i, i)
	}
	for i := MaxDepth + 2; i >= 0; i-- {
		fmt.Fprintf(&b, "END:C%d\n", i)
	}
	b.WriteString("Y:after\n")
	_, ev, pr := events(b.String())
	if pr != (Problems{TooDeep: 1}) {
		t.Errorf("problems %+v", pr)
	}
	begins, props := 0, 0
	for _, e := range ev {
		switch e.Kind {
		case Begin:
			begins++
			if e.Depth > MaxDepth {
				t.Errorf("depth %d", e.Depth)
			}
		case Prop:
			props++
		}
	}
	if begins != MaxDepth || props != MaxDepth+1 || ev[len(ev)-1].Prop.Value != "after" {
		t.Errorf("begins %d props %d", begins, props)
	}
}

func TestLineBound(t *testing.T) {
	long := strings.Repeat("a", MaxLine)
	src := "X:" + long[:MaxLine/2] + "\r\n " + long + "\r\nY:b"
	_, ev, pr := events(src)
	if pr.LongLines != 1 || len(ev) != 2 {
		t.Fatalf("problems %+v, %d events", pr, len(ev))
	}
	x := ev[0].Prop
	if !x.Cut || len(x.Value) != MaxLine-2 || x.Offset != 0 || x.Length != len(src)-5 {
		t.Errorf("cut %v, value %d bytes, span %d+%d", x.Cut, len(x.Value), x.Offset, x.Length)
	}
	if y := ev[1].Prop; y.Value != "b" || y.Offset != len(src)-3 {
		t.Errorf("after the long line: %+v", y)
	}
}

func TestNotContentLines(t *testing.T) {
	pr := check(t, "\xEF\xBB\xBFBEGIN:VEVENT\r\nDESCRIPTION:one\r\ntwo: three\r\nmeet at 10:30\r\n\r\nEND:VEVENT\r\nloose",
		"BEGIN VEVENT 1",
		`DESCRIPTION:"one" [17:32]`,
		// "two: three" reads as a property, since "two" is a name; the
		// line after it has no name before its colon, so it continues it.
		`TWO:" three\nmeet at 10:30" [34:59]`,
		"END VEVENT 1 [3:73]",
		// A line outside any property: a property with no name.
		`:"loose" [75:80]`)
	if pr != (Problems{NotLines: 2}) {
		t.Errorf("problems %+v", pr)
	}
}

func TestOffsetsExact(t *testing.T) {
	src := "\xEF\xBB\xBFBEGIN:VCARD\r\nFN:Jo\r\n hn\r\nN;CHARSET=UTF-8:Doe;John\nEND:VCARD\r\n"
	_, ev, _ := events(src)
	for _, e := range ev {
		if e.Kind != Prop {
			continue
		}
		raw := src[e.Prop.Offset : e.Prop.Offset+e.Prop.Length]
		if !strings.HasPrefix(raw, e.Prop.Name) || strings.ContainsAny(raw[len(raw)-1:], "\r\n") {
			t.Errorf("span of %s: %q", e.Prop.Name, raw)
		}
	}
	end := ev[len(ev)-1]
	if got := src[end.Offset : end.Offset+end.Length]; !strings.HasPrefix(got, "BEGIN:VCARD") || !strings.HasSuffix(got, "END:VCARD") {
		t.Errorf("component span %q", got)
	}
}
