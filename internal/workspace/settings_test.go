package workspace

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tinchoram/groot-cli/internal/config"
	"github.com/tinchoram/groot-cli/internal/provider"
	"github.com/tinchoram/groot-cli/internal/space"
)

// globalFlow is a groot.yaml that sets one value of each kind, over the defaults.
func globalFlow() config.FlowConfig {
	f := config.Default().Flow
	f.Routes.Small.Model = "global-small"
	f.Routes.Large.Agent = "global-large-agent"
	return f
}

func TestResolveFlowPrecedenceIsSpaceThenGlobalThenDefault(t *testing.T) {
	builtin := config.Default().Flow
	cases := []struct {
		name   string
		global config.FlowConfig
		sp     space.Space
		want   map[string]Setting // keys: mode, small.model, small.agent, medium.model, medium.agent, large.model, large.agent
	}{
		{
			name:   "nothing set anywhere uses the built-in defaults",
			global: builtin,
			want: map[string]Setting{
				"mode":         {"strict", SourceDefault},
				"small.model":  {builtin.Routes.Small.Model, SourceDefault},
				"small.agent":  {"", SourceDefault},
				"medium.model": {builtin.Routes.Medium.Model, SourceDefault},
				"large.model":  {builtin.Routes.Large.Model, SourceDefault},
			},
		},
		{
			name:   "global mode and route override the defaults",
			global: globalFlow(),
			want: map[string]Setting{
				"mode":         {"strict", SourceDefault},
				"small.model":  {"global-small", SourceGlobal},
				"small.agent":  {"", SourceDefault},
				"medium.model": {builtin.Routes.Medium.Model, SourceDefault},
				"large.agent":  {"global-large-agent", SourceGlobal},
			},
		},
		{
			name:   "space overrides global and default for each field",
			global: globalFlow(),
			sp: space.Space{Flow: space.SpaceFlow{Mode: "advisory", Routes: space.SpaceRoutes{
				Small:  space.SpaceRoute{Model: "space-small"},
				Medium: space.SpaceRoute{Agent: "space-medium-agent"},
				Large:  space.SpaceRoute{Model: "space-large"},
			}}},
			want: map[string]Setting{
				"mode":         {"advisory", SourceSpace},
				"small.model":  {"space-small", SourceSpace},
				"small.agent":  {"", SourceDefault},
				"medium.model": {builtin.Routes.Medium.Model, SourceDefault},
				"medium.agent": {"space-medium-agent", SourceSpace},
				"large.model":  {"space-large", SourceSpace},
				"large.agent":  {"global-large-agent", SourceGlobal},
			},
		},
		{
			name:   "a global value equal to the built-in default reads as default",
			global: config.FlowConfig{Mode: "strict", Routes: builtin.Routes},
			want: map[string]Setting{
				"mode":        {"strict", SourceDefault},
				"small.model": {builtin.Routes.Small.Model, SourceDefault},
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			view := ResolveFlow(tc.sp, tc.global)
			got := map[string]Setting{
				"mode": view.Mode,
			}
			for _, r := range view.Routes {
				got[r.Size+".model"] = r.Model
				got[r.Size+".agent"] = r.Agent
			}
			for key, want := range tc.want {
				if got[key] != want {
					t.Errorf("%s = %+v, want %+v", key, got[key], want)
				}
			}
		})
	}
}

func TestResolvedFlowIsWhatTheControllerGets(t *testing.T) {
	sp := space.Space{Flow: space.SpaceFlow{Mode: "advisory", Routes: space.SpaceRoutes{
		Medium: space.SpaceRoute{Model: "space-medium", Agent: "space-agent"},
	}}}
	cfg := ResolveFlow(sp, globalFlow()).FlowConfig()
	if cfg.Mode != "advisory" || cfg.Routes.Medium.Model != "space-medium" || cfg.Routes.Medium.Agent != "space-agent" {
		t.Fatalf("merged config = %+v", cfg)
	}
	if cfg.Routes.Small.Model != "global-small" {
		t.Fatalf("unset space fields must keep the global value: %+v", cfg.Routes.Small)
	}
}

func TestResolveModelOrderIsAgentThenSpaceThenGlobal(t *testing.T) {
	spaceModel := space.Space{Model: "space-model"}
	cases := []struct {
		name, agent string
		sp          space.Space
		global      string
		want        Setting
	}{
		{"agent wins over everything", "agent-model", spaceModel, "global-model", Setting{"agent-model", SourceAgent}},
		{"space wins over global when the agent has none", "", spaceModel, "global-model", Setting{"space-model", SourceSpace}},
		{"global applies when neither agent nor space set one", "", space.Space{}, "global-model", Setting{"global-model", SourceGlobal}},
		{"provider default when nothing is set", "", space.Space{}, "", Setting{"", SourceDefault}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := ResolveModel(tc.agent, tc.sp, tc.global); got != tc.want {
				t.Fatalf("got %+v, want %+v", got, tc.want)
			}
		})
	}
}

