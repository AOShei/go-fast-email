//go:build !race

package gofastemail_test

// raceSlowdown scales the time bounds of the worst-case tests: 1 without
// the race detector.
const raceSlowdown = 1
