package workspace

import (
	"context"
	"testing"

	"github.com/tinchoram/groot-cli/internal/catalog"
)

func TestSetAgentGlobalMemorySavesAndReachesOpenConversations(t *testing.T) {
	f := newFixture(t, text("ok"), text("still here"))
	if _, err := f.ws.Chat().Send(context.Background(), "hi"); err != nil {
		t.Fatal(err)
	}
	conv := f.ws.Chat().(*Conversation)
	if conv.mem.reader().Global {
		t.Fatal("a plain agent starts without global memory")
	}

	if err := f.ws.SetAgentGlobalMemory("reader", true); err != nil {
		t.Fatal(err)
	}
	d, err := f.ws.agentDef("reader")
	if err != nil || !d.GlobalMemory {
		t.Fatalf("the flag must be saved in the agent file: %+v (%v)", d, err)
	}
	if !conv.mem.reader().Global {
		t.Fatal("the open conversation must read global memory right away")
	}
	if f.ws.Chat().(*Conversation) != conv {
		t.Fatal("the open conversation must be kept, not reopened")
	}
	if _, err := f.ws.Chat().Send(context.Background(), "again"); err != nil {
		t.Fatalf("the conversation must keep working: %v", err)
	}

	if err := f.ws.SetAgentGlobalMemory("reader", false); err != nil {
		t.Fatal(err)
	}
	if conv.mem.reader().Global {
		t.Fatal("revoking must take effect in the open conversation too")
	}
	for _, a := range f.ws.CatalogAgents() {
		if a.Name == "reader" && a.GlobalMemory {
			t.Fatal("the catalog must show the agent without global memory")
		}
	}
}

func TestGrootAlwaysReadsGlobalMemory(t *testing.T) {
	f := newFixture(t, text("ok"))
	if err := catalog.Save(f.home, grootDef(), grootPrompt); err != nil {
		t.Fatal(err)
	}
	if err := f.ws.SetAgentGlobalMemory(managedAgent, false); err == nil {
		t.Fatal("groot must not lose global memory")
	}
	if err := f.ws.SetAgentGlobalMemory(managedAgent, true); err != nil {
		t.Fatalf("enabling it for groot is a no-op: %v", err)
	}
}