func TestConversationUsesTheSpaceModelWhenTheAgentHasNone(t *testing.T) {
	f := newFixture(t, text("ok"))
	if err := f.spaces.SetModel("default", "claude-haiku-5-5"); err != nil {
		t.Fatal(err)
	}
	var asked string
	f.ws.opts.NewProvider = func(model string) provider.Provider {
		if asked == "" {
			asked = model
		}
		return f.mock
	}
	f.ws.opts.Config.Provider.Model = "claude-sonnet-5-5"
	if err := f.ws.SelectAgent("reader"); err != nil {
		t.Fatal(err)
	}
	_ = f.ws.Chat()
	if asked != "claude-haiku-5-5" {
		t.Fatalf("provider built with %q, want the space model", asked)
	}
}

func TestOpenTaskRoutesThroughTheSpaceRoute(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t,
		toolStep("tu_1", "task_classify", map[string]string{"size": "medium", "reason": "two files"}),
		toolStep("tu_2", "task_open", map[string]string{"goal": "ship", "scope": "space", "done_when": "tests pass"}),
		text("done"),
	)
	flowAgent(t, f)
	if err := f.spaces.SetRoute("default", "medium", "claude-haiku-5-5", ""); err != nil {
		t.Fatal(err)
	}
	conv := f.ws.Chat()
	if _, err := conv.Send(ctx, "build it"); err != nil {
		t.Fatal(err)
	}
	if f.mock.Model() != "claude-haiku-5-5" {
		t.Fatalf("the space route should apply, model = %s (the global medium route is sonnet)", f.mock.Model())
	}
}

func TestAdvisorySpaceModeLetsAnUnclassifiedWriteThrough(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t,
		toolStep("tu_1", "task_classify", map[string]string{"size": "medium", "reason": "x"}),
		toolStep("tu_2", "write_file", map[string]string{"path": "a.txt", "content": "x"}),
		text("done"),
	)
	flowAgent(t, f)
	if err := f.spaces.SetFlowMode("default", "advisory"); err != nil {
		t.Fatal(err)
	}
	conv := f.ws.Chat()
	if _, err := conv.Send(ctx, "write a file"); err != nil {
		t.Fatal(err)
	}
	if got := lastResult(t, f.mock.Calls[2].Messages); got.IsError {
		t.Fatalf("the space's advisory mode must not refuse: %q", got.Content)
	}
	if _, err := os.Stat(filepath.Join(f.dir, "a.txt")); err != nil {
		t.Fatalf("the write should have happened: %v", err)
	}
}

func TestSetSpaceRouteValidatesTheModelAndPersists(t *testing.T) {
	f := newFixture(t, text("ok"))
	f.mock.Models = knownModels
	ctx := context.Background()

	if _, err := f.ws.SetSpaceRoute(ctx, "default", "medium", "claude-haiku-5-5", "builder"); err != nil {
		t.Fatalf("a listed model is accepted: %v", err)
	}
	sp, _ := f.spaces.Get("default")
	if sp.Flow.Routes.Medium != (space.SpaceRoute{Model: "claude-haiku-5-5", Agent: "builder"}) {
		t.Fatalf("route not saved: %+v", sp.Flow.Routes.Medium)
	}
	if _, err := f.ws.SetSpaceRoute(ctx, "default", "medium", "claude-hiku-5-5", ""); err == nil {
		t.Fatal("an unknown model must be refused")
	}
	if sp, _ := f.spaces.Get("default"); sp.Flow.Routes.Medium.Model != "claude-haiku-5-5" {
		t.Fatalf("a refused model reached the file: %+v", sp.Flow.Routes.Medium)
	}
	if _, err := f.ws.SetSpaceRoute(ctx, "default", "medium", "", ""); err != nil {
		t.Fatalf("clearing needs no model check: %v", err)
	}
	if sp, _ := f.spaces.Get("default"); sp.Flow.Routes.Medium != (space.SpaceRoute{}) {
		t.Fatalf("clear failed: %+v", sp.Flow.Routes.Medium)
	}
}

func TestSetSpaceModelValidatesAndNamesTheSpace(t *testing.T) {
	f := newFixture(t, text("ok"))
	f.mock.Models = knownModels
	ctx := context.Background()

	if _, err := f.ws.SetSpaceModel(ctx, "default", "claude-opus-5-5"); err != nil {
		t.Fatal(err)
	}
	if sp, _ := f.spaces.Get("default"); sp.Model != "claude-opus-5-5" {
		t.Fatalf("model = %q", sp.Model)
	}
	if _, err := f.ws.SetSpaceModel(ctx, "default", "nope"); err == nil {
		t.Fatal("an unknown model must be refused")
	}
	if _, err := f.ws.SetSpaceModel(ctx, "missing", "claude-opus-5-5"); err == nil || !strings.Contains(err.Error(), "missing") {
		t.Fatalf("an unknown space must be named: %v", err)
	}
	if _, err := f.ws.SetSpaceModel(ctx, "default", ""); err != nil {
		t.Fatal(err)
	}
	if sp, _ := f.spaces.Get("default"); sp.Model != "" {
		t.Fatalf("clear failed: %q", sp.Model)
	}
}
