package workspace

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/tinchoram/groot-cli/internal/api"
	"github.com/tinchoram/groot-cli/internal/catalog"
	"github.com/tinchoram/groot-cli/internal/config"
	"github.com/tinchoram/groot-cli/internal/gate"
	"github.com/tinchoram/groot-cli/internal/provider"
	"github.com/tinchoram/groot-cli/internal/space"
)

// adminFixture bootstraps a fresh home and opens a workspace over it, using
// the mock provider for every conversation.
func adminFixture(t *testing.T, steps ...provider.Step) (*Workspace, string, string, *provider.Mock) {
	t.Helper()
	home := t.TempDir()
	userHome := t.TempDir()
	if _, err := Bootstrap(home, userHome, t0); err != nil {
		t.Fatalf("Bootstrap: %v", err)
	}
	sp, err := space.Open(filepath.Join(home, "spaces.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	mock := provider.NewMock(steps...)
	ws, err := New(Options{
		Home:          home,
		UserHome:      userHome,
		DisablePublic: true,
		Config:        config.Default(),
		Spaces:        sp,
		Gate:          gate.AutoApprove{},
		NewProvider:   func(string) provider.Provider { return mock },
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ws.Close(context.Background()) })
	return ws, home, userHome, mock
}

func TestFreshBootstrapCreatesRootAndGroot(t *testing.T) {
	home, userHome := t.TempDir(), t.TempDir()
	fresh, err := Bootstrap(home, userHome, t0)
	if err != nil || !fresh {
		t.Fatalf("Bootstrap = %v, %v; want fresh", fresh, err)
	}
	sp, _ := space.Open(filepath.Join(home, "spaces.yaml"))
	list := sp.List()
	if len(list) != 1 || list[0].Name != "root" || list[0].Path != userHome {
		t.Fatalf("spaces = %+v", list)
	}
	if !slices.Contains(list[0].Agents, "groot") {
		t.Fatalf("root agents = %v", list[0].Agents)
	}
	if cur, ok := sp.Current(); !ok || cur.Name != "root" {
		t.Fatalf("active space = %+v", cur)
	}

	defs, err := catalog.Load(home)
	if err != nil {
		t.Fatal(err)
	}
	groot := findDef(defs, "groot")
	if groot == nil {
		t.Fatal("groot not in catalog")
	}
	for _, forbidden := range []string{"read_file", "write_file", "bash", "kubectl"} {
		if contains(groot.Tools, forbidden) {
			t.Errorf("groot must not have %s", forbidden)
		}
	}
	for _, want := range []string{"list_spaces", "create_space", "create_agent", "recall", "remember"} {
		if !contains(groot.Tools, want) {
			t.Errorf("groot lacks %s", want)
		}
	}
	if !strings.Contains(groot.Prompt, "administrator") {
		t.Errorf("groot prompt should describe its role:\n%s", groot.Prompt)
	}
	if len(defs) != 1 {
		t.Fatalf("a fresh home must hold only groot, got %d agents", len(defs))
	}
	if findDef(defs, "devjunior") != nil {
		t.Fatal("a fresh home must not create the devjunior sample")
	}
}

func TestUpgradeKeepsExistingSpaceAndAddsRootAndGroot(t *testing.T) {
	home, userHome, cwd := t.TempDir(), t.TempDir(), t.TempDir()
	if err := catalog.Save(home, legacyDevJunior(), legacyDevJuniorPrompt); err != nil {
		t.Fatal(err)
	}
	old, _ := space.Open(filepath.Join(home, "spaces.yaml"))
	_ = old.Add("default", cwd, t0)
	_ = old.Assign("default", "devjunior")
	_ = old.Use("default")

	fresh, err := Bootstrap(home, userHome, t0)
	if err != nil || fresh {
		t.Fatalf("upgrade must not report fresh: %v, %v", fresh, err)
	}
	sp, _ := space.Open(filepath.Join(home, "spaces.yaml"))
	list := sp.List()
	if len(list) != 2 || list[0].Name != "root" || list[1].Name != "default" {
		t.Fatalf("root must come first and default must stay: %+v", list)
	}
	if cur, _ := sp.Current(); cur.Name != "default" {
		t.Fatalf("active space must stay default, got %q", cur.Name)
	}
	if got := list[1].Agents; len(got) != 1 || got[0] != "devjunior" {
		t.Fatalf("default agents changed: %v", got)
	}
	if got := list[0].Agents; !slices.Equal(got, []string{"groot"}) {
		t.Fatalf("root agents = %v, want only groot", got)
	}
}

func TestBootstrapIsIdempotentAndKeepsEdits(t *testing.T) {
	home, userHome := t.TempDir(), t.TempDir()
	if _, err := Bootstrap(home, userHome, t0); err != nil {
		t.Fatal(err)
	}
	if err := catalog.Save(home, grootDef(), "my own groot prompt"); err != nil {
		t.Fatal(err)
	}
	before, _ := os.ReadFile(filepath.Join(home, "spaces.yaml"))
	fresh, err := Bootstrap(home, userHome, t0)
	if err != nil || fresh {
		t.Fatalf("second run: %v, %v", fresh, err)
	}
	after, _ := os.ReadFile(filepath.Join(home, "spaces.yaml"))
	if string(before) != string(after) {
		t.Fatal("second run must not change spaces.yaml")
	}
	defs, _ := catalog.Load(home)
	if g := findDef(defs, "groot"); g == nil || g.Prompt != "my own groot prompt" {
		t.Fatal("user edits to groot's prompt must survive")
	}
}

func TestBootstrapRequiresUserHome(t *testing.T) {
	if _, err := Bootstrap(t.TempDir(), "", t0); err == nil {
		t.Fatal("empty user home must be an error")
	}
}

func TestGrootConversationCreatesAgentThroughAdminTool(t *testing.T) {
	in := `{"name":"auditor","description":"checks merges","prompt":"You audit merges.","tools":["read_file"]}`
	ws, home, _, _ := adminFixture(t,
		provider.Step{Response: api.Response{Content: []api.Block{{Type: api.BlockToolUse, ID: "t1", Name: "create_agent", Input: []byte(in)}}, StopReason: api.StopToolUse}},
		provider.Step{Response: api.Response{Content: []api.Block{{Type: api.BlockText, Text: "created"}}, StopReason: api.StopEndTurn}},
	)
	if ws.ActiveAgent() != "groot" {
		t.Fatalf("fresh home should select groot, got %q", ws.ActiveAgent())
	}
	if _, err := ws.Chat().Send(context.Background(), "make an auditor"); err != nil {
		t.Fatal(err)
	}
	defs, _ := catalog.Load(home)
	a := findDef(defs, "auditor")
	if a == nil || a.Prompt != "You audit merges." || a.Description != "checks merges" {
		t.Fatalf("auditor not created: %+v", a)
	}
}

func TestGrootConversationCreatesSpaceThroughAdminTool(t *testing.T) {
	dir := t.TempDir()
	in := `{"name":"payments","path":"` + dir + `"}`
	ws, _, _, _ := adminFixture(t,
		provider.Step{Response: api.Response{Content: []api.Block{{Type: api.BlockToolUse, ID: "t1", Name: "create_space", Input: []byte(in)}}, StopReason: api.StopToolUse}},
		provider.Step{Response: api.Response{Content: []api.Block{{Type: api.BlockText, Text: "done"}}, StopReason: api.StopEndTurn}},
	)
	if _, err := ws.Chat().Send(context.Background(), "create payments"); err != nil {
		t.Fatal(err)
	}
	found := false
	for _, sp := range ws.Spaces() {
		if sp.Name == "payments" && sp.Path == dir {
			found = true
		}
	}
	if !found {
		t.Fatal("create_space did not create the space")
	}
}

func TestGrootNameIsManaged(t *testing.T) {
	ws, _, _, _ := adminFixture(t)
	if _, err := ws.NewAgent("groot", "impostor"); err == nil || !strings.Contains(err.Error(), "managed") {
		t.Fatalf("/agent new groot must be refused, got %v", err)
	}
}

func findDef(defs []catalog.AgentDef, name string) *catalog.AgentDef {
	for i := range defs {
		if defs[i].Name == name {
			return &defs[i]
		}
	}
	return nil
}

// legacyDevJunior is the sample agent that 0.2.0 development builds created.
// Homes made by those builds keep it; the tests recreate one to check that
// upgrades leave it alone.
func legacyDevJunior() catalog.AgentDef {
	return catalog.AgentDef{
		Name:        "devjunior",
		Description: "Junior developer: reads code, runs tests, proposes small changes",
		Tools:       []string{"read_file", "write_file", "bash", "recall", "remember"},
	}
}

const legacyDevJuniorPrompt = "You are DevJunior, a junior developer. Explore before you edit."

func TestGrootHasLibraryToolsAndPrompt(t *testing.T) {
	def := grootDef()
	for _, want := range []string{"list_library", "install_agent"} {
		if !contains(def.Tools, want) {
			t.Errorf("groot lacks %s", want)
		}
	}
	if !strings.Contains(grootPrompt, "list_library") || !strings.Contains(grootPrompt, "install_agent") {
		t.Error("groot's prompt must say when to use the library tools")
	}
}
