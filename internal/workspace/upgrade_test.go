package workspace

import (
	"slices"
	"strings"
	"testing"

	"github.com/tinchoram/groot-cli/internal/catalog"
)

// oldGroot writes a groot.md from an older version: only the admin tools, and
// the user's own prompt.
func oldGroot(t *testing.T, home string) {
	t.Helper()
	def := catalog.AgentDef{Name: managedAgent, Description: "old groot", Tools: []string{"list_spaces", "recall"}}
	if err := catalog.Save(home, def, "my own prompt"); err != nil {
		t.Fatal(err)
	}
}

func TestBootstrapAddsManagedToolsToAnOldGrootAndKeepsItsPrompt(t *testing.T) {
	home, userHome := t.TempDir(), t.TempDir()
	oldGroot(t, home)
	if _, err := Bootstrap(home, userHome, t0); err != nil {
		t.Fatal(err)
	}
	defs, err := catalog.Load(home)
	if err != nil {
		t.Fatal(err)
	}
	var groot catalog.AgentDef
	for _, d := range defs {
		if d.Name == managedAgent {
			groot = d
		}
	}
	if groot.Prompt != "my own prompt" {
		t.Fatalf("the prompt must be kept, got %q", groot.Prompt)
	}
	if !slices.Equal(groot.Tools[:2], []string{"list_spaces", "recall"}) {
		t.Fatalf("existing tools keep their order: %v", groot.Tools)
	}
	for _, want := range catalog.ManagedTools() {
		if !slices.Contains(groot.Tools, want) {
			t.Fatalf("groot lacks %s after bootstrap: %v", want, groot.Tools)
		}
	}
	// The full managed set, not only the base tools: admin tools added in
	// later versions (list_models, grant_tool...) must reach an old file too.
	for _, want := range grootDef().Tools {
		if !slices.Contains(groot.Tools, want) {
			t.Fatalf("groot lacks the managed admin tool %s after bootstrap: %v", want, groot.Tools)
		}
	}
}

func TestBootstrapAddsManagedToolsToAnOldDebug(t *testing.T) {
	home, userHome := t.TempDir(), t.TempDir()
	def := catalog.AgentDef{Name: debugAgent, Description: "old debug", Tools: []string{"feedback_list"}}
	if err := catalog.Save(home, def, "my debug prompt"); err != nil {
		t.Fatal(err)
	}
	if _, err := Bootstrap(home, userHome, t0); err != nil {
		t.Fatal(err)
	}
	defs, _ := catalog.Load(home)
	for _, d := range defs {
		if d.Name != debugAgent {
			continue
		}
		if d.Prompt != "my debug prompt" {
			t.Fatalf("the prompt must be kept, got %q", d.Prompt)
		}
		for _, want := range debugDef().Tools {
			if !slices.Contains(d.Tools, want) {
				t.Fatalf("debug lacks %s after bootstrap: %v", want, d.Tools)
			}
		}
		return
	}
	t.Fatal("debug not found")
}

func TestBootstrapUpgradeIsIdempotent(t *testing.T) {
	home, userHome := t.TempDir(), t.TempDir()
	oldGroot(t, home)
	if _, err := Bootstrap(home, userHome, t0); err != nil {
		t.Fatal(err)
	}
	defs, _ := catalog.Load(home)
	before := len(defs[0].Tools)
	if _, err := Bootstrap(home, userHome, t0); err != nil {
		t.Fatal(err)
	}
	defs, _ = catalog.Load(home)
	if after := len(defs[0].Tools); after != before {
		t.Fatalf("second bootstrap changed the tool count %d -> %d", before, after)
	}
}

func TestUpgradeAgentsAddsTheManagedToolsByName(t *testing.T) {
	f := newFixture(t, text("ok"))
	if err := catalog.Save(f.home, catalog.AgentDef{Name: "developer", Tools: []string{"bash"}}, "dev"); err != nil {
		t.Fatal(err)
	}
	if err := f.ws.reload(); err != nil {
		t.Fatal(err)
	}
	report, err := f.ws.UpgradeAgents("developer")
	if err != nil {
		t.Fatal(err)
	}
	if len(report) != 1 || !strings.Contains(report[0], "developer: added") || !strings.Contains(report[0], "send_message") {
		t.Fatalf("report = %v", report)
	}
	if again, _ := f.ws.UpgradeAgents("developer"); len(again) != 1 || !strings.Contains(again[0], "up to date") {
		t.Fatalf("second upgrade = %v", again)
	}
	defs, _ := catalog.Load(f.home)
	for _, d := range defs {
		if d.Name == "developer" && (!slices.Contains(d.Tools, "bash") || !slices.Contains(d.Tools, "task_open")) {
			t.Fatalf("developer tools = %v", d.Tools)
		}
	}
}

func TestUpgradeAgentsRefusesGrootAndUnknownNames(t *testing.T) {
	f := newFixture(t, text("ok"))
	if _, err := f.ws.UpgradeAgents(managedAgent); err == nil {
		t.Fatal("groot is not upgraded by hand")
	}
	if _, err := f.ws.UpgradeAgents("nobody"); err == nil {
		t.Fatal("an unknown agent must be reported")
	}
}

func TestUpgradeAllSkipsGroot(t *testing.T) {
	f := newFixture(t, text("ok"))
	report, err := f.ws.UpgradeAgents("")
	if err != nil {
		t.Fatal(err)
	}
	for _, line := range report {
		if strings.HasPrefix(line, managedAgent+":") {
			t.Fatalf("groot must not appear in an all-agents upgrade: %v", report)
		}
	}
}
