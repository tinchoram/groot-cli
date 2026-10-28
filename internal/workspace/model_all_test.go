package workspace

import (
	"context"
	"reflect"
	"strings"
	"testing"

	"github.com/tinchoram/groot-cli/internal/catalog"
	"github.com/tinchoram/groot-cli/internal/provider"
)

// twoAgentFixture adds a second agent to the default space of the fixture.
func twoAgentFixture(t *testing.T) *fixture {
	t.Helper()
	f := newFixture(t, text("ok"))
	if err := catalog.Save(f.home, catalog.AgentDef{Name: "writer", Description: "writes files", Tools: []string{"read_file"}}, "You write."); err != nil {
		t.Fatal(err)
	}
	if err := f.ws.reload(); err != nil {
		t.Fatal(err)
	}
	if err := f.ws.opts.Spaces.Assign("default", "writer"); err != nil {
		t.Fatal(err)
	}
	return f
}

func TestSetSpaceAgentsModelSetsEveryAgentAndTheSpaceDefault(t *testing.T) {
	f := twoAgentFixture(t)
	f.mock.Models = []provider.ModelInfo{{ID: "gemini-3.8-flash"}}

	changed, warn, err := f.ws.SetSpaceAgentsModel(context.Background(), "default", "gemini-3.8-flash", false)
	if err != nil || warn != "" {
		t.Fatalf("listed model: warn=%q err=%v", warn, err)
	}
	if want := []string{"reader", "writer"}; !reflect.DeepEqual(changed, want) {
		t.Fatalf("changed = %v, want %v", changed, want)
	}
	for _, a := range []string{"reader", "writer"} {
		if got := agentModel(t, f.home, a); got != "gemini-3.8-flash" {
			t.Fatalf("%s model = %q", a, got)
		}
	}
	sp, _ := f.ws.opts.Spaces.Get("default")
	if sp.Model != "gemini-3.8-flash" {
		t.Fatalf("space default = %q", sp.Model)
	}
}

func TestSetSpaceAgentsModelRefusesAnUnknownIdAndChangesNothing(t *testing.T) {
	f := twoAgentFixture(t)
	f.mock.Models = []provider.ModelInfo{{ID: "gemini-3.8-flash"}}

	changed, _, err := f.ws.SetSpaceAgentsModel(context.Background(), "default", "gemini-3.8-flsh", false)
	if err == nil || !strings.Contains(err.Error(), "unknown model") {
		t.Fatalf("err = %v, want an unknown model refusal", err)
	}
	if len(changed) != 0 {
		t.Fatalf("changed = %v after a refusal", changed)
	}
	if got := agentModel(t, f.home, "reader"); got == "gemini-3.8-flsh" {
		t.Fatal("a refused id was saved")
	}
}

func TestSetSpaceAgentsModelForceSkipsTheCheck(t *testing.T) {
	f := twoAgentFixture(t)
	f.mock.Models = []provider.ModelInfo{{ID: "gemini-3.8-flash"}}

	changed, _, err := f.ws.SetSpaceAgentsModel(context.Background(), "default", "preview-model", true)
	if err != nil || len(changed) != 2 {
		t.Fatalf("force: changed=%v err=%v", changed, err)
	}
	if got := agentModel(t, f.home, "writer"); got != "preview-model" {
		t.Fatalf("writer model = %q", got)
	}
}

func TestSetSpaceAgentsModelRefusesAnUnknownSpace(t *testing.T) {
	f := twoAgentFixture(t)
	if _, _, err := f.ws.SetSpaceAgentsModel(context.Background(), "nowhere", "x", true); err == nil {
		t.Fatal("an unknown space must be refused")
	}
}
