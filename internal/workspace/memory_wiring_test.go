package workspace

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tinchoram/groot-cli/internal/catalog"
	"github.com/tinchoram/groot-cli/internal/memory"
	"github.com/tinchoram/groot-cli/internal/tool"
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

// newMemoryConversation builds a conversation whose prompt is rebuilt from the
// store the way open builds it.
func newMemoryConversation(t *testing.T, store memory.Store, mem *memoryState) *Conversation {
	t.Helper()
	conv := &Conversation{store: store, name: mem.agent, baseSystem: "base prompt"}
	conv.rebuildSystem = func(ctx context.Context) (string, error) {
		entries, err := store.Preamble(ctx, mem.reader(), mem.budget)
		if err != nil {
			return "", err
		}
		return "base prompt\n" + memory.FormatPreamble(entries), nil
	}
	mem.changed = func() { _ = conv.refreshSystem(context.Background()) }
	return conv
}

func TestRememberRefreshesThePromptForTheNextTurn(t *testing.T) {
	ctx := context.Background()
	store := openMemoryForTest(t)
	mem := &memoryState{store: store, agent: "payments", budget: memory.Budget{Tokens: 1500, MaxSummaries: 3}}
	conv := newMemoryConversation(t, store, mem)
	if strings.Contains(conv.base(), "drain the node first") {
		t.Fatal("the prompt starts without the entry")
	}

	reg := tool.NewRemember(mem.access())
	out, isErr := reg.Execute(ctx, `{"kind":"runbook","text":"drain the node first"}`)
	if isErr {
		t.Fatalf("remember failed: %s", out)
	}
	if !strings.Contains(conv.base(), "drain the node first") {
		t.Fatalf("the next turn's prompt must carry the entry, got:\n%s", conv.base())
	}
}

func TestGlobalMemoryReachesOnlyReadersThatMayReadIt(t *testing.T) {
	ctx := context.Background()
	store := openMemoryForTest(t)
	if _, err := store.Remember(ctx, memory.Entry{Kind: memory.KindRunbook, Scope: memory.ScopeGlobal, Agent: "groot", Text: "rotate the root key"}); err != nil {
		t.Fatal(err)
	}
	mem := &memoryState{store: store, agent: "payments", budget: memory.Budget{Tokens: 1500, MaxSummaries: 3}}
	conv := newMemoryConversation(t, store, mem)

	if strings.Contains(conv.base(), "rotate the root key") {
		t.Fatal("an agent without global memory must not see global entries")
	}
	mem.setGlobal(true)
	conv.refreshSystem(ctx)
	if !strings.Contains(conv.base(), "[runbook, global] rotate the root key") {
		t.Fatalf("an enabled agent must see the global entry tagged global, got:\n%s", conv.base())
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
