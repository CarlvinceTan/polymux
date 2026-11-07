package server

import (
	"regexp"
	"testing"
)

var generatedName = regexp.MustCompile(`^[A-Z][a-z]{3,4}$`)

func assertName(t *testing.T, name string) {
	t.Helper()
	if !generatedName.MatchString(name) {
		t.Fatalf("generated name %q must contain 4–5 letters", name)
	}
}

func TestRandomName(t *testing.T) {
	t.Parallel()
	seen := map[string]bool{}
	lengths := map[int]bool{}
	for range 1000 {
		name := randomName()
		assertName(t, name)
		seen[name] = true
		lengths[len(name)] = true
	}
	if len(seen) < 2 || !lengths[4] || !lengths[5] {
		t.Fatalf("names did not vary: %d distinct names, lengths %v", len(seen), lengths)
	}
}
