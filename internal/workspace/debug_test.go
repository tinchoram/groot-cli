package workspace

import (
	"slices"
	"testing"

	"github.com/tinchoram/groot-cli/internal/catalog"
	"github.com/tinchoram/groot-cli/internal/space"
)

func TestBootstrapCreatesTheDebugAgentNextToGroot(t *testing.T) {
	home, userHome := t.TempDir(), t.TempDir()
	if _, err := Bootstrap(home, userHome, t0); err != nil {
		t.Fatal(err)
	}
	defs, _ := catalog.Load(home)
	var debug catalog.AgentDef
	for _, d := range defs {
		if d.Name == debugAgent {
			debug = d
		}
	}
	if debug.Name == "" {
		t.Fatal("bootstrap must create the debug agent")
	}
	for _, want := range []string{"feedback_report", "feedback_list", "feedback_update"} {
		if !slices.Contains(debug.Tools, want) {
			t.Fatalf("debug lacks %s: %v", want, debug.Tools)
		}
	}
	sp, _ := space.Open(home + "/spaces.yaml")
	root, _ := sp.Get("root")
	if !slices.Contains(root.Agents, debugAgent) {
		t.Fatalf("debug is assigned to root: %v", root.Agents)
	}
}

func TestDebugIsManagedLikeGroot(t *testing.T) {
	f := newFixture(t, text("ok"))
	if _, err := f.ws.NewAgent(debugAgent, "x"); err == nil {
		t.Fatal("debug cannot be created again")
	}
	if _, err := f.ws.UpgradeAgents(debugAgent); err == nil {
		t.Fatal("debug is not upgraded by hand")
	}
}
