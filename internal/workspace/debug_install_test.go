package workspace

import (
	"context"
	"slices"
	"strings"
	"testing"

	"github.com/tinchoram/groot-cli/internal/catalog"
	"github.com/tinchoram/groot-cli/internal/space"
)

// debug is installed from the library like any agent, into a space or only into
// the catalog. It is not refused as managed.
func TestInstallDebugFromTheLibrary(t *testing.T) {
	ws, home, _, _ := adminFixture(t)
	if err := ws.InstallLibraryAgent("debug", "root", false); err != nil {
		t.Fatalf("install debug: %v", err)
	}
	defs, _ := catalog.Load(home)
	d := findDef(defs, debugAgent)
	if d == nil || d.Collection != "@tinchoram" || !slices.Contains(d.Tools, "feedback_report") {
		t.Fatalf("debug not installed from the library: %+v", d)
	}
	sp, _ := space.Open(home + "/spaces.yaml")
	root, _ := sp.Get("root")
	if !slices.Contains(root.Agents, debugAgent) {
		t.Fatalf("install with a space must assign debug there: %v", root.Agents)
	}
	if err := ws.InstallLibraryAgent("debug", "", false); err == nil || !strings.Contains(err.Error(), "already exists") {
		t.Fatalf("a second install without overwrite must refuse: %v", err)
	}
}

// An installed debug gets the release sweep and the reply rule when its
// conversation opens, like the one bootstrap used to create.
func TestInstalledDebugGetsTheSweepRuleAtOpen(t *testing.T) {
	ws, _, _, mock := adminFixture(t, text("ok"))
	if err := ws.InstallLibraryAgent("debug", "root", false); err != nil {
		t.Fatal(err)
	}
	if err := ws.SelectAgent(debugAgent); err != nil {
		t.Fatal(err)
	}
	if _, err := ws.Chat().Send(context.Background(), "hi"); err != nil {
		t.Fatal(err)
	}
	sys := mock.Calls[0].System
	for _, want := range []string{"Release sweep", "Confirmations are off", "You are Debug"} {
		if !strings.Contains(sys, want) {
			t.Fatalf("debug's system prompt lacks %q:\n%s", want, sys)
		}
	}
}

// Deleting an installed debug keeps the backlog files: they live outside the
// catalog. Groot stays protected.
func TestInstalledDebugCanBeDeletedButGrootCannot(t *testing.T) {
	ws, home, _, _ := adminFixture(t)
	if err := ws.InstallLibraryAgent("debug", "root", false); err != nil {
		t.Fatal(err)
	}
	if _, err := ws.DeleteAgentWithConfirm(debugAgent, true); err != nil {
		t.Fatalf("delete installed debug: %v", err)
	}
	if catalog.Exists(home, debugAgent) {
		t.Fatal("debug must be gone from the catalog")
	}
	if _, err := ws.DeleteAgent(managedAgent); err == nil {
		t.Fatal("groot must never be deleted")
	}
}

// An installed debug keeps its name: it can be edited, not renamed.
func TestInstalledDebugCannotBeRenamed(t *testing.T) {
	ws, _, _, _ := adminFixture(t)
	if err := ws.InstallLibraryAgent("debug", "root", false); err != nil {
		t.Fatal(err)
	}
	if _, err := ws.EditAgent(AgentEdit{Name: debugAgent, NewName: "boss"}); err == nil {
		t.Fatal("renaming debug must be refused")
	}
}
