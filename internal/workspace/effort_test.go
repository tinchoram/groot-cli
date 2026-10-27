package workspace

import (
	"context"
	"testing"

	"github.com/tinchoram/groot-cli/internal/catalog"
)

// An agent's own effort reaches its model calls; without one the call carries
// none and the provider's configured value applies.
func TestAgentEffortOverridesProviderPerCall(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t, text("one"), text("two"))
	if err := catalog.Save(f.home, catalog.AgentDef{Name: "reader", Description: "reads files", Tools: []string{"read_file", "recall"}, Effort: "high"}, "You read files carefully."); err != nil {
		t.Fatal(err)
	}
	if err := f.ws.reload(); err != nil {
		t.Fatal(err)
	}
	if err := f.ws.SelectAgent("reader"); err != nil {
		t.Fatal(err)
	}
	if _, err := f.ws.Chat().Send(ctx, "hi"); err != nil {
		t.Fatal(err)
	}
	if got := f.mock.Calls[0].Effort; got != "high" {
		t.Fatalf("agent effort not passed to the call: %q", got)
	}
}

func TestAgentInfoShowsEffort(t *testing.T) {
	f := newFixture(t)
	if err := catalog.Save(f.home, catalog.AgentDef{Name: "reader", Description: "reads files", Tools: []string{"read_file"}, Effort: "max"}, "You read."); err != nil {
		t.Fatal(err)
	}
	if err := f.ws.reload(); err != nil {
		t.Fatal(err)
	}
	info, err := f.ws.Agent("reader")
	if err != nil {
		t.Fatal(err)
	}
	if info.Effort != "max" {
		t.Fatalf("AgentInfo.Effort = %q, want max", info.Effort)
	}
}
