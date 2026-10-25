package changelog

import (
	"strings"
	"testing"
)

const fixture = `# Changelog

## 0.6.0-dev (unreleased)
### Added
- new thing

## 0.5.2 — 2026-10-25
### Fixed
- old thing

## 0.5.1 — 2026-10-25
- older
`

func TestSectionOfMatchesTheReleaseHeading(t *testing.T) {
	got, ok := SectionOf(fixture, "0.5.2")
	if !ok {
		t.Fatal("0.5.2 must be found")
	}
	if !strings.HasPrefix(got, "## 0.5.2 — 2026-10-25") {
		t.Fatalf("section must start with its heading: %q", got)
	}
	if !strings.Contains(got, "old thing") {
		t.Fatalf("section lacks its body: %q", got)
	}
	if strings.Contains(got, "older") || strings.Contains(got, "new thing") {
		t.Fatalf("section runs past its heading: %q", got)
	}
}

func TestSectionOfMatchesAnUnreleasedHeading(t *testing.T) {
	got, ok := SectionOf(fixture, "0.6.0-dev")
	if !ok {
		t.Fatal("0.6.0-dev must be found")
	}
	if !strings.HasPrefix(got, "## 0.6.0-dev (unreleased)") || !strings.Contains(got, "new thing") {
		t.Fatalf("unreleased section = %q", got)
	}
}

func TestSectionOfMatchesTheWholeVersionToken(t *testing.T) {
	if _, ok := SectionOf(fixture, "0.5"); ok {
		t.Fatal("a prefix of a version is not a match")
	}
}

func TestSectionOfMissingVersion(t *testing.T) {
	for _, v := range []string{"9.9.9", "", "   "} {
		if got, ok := SectionOf(fixture, v); ok {
			t.Fatalf("%q must not match, got %q", v, got)
		}
	}
}

func TestSectionReadsTheEmbeddedChangelog(t *testing.T) {
	if !strings.HasPrefix(Text, "# Changelog") {
		t.Fatal("Text must hold CHANGELOG.md")
	}
	if _, ok := Section("0.5.2"); !ok {
		t.Fatal("the embedded changelog must hold the 0.5.2 section")
	}
}
