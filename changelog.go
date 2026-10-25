// Package changelog embeds the CHANGELOG.md of this repository, so the binary
// can read its own release notes. It sits at the module root because go:embed
// cannot reach a parent directory.
package changelog

import (
	_ "embed"
	"strings"
)

// Text is the whole CHANGELOG.md.
//
//go:embed CHANGELOG.md
var Text string

// Section returns the entry of version in the embedded changelog.
func Section(version string) (string, bool) {
	return SectionOf(Text, version)
}

// SectionOf returns the section of text whose heading names version: the
// heading line and the body up to the next "## " heading. The version is the
// first word after "## ", so "## 0.5.2 — 2026-10-25" matches "0.5.2" and
// "## 0.6.0-dev (unreleased)" matches "0.6.0-dev".
func SectionOf(text, version string) (string, bool) {
	version = strings.TrimSpace(version)
	if version == "" {
		return "", false
	}
	lines := strings.Split(text, "\n")
	start := -1
	for i, line := range lines {
		if start < 0 {
			if strings.HasPrefix(line, "## ") && headingVersion(line) == version {
				start = i
			}
			continue
		}
		if strings.HasPrefix(line, "## ") {
			return strings.TrimSpace(strings.Join(lines[start:i], "\n")), true
		}
	}
	if start < 0 {
		return "", false
	}
	return strings.TrimSpace(strings.Join(lines[start:], "\n")), true
}

// headingVersion returns the first word after "## " on a heading line.
func headingVersion(line string) string {
	fields := strings.Fields(strings.TrimPrefix(line, "## "))
	if len(fields) == 0 {
		return ""
	}
	return fields[0]
}
