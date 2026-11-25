package vobj

import (
	"strings"
	"testing"
)

// When the input ends inside a component skipped by the depth bound, the
// components still open have no END line, and the parser closes them
// with Missing set, but does not count them in Problems.MissingEnd
// (vobj.go, step: "if p.skip == 0"), so the library raises no
// input.repaired for them. The 32 events below are reported Missing and
// MissingEnd is 0.
func TestReviewMissingEndUnderSkip(t *testing.T) {
	src := strings.Repeat("BEGIN:VEVENT\nSUMMARY:s\n", MaxDepth+3)
	_, ev, pr := events(src)
	missing := 0
	for _, e := range ev {
		if e.Kind == End && e.Missing {
			missing++
		}
	}
	if missing == 0 || pr.MissingEnd < missing {
		t.Errorf("%d components closed as missing their END, Problems.MissingEnd %d", missing, pr.MissingEnd)
	}
}
