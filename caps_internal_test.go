package gofastemail

import (
	"testing"

	"github.com/AOShei/go-fast-core"
)

// A grouped warning is rewritten only where it still is: where a part
// keeps no more warnings (core caps them), its entry may be missing or
// hold another warning, which must be left as it is.
func TestGroupNotRewrittenOutOfPlace(t *testing.T) {
	many := func(count, list string) string { return count + " values: " + list }
	for _, damage := range []func(p *gofastcore.Part){
		func(p *gofastcore.Part) { p.Warnings = p.Warnings[:0] }, // never kept
		func(p *gofastcore.Part) {
			p.Warnings[0] = gofastcore.Warning{Code: "warnings.omitted", Message: "omitted"}
		},
	} {
		m := &message{}
		m.warnEach(gofastcore.SeverityWarning, "x.code", "", "a", "a", "one a", false, many)
		m.warnEach(gofastcore.SeverityWarning, "x.code", "", "b", "b", "one b", false, many)
		m.warnCount(gofastcore.WarnLimitExceeded, "k", 2, func(n int) string { return "counted" })
		damage(&m.part)
		before := append([]gofastcore.Warning(nil), m.part.Warnings...)
		m.finish() // must not panic
		for i, w := range m.part.Warnings {
			if i < len(before) && w.Code == "warnings.omitted" && w != before[i] {
				t.Errorf("the omitted entry was rewritten: %+v", w)
			}
		}
	}
	// In place, it is rewritten.
	m := &message{}
	m.warnEach(gofastcore.SeverityWarning, "x.code", "", "a", "a", "one a", false, many)
	m.warnEach(gofastcore.SeverityWarning, "x.code", "", "b", "b", "one b", false, many)
	m.finish()
	if got := m.part.Warnings[0].Message; got != `2 values: "a", "b"` {
		t.Errorf("message %q", got)
	}
}
