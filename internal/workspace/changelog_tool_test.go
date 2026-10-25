package workspace

import (
	"context"
	"slices"
	"strings"
	"testing"

	"github.com/tinchoram/groot-cli/internal/catalog"
)

func TestBuildToolsRegistersReadChangelog(t *testing.T) {
	w := &Workspace{}
	reg, err := w.buildTools(t.TempDir(), []string{"read_changelog"}, nil, nil, nil, nil, debugAgent)
	if err != nil {
		t.Fatalf("buildTools: %v", err)
	}
	tl, ok := reg.Get("read_changelog")
	if !ok {
		t.Fatal("read_changelog must be registered")
	}
	out, isErr := tl.Execute(context.Background(), "{}")
	if isErr || !strings.Contains(out, "## 0.5.2") {
		t.Fatalf("the embedded changelog must be returned: err=%v", isErr)
	}
}

func TestReadChangelogIsAKnownTool(t *testing.T) {
	if !slices.Contains(catalog.KnownTools, "read_changelog") {
		t.Fatal("read_changelog must be a known tool so agents can be granted it")
	}
}

func TestDebugReadsTheEmbeddedChangelogNotTheSpace(t *testing.T) {
	if !slices.Contains(debugDef().Tools, "read_changelog") {
		t.Fatalf("debug must carry read_changelog: %v", debugDef().Tools)
	}
	if slices.Contains(debugDef().Tools, "read_file") {
		t.Fatalf("read_file was only for the sweep and must leave debug: %v", debugDef().Tools)
	}
}

func TestReleaseSweepRuleCallsReadChangelog(t *testing.T) {
	if !strings.Contains(debugSweepRule, "read_changelog") {
		t.Fatalf("the sweep must call read_changelog:\n%s", debugSweepRule)
	}
	if strings.Contains(debugSweepRule, "read_file") {
		t.Fatalf("the sweep must not read CHANGELOG.md from the space:\n%s", debugSweepRule)
	}
	for _, want := range []string{"(unreleased)", "-dev", "not shipped"} {
		if !strings.Contains(debugSweepRule, want) {
			t.Fatalf("the sweep must say that unreleased entries are not shipped, lacks %q:\n%s", want, debugSweepRule)
		}
	}
}
