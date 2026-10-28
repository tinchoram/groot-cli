package workspace

import (
	"context"
	"strings"
	"testing"
)

// addCatalogAgent adds an agent to the catalog and assigns it to the default space.
func addCatalogAgent(t *testing.T, f *fixture, name string) {
	t.Helper()
	saveAgent(t, f, name, "helps in the space", []string{"read_file"})
	if err := f.ws.reload(); err != nil {
		t.Fatal(err)
	}
	addAgentToDefault(t, f, name)
}

func TestSpaceWorkModeTurnsOnEveryAssignedAgentAndLaterOnesInherit(t *testing.T) {
	f := newFixture(t, text("ok"))
	addCatalogAgent(t, f, "writer")

	if err := f.ws.SetSpaceWorkMode("default", true); err != nil {
		t.Fatal(err)
	}
	modes := f.ws.WorkModes()
	if !modes["default/reader"] || !modes["default/writer"] {
		t.Fatalf("work modes = %v, want reader and writer on", modes)
	}

	addCatalogAgent(t, f, "scribe")
	if err := f.ws.SelectAgent("scribe"); err != nil {
		t.Fatal(err)
	}
	if !f.ws.Chat().WorkMode() {
		t.Fatal("a conversation opened later in the space inherits work mode")
	}
}

func TestSpaceWorkModeOffClearsEveryAgent(t *testing.T) {
	f := newFixture(t, text("ok"))
	addCatalogAgent(t, f, "writer")
	if err := f.ws.SetSpaceWorkMode("default", true); err != nil {
		t.Fatal(err)
	}

	if err := f.ws.SetSpaceWorkMode("default", false); err != nil {
		t.Fatal(err)
	}
	if modes := f.ws.WorkModes(); len(modes) != 0 {
		t.Fatalf("work modes after off = %v, want none", modes)
	}

	addCatalogAgent(t, f, "scribe")
	if err := f.ws.SelectAgent("scribe"); err != nil {
		t.Fatal(err)
	}
	if f.ws.Chat().WorkMode() {
		t.Fatal("a conversation opened after off does not inherit work mode")
	}
}

func TestSpaceWorkModeRejectsAnUnknownSpace(t *testing.T) {
	f := newFixture(t, text("ok"))
	if err := f.ws.SetSpaceWorkMode("nowhere", true); err == nil {
		t.Fatal("a space that does not exist cannot turn work mode on")
	}
}

func TestDeliverTurnsOffOnlyTheDeliveringAgent(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t,
		classifyStep("tu_1", "medium", true, "two files"),
		toolStep("tu_2", "task_open", map[string]string{"goal": "ship", "scope": "space", "done_when": "tests pass"}),
		toolStep("tu_3", "write_file", map[string]string{"path": "a.txt", "content": "x"}),
		toolStep("tu_4", "bash", map[string]string{"command": "echo hi"}),
		toolStep("tu_5", "task_advance", map[string]string{"phase": "verify"}),
		toolStep("tu_6", "task_advance", map[string]string{"phase": "deliver", "evidence": "changed a.txt; risk: none known"}),
		text("delivered"),
	)
	h := wireTriggers(f)
	flowAgent(t, f)
	addCatalogAgent(t, f, "writer")
	if err := f.ws.SetSpaceWorkMode("default", true); err != nil {
		t.Fatal(err)
	}

	if _, err := f.ws.Chat().Send(ctx, "build it"); err != nil {
		t.Fatal(err)
	}
	modes := f.ws.WorkModes()
	if modes["default/reader"] {
		t.Fatal("the delivering agent turns off")
	}
	if !modes["default/writer"] {
		t.Fatal("the other agent of the space stays on")
	}
	if got := awaitWarning(t, h.warnings, "/work on turns it back on"); !strings.Contains(got, "delivered for review") {
		t.Fatalf("warning = %q, want the reason named", got)
	}

	if err := f.ws.SelectAgent("scribe"); err == nil {
		t.Fatal("scribe is not assigned yet")
	}
	addCatalogAgent(t, f, "scribe")
	if err := f.ws.SelectAgent("scribe"); err != nil {
		t.Fatal(err)
	}
	if !f.ws.Chat().WorkMode() {
		t.Fatal("the next conversation opened in the space still inherits on")
	}
}
