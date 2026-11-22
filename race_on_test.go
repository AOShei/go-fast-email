//go:build race

package gofastemail_test

// raceSlowdown scales the time bounds of the worst-case tests: the race
// detector makes the reading several times slower, most on the shared
// runners of CI.
const raceSlowdown = 4
