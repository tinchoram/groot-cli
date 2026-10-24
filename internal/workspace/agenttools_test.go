package workspace

import (
	"slices"
	"testing"

	"github.com/tinchoram/groot-cli/internal/catalog"
)

func TestEditAgentToolsAddsAndRemovesAndSaves(t *testing.T) {
	f := newFixture(t, text("ok"))

	if err := f.ws.EditAgentTools("reader", []string{"bash"}, nil); err != nil {
		t.Fatal(err)
	}
	tools, err := f.ws.AgentTools("reader")
	if err != nil || !slices.Contains(tools, "bash") {
		t.Fatalf("after add: %v (%v)", tools, err)
	}
	if err := f.ws.EditAgentTools("reader", nil, []string{"bash"}); err != nil {
		t.Fatal(err)
	}
	if tools, _ := f.ws.AgentTools("reader"); slices.Contains(tools, "bash") {
		t.Fatalf("after remove: %v", tools)
	}
	if err := f.ws.EditAgentTools("reader", []string{"teleport"}, nil); err == nil {
		t.Fatal("an unknown tool must be refused")
	}
	if err := f.ws.EditAgentTools("nobody", []string{"bash"}, nil); err == nil {
		t.Fatal("an unknown agent must be refused")
	}
}

func TestManagedAgentsKeepTheirRequiredTools(t *testing.T) {
	f := newFixture(t, text("ok"))
	if err := catalog.Save(f.home, grootDef(), grootPrompt); err != nil {
		t.Fatal(err)
	}

	if err := f.ws.EditAgentTools(managedAgent, nil, []string{"create_space"}); err == nil {
		t.Fatal("groot must not lose an admin tool")
	}
	if err := f.ws.EditAgentTools(managedAgent, nil, []string{"recall"}); err == nil {
		t.Fatal("groot must not lose a managed tool")
	}
	if err := f.ws.EditAgentTools(managedAgent, []string{"bash"}, nil); err != nil {
		t.Fatalf("groot may gain tools: %v", err)
	}
}
