package provider

import (
	"context"
	"errors"
	"testing"

	"github.com/tinchoram/groot-cli/internal/api"
)

// stubProvider is a Provider that records calls, reports usage and lists models.
type stubProvider struct {
	name   string
	model  string
	sent   []string
	usage  api.Usage
	cost   float64
	models []ModelInfo
	err    error
}

func (s *stubProvider) Send(_ context.Context, _ Request) (api.Response, error) {
	s.sent = append(s.sent, s.model)
	s.usage.Add(api.Usage{InputTokens: 10, OutputTokens: 5})
	s.cost += 0.5
	return api.Response{Content: []api.Block{{Type: api.BlockText, Text: s.name}}, StopReason: api.StopEndTurn}, nil
}
func (s *stubProvider) Model() string     { return s.model }
func (s *stubProvider) SetModel(n string) { s.model = n }
func (s *stubProvider) Usage() api.Usage  { return s.usage }
func (s *stubProvider) EstimatedCostUSD() float64 {
	return s.cost
}
func (s *stubProvider) ListModels(context.Context) ([]ModelInfo, error) {
	return s.models, s.err
}

// stubs returns a builder that keeps one stub per provider name and counts builds.
func stubs(defaults map[string]string, builds *[]string) (Builder, map[string]*stubProvider) {
	made := map[string]*stubProvider{}
	return func(name, model string) Provider {
		*builds = append(*builds, name)
		s := &stubProvider{name: name, model: model}
		if model == "" {
			s.model = defaults[name]
		}
		made[name] = s
		return s
	}, made
}

func TestNameOfRecognisesPrefixesAndFallsBack(t *testing.T) {
	cases := []struct {
		model, fallback, want string
	}{
		{"gemini-3.8-flash", NameAnthropic, NameGemini},
		{"claude-sonnet-5-5", NameGemini, NameAnthropic},
		{"", NameGemini, NameGemini},
		{"mystery-model", NameGemini, NameGemini},
		{"mystery-model", NameAnthropic, NameAnthropic},
	}
	for _, c := range cases {
		if got := NameOf(c.model, c.fallback); got != c.want {
			t.Fatalf("NameOf(%q, %q) = %q, want %q", c.model, c.fallback, got, c.want)
		}
	}
}

func TestRouterSwitchingToAnotherProviderBuildsIt(t *testing.T) {
	var builds []string
	build, made := stubs(map[string]string{NameAnthropic: "claude-sonnet-5-5"}, &builds)
	r := NewRouter(RouterOptions{Default: NameAnthropic, Model: "", Keyed: []string{NameAnthropic, NameGemini}, Build: build})
	if r.Model() != "claude-sonnet-5-5" {
		t.Fatalf("Model() = %q, want the provider default", r.Model())
	}
	r.SetModel("gemini-3.8-flash")
	if got := builds; len(got) != 2 || got[1] != NameGemini {
		t.Fatalf("builds = %v, want a new gemini provider", got)
	}
	if r.Model() != "gemini-3.8-flash" || made[NameGemini].model != "gemini-3.8-flash" {
		t.Fatalf("model not carried to the gemini provider: router %q, gemini %q", r.Model(), made[NameGemini].model)
	}
	if _, err := r.Send(context.Background(), Request{}); err != nil {
		t.Fatal(err)
	}
	if len(made[NameGemini].sent) != 1 || len(made[NameAnthropic].sent) != 0 {
		t.Fatalf("the call went to the wrong provider: gemini %v, anthropic %v", made[NameGemini].sent, made[NameAnthropic].sent)
	}
}

func TestRouterKeepsUsageAcrossSwitches(t *testing.T) {
	var builds []string
	build, _ := stubs(map[string]string{NameAnthropic: "claude-sonnet-5-5"}, &builds)
	r := NewRouter(RouterOptions{Default: NameAnthropic, Keyed: []string{NameAnthropic, NameGemini}, Build: build})
	ctx := context.Background()
	mustSend := func() {
		t.Helper()
		if _, err := r.Send(ctx, Request{}); err != nil {
			t.Fatal(err)
		}
	}
	mustSend()
	r.SetModel("gemini-3.8-flash")
	mustSend()
	r.SetModel("claude-sonnet-5-5")
	mustSend()
	if len(builds) != 2 {
		t.Fatalf("builds = %v, want one provider per kind (the Anthropic one is reused)", builds)
	}
	if u := r.Usage(); u.InputTokens != 30 || u.OutputTokens != 15 {
		t.Fatalf("usage = %+v, want the sum over all three calls", u)
	}
	if c := r.EstimatedCostUSD(); c != 1.5 {
		t.Fatalf("cost = %v, want 1.5", c)
	}
}

func TestRouterListsEveryKeyedProviderTagged(t *testing.T) {
	r := NewRouter(RouterOptions{Default: NameAnthropic, Model: "claude-sonnet-5-5", Keyed: []string{NameAnthropic, NameGemini},
		Build: func(name, model string) Provider {
			s := &stubProvider{name: name, model: model}
			if name == NameAnthropic {
				s.models = []ModelInfo{{ID: "claude-sonnet-5-5", DisplayName: "Sonnet"}}
			} else {
				s.models = []ModelInfo{{ID: "gemini-3.8-flash", DisplayName: "Gemini 3.8 Flash"}}
			}
			return s
		}})
	models, err := r.ListModels(context.Background())
	if err != nil {
		t.Fatalf("ListModels: %v", err)
	}
	if len(models) != 2 || models[0].Provider != NameAnthropic || models[1].Provider != NameGemini {
		t.Fatalf("models = %+v, want one anthropic and one gemini entry, tagged", models)
	}
}

func TestRouterListingSurvivesOneProviderFailing(t *testing.T) {
	r := NewRouter(RouterOptions{Default: NameAnthropic, Model: "claude-sonnet-5-5", Keyed: []string{NameAnthropic, NameGemini},
		Build: func(name, model string) Provider {
			if name == NameAnthropic {
				return &stubProvider{name: name, model: model, err: errors.New("offline")}
			}
			return &stubProvider{name: name, model: model, models: []ModelInfo{{ID: "gemini-3.8-flash"}}}
		}})
	models, err := r.ListModels(context.Background())
	if err != nil {
		t.Fatalf("ListModels with one provider up: %v", err)
	}
	if len(models) != 1 || models[0].ID != "gemini-3.8-flash" || models[0].Provider != NameGemini {
		t.Fatalf("models = %+v, want only the gemini model", models)
	}
}

func TestRouterListingFailsWhenEveryProviderFails(t *testing.T) {
	r := NewRouter(RouterOptions{Default: NameAnthropic, Model: "claude-sonnet-5-5", Keyed: []string{NameAnthropic},
		Build: func(name, model string) Provider {
			return &stubProvider{name: name, model: model, err: errors.New("offline")}
		}})
	if _, err := r.ListModels(context.Background()); err == nil {
		t.Fatal("want an error when no provider can list")
	}
}
