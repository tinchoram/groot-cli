package workspace

import (
	"slices"
	"testing"

	"github.com/tinchoram/groot-cli/internal/catalog"
	"github.com/tinchoram/groot-cli/internal/space"
	"github.com/tinchoram/groot-cli/library"
)

// A fresh home holds root and groot only: debug is installed from the library
// on request, like any other agent.
func TestFreshBootstrapDoesNotCreateDebug(t *testing.T) {
	home, userHome := t.TempDir(), t.TempDir()
	if _, err := Bootstrap(home, userHome, t0); err != nil {
		t.Fatal(err)
	}
	if catalog.Exists(home, debugAgent) {
		t.Fatal("a fresh home must not create the debug agent")
	}
	sp, _ := space.Open(home + "/spaces.yaml")
	root, _ := sp.Get("root")
	if !slices.Equal(root.Agents, []string{managedAgent}) {
		t.Fatalf("root agents = %v, want only groot", root.Agents)
	}
}

// The debug definition is the embedded library file, so the library entry and
// the tools that groot keeps current can never drift apart.
func TestDebugDefComesFromTheLibraryFile(t *testing.T) {
	raw, err := library.Agents.ReadFile("agents/debug.md")
	if err != nil {
		t.Fatal(err)
	}
	want, err := catalog.ParseAgentFile(raw)
	if err != nil {
		t.Fatal(err)
	}
	got := debugDef()
	if got.Name != want.Name || got.Description != want.Description || !slices.Equal(got.Tools, want.Tools) || got.Prompt != want.Prompt {
		t.Fatalf("debugDef() = %+v, library file = %+v", got, want)
	}
	for _, need := range []string{"feedback_report", "feedback_list", "feedback_update"} {
		if !slices.Contains(got.Tools, need) {
			t.Fatalf("debug lacks %s: %v", need, got.Tools)
		}
	}
}

func TestDebugIsManagedLikeGroot(t *testing.T) {
	f := newFixture(t, text("ok"))
	if _, err := f.ws.NewAgent(debugAgent, "x"); err == nil {
		t.Fatal("debug cannot be created again")
	}
	if _, err := f.ws.UpgradeAgents(debugAgent); err == nil {
		t.Fatal("debug is not upgraded by hand")
	}
}
