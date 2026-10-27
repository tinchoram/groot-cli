package workspace

import (
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/tinchoram/groot-cli/internal/api"
	"github.com/tinchoram/groot-cli/internal/catalog"
	"github.com/tinchoram/groot-cli/internal/memory"
	"github.com/tinchoram/groot-cli/internal/provider"
)

func openMemoryForTest(t *testing.T) *memory.SQLite {
	t.Helper()
	st, err := memory.OpenSQLite(filepath.Join(t.TempDir(), "groot.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	return st
}

// rememberStep is a model answer that calls the remember tool.
func rememberStep(id, kind, text string) provider.Step {
	in, _ := json.Marshal(map[string]string{"kind": kind, "text": text})
	return provider.Step{Response: api.Response{
		Content:    []api.Block{{Type: api.BlockToolUse, ID: id, Name: "remember", Input: in}},
		StopReason: api.StopToolUse,
	}}
}

// newScribe opens a fixture on an agent that may remember and recall, with a
// fixed clock so the system prompts of two turns can be compared byte for byte.
func newScribe(t *testing.T, f *fixture) {
	t.Helper()
	if err := catalog.Save(f.home, catalog.AgentDef{Name: "scribe", Description: "keeps notes", Tools: []string{"remember", "recall"}}, "You keep notes."); err != nil {
		t.Fatal(err)
	}
	if err := f.ws.reload(); err != nil {
		t.Fatal(err)
	}
	if err := f.spaces.Assign("default", "scribe"); err != nil {
		t.Fatal(err)
	}
	f.ws.opts.Now = func() time.Time { return t0 }
	if err := f.ws.SelectAgent("scribe"); err != nil {
		t.Fatal(err)
	}
}

// lastUserText is the text of the last message of a request, which is where
// the turn's blocks are.
func lastUserText(req provider.Request) string {
	last := req.Messages[len(req.Messages)-1]
	var sb strings.Builder
	for _, b := range last.Content {
		sb.WriteString(b.Text)
		sb.WriteString(b.Content)
	}
	return sb.String()
}

func TestRememberLeavesTheSystemPromptUnchanged(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t, rememberStep("tu_1", "runbook", "drain the node first"), text("saved"), text("what next"))
	newScribe(t, f)
	conv := f.ws.Chat()
	if _, err := conv.Send(ctx, "note the drain rule"); err != nil {
		t.Fatal(err)
	}
	if _, err := conv.Send(ctx, "what next?"); err != nil {
		t.Fatal(err)
	}
	before, after := f.mock.Calls[0].System, f.mock.Calls[2].System
	if before != after {
		t.Fatalf("the system prompt changed after a remember:\nbefore:\n%s\nafter:\n%s", before, after)
	}
	if strings.Contains(after, "drain the node first") {
		t.Fatal("memory must not be part of the system prompt")
	}
	next := lastUserText(f.mock.Calls[2])
	if !strings.Contains(next, "<memory-update>") || !strings.Contains(next, "drain the node first") {
		t.Fatalf("the next turn must carry the memory delta, got:\n%s", next)
	}
}

func TestFirstTurnCarriesTheFullMemoryBlockOnce(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t, text("ok"), text("again"))
	newScribe(t, f)
	st, err := f.ws.storesFor(f.dir)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.mem.Remember(ctx, memory.Entry{Kind: memory.KindRunbook, Scope: memory.ScopeAgent, Agent: "scribe", Text: "drain the node first"}); err != nil {
		t.Fatal(err)
	}
	conv := f.ws.Chat()
	if _, err := conv.Send(ctx, "hi"); err != nil {
		t.Fatal(err)
	}
	first := lastUserText(f.mock.Calls[0])
	if !strings.Contains(first, "<memory>") || !strings.Contains(first, "- [runbook] drain the node first") || !strings.Contains(first, "hi") {
		t.Fatalf("the first turn must carry the full memory block before the prompt, got:\n%s", first)
	}
	if _, err := conv.Send(ctx, "again"); err != nil {
		t.Fatal(err)
	}
	if second := lastUserText(f.mock.Calls[1]); strings.Contains(second, "<memory") {
		t.Fatalf("an unchanged memory is not sent again, got:\n%s", second)
	}
}

func TestGlobalMemoryReachesOnlyReadersThatMayReadIt(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t, text("a"), text("b"))
	// The fixture has no user home, so its space is the root space and the global
	// entry sits in the database the space reads.
	root, err := f.ws.storesFor(f.dir)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := root.mem.Remember(ctx, memory.Entry{Kind: memory.KindRunbook, Scope: memory.ScopeGlobal, Agent: "groot", Text: "rotate the root key"}); err != nil {
		t.Fatal(err)
	}
	if err := catalog.Save(f.home, catalog.AgentDef{Name: "ops", Description: "ops", Tools: []string{"recall"}, GlobalMemory: true}, "You run operations."); err != nil {
		t.Fatal(err)
	}
	if err := f.ws.reload(); err != nil {
		t.Fatal(err)
	}
	if err := f.spaces.Assign("default", "ops"); err != nil {
		t.Fatal(err)
	}

	if err := f.ws.SelectAgent("reader"); err != nil {
		t.Fatal(err)
	}
	if _, err := f.ws.Chat().Send(ctx, "hi"); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(lastUserText(f.mock.Calls[0]), "rotate the root key") {
		t.Fatal("an agent without global memory must not see global entries")
	}

	if err := f.ws.SelectAgent("ops"); err != nil {
		t.Fatal(err)
	}
	if _, err := f.ws.Chat().Send(ctx, "hi"); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(lastUserText(f.mock.Calls[1]), "[runbook, global] rotate the root key") {
		t.Fatalf("an enabled agent must see the global entry tagged global, got:\n%s", lastUserText(f.mock.Calls[1]))
	}
}

func TestReadsGlobalMemoryIsGrootOrEnabled(t *testing.T) {
	tests := []struct {
		name  string
		agent string
		def   catalog.AgentDef
		want  bool
	}{
		{name: "groot always reads global", agent: managedAgent, def: catalog.AgentDef{}, want: true},
		{name: "enabled agent reads global", agent: "payments", def: catalog.AgentDef{GlobalMemory: true}, want: true},
		{name: "other agents do not", agent: "payments", def: catalog.AgentDef{}, want: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := readsGlobal(tt.agent, tt.def); got != tt.want {
				t.Fatalf("readsGlobal = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestSpaceMemoryIsLayeredOnlyOutsideTheRootSpace(t *testing.T) {
	local := openMemoryForTest(t)
	root := openMemoryForTest(t)

	if got := spaceMemoryStore(true, local, root, false); got != memory.Store(local) {
		t.Fatal("the root space is the root database: no layering")
	}
	layered, ok := spaceMemoryStore(false, local, root, true).(*memory.Layered)
	if !ok {
		t.Fatal("a project space must read and write global memory through the layered store")
	}
	if !layered.GlobalWrite {
		t.Fatal("the layered store must carry the write right of its agent")
	}
}
