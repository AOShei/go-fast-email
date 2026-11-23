package gofastemail

func init() {
	// Every test runs with released memory overwritten: see reuse_test.go.
	poisonReleased = true
}
