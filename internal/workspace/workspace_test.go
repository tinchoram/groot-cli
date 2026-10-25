package workspace

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/tinchoram/groot-cli/internal/api"
	"github.com/tinchoram/groot-cli/internal/catalog"
	"github.com/tinchoram/groot-cli/internal/config"
	"github.com/tinchoram/groot-cli/internal/gate"
	"github.com/tinchoram/groot-cli/internal/memory"
	"github.com/tinchoram/groot-cli/internal/provider"
	"github.com/tinchoram/groot-cli/internal/space"
)

var t0 = time.Date(2026, 10, 24, 9, 0, 0, 0, time.UTC)

type fixture struct {
	ws     *Workspace
	home   string
	dir    string // space working directory
	mock   *provider.Mock
	spaces *space.Store
}

func text(s string) provider.Step {
	return provider.Step{Response: api.Response{Content: []api.Block{{Type: api.BlockText, Text: s}}, StopReason: api.StopEndTurn}}
}

func readFileCall(id, path string) provider.Step {
	in, _ := json.Marshal(map[string]string{"path": path})
	return provider.Step{Response: api.Response{
		Content:    []api.Block{{Type: api.BlockToolUse, ID: id, Name: "read_file", Input: in}},
		StopReason: api.StopToolUse,
	}}
}

