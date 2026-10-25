package main

import (
	"testing"

	changelog "github.com/tinchoram/groot-cli"
)

// A release bump needs its changelog heading: this fails until ## <version>
// exists in CHANGELOG.md.
func TestVersionHasAChangelogSection(t *testing.T) {
	if _, ok := changelog.Section(version); !ok {
		t.Fatalf("CHANGELOG.md has no ## %s heading: add the release entry before bumping the version", version)
	}
}
