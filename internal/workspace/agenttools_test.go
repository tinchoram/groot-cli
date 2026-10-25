package workspace

import (
	"context"
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

// catalogTools returns the tools the catalog lists for an agent.
func catalogTools(t *testing.T, f *fixture, name string) []string {
	t.Helper()
	for _, a := range f.ws.CatalogAgents() {
		if a.Name == name {
			return a.Tools
		}
	}
	t.Fatalf("agent %q not in the catalog", name)
	return nil
}

func TestEditAgentToolsRefreshesTheCatalogCache(t *testing.T) {
	f := newFixture(t, text("ok"))

	if err := f.ws.EditAgentTools("reader", []string{"bash"}, nil); err != nil {
		t.Fatal(err)
	}
	if tools := catalogTools(t, f, "reader"); !slices.Contains(tools, "bash") {
		t.Fatalf("CatalogAgents after add must list bash, got %v", tools)
	}
	if err := f.ws.EditAgentTools("reader", nil, []string{"bash"}); err != nil {
		t.Fatal(err)
	}
	if tools := catalogTools(t, f, "reader"); slices.Contains(tools, "bash") {
		t.Fatalf("CatalogAgents after remove must not list bash, got %v", tools)
	}
}

func TestEditAgentToolsReachesOpenConversations(t *testing.T) {
	f := newFixture(t, text("ok"), text("still here"))
	if _, err := f.ws.Chat().Send(context.Background(), "hi"); err != nil {
		t.Fatal(err)
	}
	conv := f.ws.Chat().(*Conversation)
	session, messages := conv.SessionID(), conv.MessageCount()

	if err := f.ws.EditAgentTools("reader", []string{"bash"}, nil); err != nil {
		t.Fatal(err)
	}
	if _, ok := conv.Agent.Tools.Get("bash"); !ok {
		t.Fatal("an open conversation must get a tool added to its agent")
	}
	if f.ws.Chat().(*Conversation) != conv {
		t.Fatal("the open conversation must be kept, not reopened")
	}
	if conv.SessionID() != session || conv.MessageCount() != messages {
		t.Fatalf("the session must survive the edit: id %q->%q, messages %d->%d",
			session, conv.SessionID(), messages, conv.MessageCount())
	}
	if _, err := f.ws.Chat().Send(context.Background(), "again"); err != nil {
		t.Fatalf("the conversation must keep working: %v", err)
	}

	if err := f.ws.EditAgentTools("reader", nil, []string{"bash"}); err != nil {
		t.Fatal(err)
	}
	if _, ok := conv.Agent.Tools.Get("bash"); ok {
		t.Fatal("a tool removed from the agent must leave its open conversation")
	}
	if _, ok := conv.Agent.Tools.Get("read_file"); !ok {
		t.Fatal("the tools the agent keeps must stay in the open conversation")
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
