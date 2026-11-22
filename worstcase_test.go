package gofastemail_test

import (
	"strings"
	"testing"
	"time"
)

// TestWorstCase runs inputs built to make a careless parser quadratic.
// Each is a few megabytes and must be read in well under the time a
// quadratic reading would take (minutes).
func TestWorstCase(t *testing.T) {
	cases := map[string]string{
		"folded header": "From: a\nX-Long: x\n" + strings.Repeat(" y\n", 300_000) + "\nbody",
		"encoded words": "From: a\nSubject: " + strings.Repeat("=?utf-8?q?a?= ", 70_000) + "\n\nbody",
		"broken words":  "From: a\nSubject: " + strings.Repeat("=?", 400_000) + "\n\nbody",
		"near boundaries": "From: a\nContent-Type: multipart/mixed; boundary=bb\n\n" +
			strings.Repeat("--b\n", 500_000) + "--bb--\n",
		"many parts": "From: a\nContent-Type: multipart/mixed; boundary=b\n\n" + strings.Repeat("--b\n\nx\n", 200_000),
		"mailbox":    strings.Repeat("From x\nSubject: s\n\nb\n\n", 100_000),
		"quotes":     "From: a\n\n" + strings.Repeat(">", 2_000_000),
	}
	for name, data := range cases {
		start := time.Now()
		extract(t, data, nil)
		if d := time.Since(start); d > 10*raceSlowdown*time.Second {
			t.Errorf("%s: %v", name, d)
		}
	}
}
