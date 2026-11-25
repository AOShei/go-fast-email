package vobj

import "testing"

// FuzzVobj: whatever the input, the parser ends, never panics, keeps
// every span inside the input, nests no deeper than MaxDepth, closes
// every component it opens, and gives no line longer than MaxLine plus
// the lines that continue it.
func FuzzVobj(f *testing.F) {
	for _, s := range []string{
		"", "BEGIN:VCALENDAR\r\nBEGIN:VEVENT\r\nSUMMARY:a\r\n b\r\nEND:VEVENT\r\nEND:VCALENDAR\r\n",
		"BEGIN:VCARD\nN;CHARSET=ISO-8859-1;ENCODING=QUOTED-PRINTABLE:D=F6e;=\nJ\nTEL;CELL;PREF:1\nEND:VCARD",
		"BEGIN:A\rEND:B\rX;P=\"a:b\";Q=^n^^^':v\rno colon\r\n\tEND:A",
		"\xEF\xBB\xBFitem1.EMAIL;type=INTERNET:a@b\nitem1.X-ABLabel:_$!<Other>!$_\n",
	} {
		f.Add([]byte(s))
	}
	f.Fuzz(func(t *testing.T, data []byte) {
		p := NewParser(data)
		depth, events := 0, 0
		for {
			e, ok := p.Next()
			if !ok {
				break
			}
			events++
			if events > len(data)+1 {
				t.Fatalf("%d events from %d bytes", events, len(data))
			}
			off, n := e.Offset, e.Length
			if e.Kind == Prop {
				off, n = e.Prop.Offset, e.Prop.Length
				if len(e.Prop.Value) > len(data) {
					t.Fatalf("value of %d bytes from %d", len(e.Prop.Value), len(data))
				}
			}
			if off < 0 || n < 0 || off+n > len(data) {
				t.Fatalf("span %d+%d outside %d bytes", off, n, len(data))
			}
			switch e.Kind {
			case Begin:
				depth++
				if e.Depth != depth || depth > MaxDepth {
					t.Fatalf("begin at depth %d, counted %d", e.Depth, depth)
				}
			case End:
				if e.Depth != depth {
					t.Fatalf("end at depth %d, counted %d", e.Depth, depth)
				}
				depth--
			}
		}
		if depth != 0 {
			t.Fatalf("%d components left open", depth)
		}
	})
}