// newFixture builds a workspace over a temporary home and space directory.
// The space has one agent, "reader", that may only read files.
func newFixture(t *testing.T, steps ...provider.Step) *fixture {
	t.Helper()
	home := t.TempDir()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "notes.txt"), []byte("hello from space"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "AGENTS.md"), []byte("Cluster: prod-eu"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := catalog.Save(home, catalog.AgentDef{Name: "reader", Description: "reads files", Tools: []string{"read_file", "recall"}}, "You read files carefully."); err != nil {
		t.Fatal(err)
	}
	sp, err := space.Open(filepath.Join(home, "spaces.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if err := sp.Add("default", dir, t0); err != nil {
		t.Fatal(err)
	}
	if err := sp.Assign("default", "reader"); err != nil {
		t.Fatal(err)
	}
	if err := sp.Use("default"); err != nil {
		t.Fatal(err)
	}
	mock := provider.NewMock(steps...)
	ws, err := New(Options{
		Home:        home,
		Config:      config.Default(),
		Spaces:      sp,
		Gate:        gate.AutoApprove{},
		NewProvider: func(string) provider.Provider { return mock },
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(func() { _ = ws.Close(context.Background()) })
	return &fixture{ws: ws, home: home, dir: dir, mock: mock, spaces: sp}
}

func TestChatRunsToolsInsideSpaceDirectory(t *testing.T) {
	f := newFixture(t, readFileCall("tu_1", "notes.txt"), text("done"))
	if err := f.ws.SelectAgent("reader"); err != nil {
		t.Fatal(err)
	}
	reply, err := f.ws.Chat().Send(context.Background(), "read the notes")
	if err != nil || reply != "done" {
		t.Fatalf("Send = %q, %v", reply, err)
	}
	last := f.mock.Calls[1].Messages[len(f.mock.Calls[1].Messages)-1].Content[0]
	if last.Type != api.BlockToolResult || !strings.Contains(last.Content, "hello from space") {
		t.Fatalf("tool did not read from the space directory: %+v", last)
	}
}

func TestSystemPromptCombinesAgentPromptAndProjectContext(t *testing.T) {
	f := newFixture(t, text("ok"))
	_ = f.ws.SelectAgent("reader")
	if _, err := f.ws.Chat().Send(context.Background(), "hi"); err != nil {
		t.Fatal(err)
	}
	sys := f.mock.Calls[0].System
	if !strings.Contains(sys, "You read files carefully.") || !strings.Contains(sys, "Cluster: prod-eu") {
		t.Fatalf("system prompt missing parts:\n%s", sys)
	}
}

func TestEachAgentKeepsItsOwnConversation(t *testing.T) {
	f := newFixture(t, text("first reply"))
	if err := catalog.Save(f.home, catalog.AgentDef{Name: "scribe", Description: "writes notes", Tools: []string{"recall"}}, ""); err != nil {
		t.Fatal(err)
	}
	if err := f.ws.reload(); err != nil {
		t.Fatal(err)
	}
	if err := f.spaces.Assign("default", "scribe"); err != nil {
		t.Fatal(err)
	}
	if err := f.ws.SelectAgent("reader"); err != nil {
		t.Fatal(err)
	}
	if _, err := f.ws.Chat().Send(context.Background(), "hello"); err != nil {
		t.Fatal(err)
	}
	if err := f.ws.SelectAgent("scribe"); err != nil {
		t.Fatal(err)
	}
	if n := f.ws.Chat().MessageCount(); n != 0 {
		t.Fatalf("scribe should start empty, has %d messages", n)
	}
	if err := f.ws.SelectAgent("reader"); err != nil {
		t.Fatal(err)
	}
	if n := f.ws.Chat().MessageCount(); n != 2 {
		t.Fatalf("reader history must survive switching, has %d messages", n)
	}
}

func TestSelectAgentRequiresAssignment(t *testing.T) {
	f := newFixture(t)
	if err := f.ws.SelectAgent("ghost"); err == nil {
		t.Fatal("selecting an agent not assigned to the space must fail")
	}
}

func TestNoAgentAssignedGivesIdleChatError(t *testing.T) {
	f := newFixture(t)
	if err := f.ws.UnassignAgent("reader"); err != nil {
		t.Fatal(err)
	}
	_, err := f.ws.Chat().Send(context.Background(), "hi")
	if err == nil || !strings.Contains(err.Error(), "no agent") {
		t.Fatalf("expected a no-agent error, got %v", err)
	}
}

func TestUseSpaceSelectsFirstAssignedAgent(t *testing.T) {
	f := newFixture(t)
	other := t.TempDir()
	if err := f.ws.NewSpace("second", other); err != nil {
		t.Fatal(err)
	}
	if err := f.ws.UseSpace("second"); err != nil {
		t.Fatal(err)
	}
	if f.ws.ActiveAgent() != "" {
		t.Fatalf("empty space should have no active agent, got %q", f.ws.ActiveAgent())
	}
	if err := f.ws.UseSpace("default"); err != nil {
		t.Fatal(err)
	}
	if f.ws.ActiveAgent() != "reader" {
		t.Fatalf("UseSpace should pick the first agent, got %q", f.ws.ActiveAgent())
	}
}

func TestUnassigningActiveAgentMovesSelection(t *testing.T) {
	f := newFixture(t)
	if err := catalog.Save(f.home, catalog.AgentDef{Name: "scribe", Description: "x", Tools: []string{"recall"}}, ""); err != nil {
		t.Fatal(err)
	}
	_ = f.ws.reload()
	_ = f.ws.AssignAgent("scribe")
	_ = f.ws.SelectAgent("reader")
	if err := f.ws.UnassignAgent("reader"); err != nil {
		t.Fatal(err)
	}
	if f.ws.ActiveAgent() != "scribe" {
		t.Fatalf("selection should move to scribe, got %q", f.ws.ActiveAgent())
	}
}

func TestCloseWritesSessionSummaryToSpaceMemory(t *testing.T) {
	f := newFixture(t, text("answer"), text("Asked about notes. Read one file. Nothing open."))
	_ = f.ws.SelectAgent("reader")
	if _, err := f.ws.Chat().Send(context.Background(), "what is in notes"); err != nil {
		t.Fatal(err)
	}
	if err := f.ws.Close(context.Background()); err != nil {
		t.Fatalf("Close: %v", err)
	}
	st, err := memory.OpenSQLite(filepath.Join(f.dir, ".groot", "groot.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	entries, err := st.Preamble(context.Background(), memory.Reader{Agent: "reader"}, memory.Budget{})
	pre := memory.FormatPreamble(entries)
	if err != nil || !strings.Contains(pre, "Read one file") {
		t.Fatalf("summary not stored in the space memory: %q (%v)", pre, err)
	}
}

func TestNewAgentWritesStarterFolderAndRejectsDuplicates(t *testing.T) {
	f := newFixture(t)
	path, err := f.ws.NewAgent("auditor", "checks changes")
	if err != nil {
		t.Fatalf("NewAgent: %v", err)
	}
	raw, err := os.ReadFile(path)
	if err != nil || !strings.Contains(string(raw), "name: auditor") {
		t.Fatalf("starter agent file missing or wrong: %v", err)
	}
	if _, err := f.ws.NewAgent("auditor", "again"); err == nil {
		t.Fatal("duplicate agent must fail")
	}
	if _, err := f.ws.NewAgent("Bad Name", ""); err == nil {
		t.Fatal("invalid name must fail")
	}
	if _, err := f.ws.Agent("auditor"); err != nil {
		t.Fatalf("new agent not visible in the catalog: %v", err)
	}
}

func TestAssignRequiresCatalogEntry(t *testing.T) {
	f := newFixture(t)
	if err := f.ws.AssignAgent("nobody"); err == nil {
		t.Fatal("assigning an unknown agent must fail")
	}
}

func TestBuildToolsRejectsUnknownNames(t *testing.T) {
	f := newFixture(t)
	if _, err := f.ws.buildTools(f.dir, []string{"teleport"}, nil, nil, nil, nil, "reader"); err == nil {
		t.Fatal("unknown tool must be rejected")
	}
}

func contains(list []string, v string) bool {
	for _, x := range list {
		if x == v {
			return true
		}
	}
	return false
}
