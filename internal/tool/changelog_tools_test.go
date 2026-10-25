package tool

import (
	"context"
	"strings"
	"testing"
)

const testChangelog = `# Changelog

## 0.6.0-dev (unreleased)
- new thing

## 0.5.2 — 2026-10-25
- old thing
`

func TestReadChangelogReturnsTheWholeTextWithoutAVersion(t *testing.T) {
	tl := NewReadChangelog(testChangelog)
	for _, in := range []string{"", "{}", `{"version":""}`} {
		out, isErr := tl.Execute(context.Background(), in)
		if isErr || out != testChangelog {
			t.Fatalf("input %q: out=%q err=%v", in, out, isErr)
		}
	}
}

func TestReadChangelogReturnsOneSection(t *testing.T) {
	out, isErr := NewReadChangelog(testChangelog).Execute(context.Background(), `{"version":"0.5.2"}`)
	if isErr {
		t.Fatalf("section read failed: %q", out)
	}
	if !strings.HasPrefix(out, "## 0.5.2") || !strings.Contains(out, "old thing") || strings.Contains(out, "new thing") {
		t.Fatalf("section = %q", out)
	}
}

func TestReadChangelogUnknownVersion(t *testing.T) {
	out, isErr := NewReadChangelog(testChangelog).Execute(context.Background(), `{"version":"9.9.9"}`)
	if isErr || out != "no changelog entry for 9.9.9" {
		t.Fatalf("unknown version: out=%q err=%v", out, isErr)
	}
}

func TestReadChangelogRefusesInvalidInput(t *testing.T) {
	if _, isErr := NewReadChangelog(testChangelog).Execute(context.Background(), `{"version":`); !isErr {
		t.Fatal("malformed input must be an error")
	}
}

func TestReadChangelogIsARead(t *testing.T) {
	tl := NewReadChangelog(testChangelog)
	if tl.Risk() != RiskRead || tl.RiskFor(`{"version":"0.5.2"}`) != RiskRead {
		t.Fatal("reading the changelog is a read")
	}
	if tl.Definition().Name != "read_changelog" {
		t.Fatalf("name = %q", tl.Definition().Name)
	}
}
