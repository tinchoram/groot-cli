package admintool

import (
	"context"
	"strings"
	"testing"

	"github.com/tinchoram/groot-cli/internal/provider"
	"github.com/tinchoram/groot-cli/internal/tool"
)

// defaultManager answers the default and session models and records the default
// that set_default_model writes.
type defaultManager struct {
	fakeManager
	def, session string
	agents       []AgentInfo
	models       []provider.ModelInfo
	set          string
}

func (d *defaultManager) DefaultModel() string    { return d.def }
func (d *defaultManager) SessionModel() string    { return d.session }
func (d *defaultManager) ListAgents() []AgentInfo { return d.agents }

func (d *defaultManager) ListModels(context.Context) ([]provider.ModelInfo, error) {
	return d.models, nil
}

func (d *defaultManager) SetDefaultModel(_ context.Context, model string) (string, error) {
	d.set = model
	return "", nil
}

func TestListAgentsResolvesTheDefaultModel(t *testing.T) {
	d := &defaultManager{def: "claude-sonnet-5-5", agents: []AgentInfo{
		{Name: "dev"},
		{Name: "ops", Model: "claude-haiku-5-5"},
	}}
	out, isErr := NewListAgents(d).Execute(context.Background(), `{}`)
	if isErr {
		t.Fatalf("unexpected error: %s", out)
	}
	if !strings.Contains(out, "model: default (claude-sonnet-5-5)") || !strings.Contains(out, "model: claude-haiku-5-5") {
		t.Fatalf("list_agents must show the resolved model:\n%s", out)
	}
}

func TestListModelsMarksTheDefaultAndTheSession(t *testing.T) {
	d := &defaultManager{
		def:     "claude-sonnet-5-5",
		session: "mystery-model",
		models: []provider.ModelInfo{
			{ID: "claude-sonnet-5-5", DisplayName: "Sonnet 5.5"},
			{ID: "mystery-model", DisplayName: "Mystery"},
		},
	}
	out, _ := NewListModels(d).Execute(context.Background(), `{}`)
	for _, line := range strings.Split(out, "\n") {
		switch {
		case strings.HasPrefix(line, "claude-sonnet-5-5"):
			if !strings.Contains(line, "[default]") || strings.Contains(line, "[session]") {
				t.Fatalf("default row: %q", line)
			}
		case strings.HasPrefix(line, "mystery-model"):
			if !strings.Contains(line, "[session]") || strings.Contains(line, "[default]") {
				t.Fatalf("session row: %q", line)
			}
		}
	}
}

func TestSetDefaultModelToolWritesThroughTheManager(t *testing.T) {
	d := &defaultManager{}
	tl := NewSetDefaultModel(d)
	out, isErr := tl.Execute(context.Background(), `{"model":"claude-haiku-5-5"}`)
	if isErr || d.set != "claude-haiku-5-5" || !strings.Contains(out, "default model set to claude-haiku-5-5") {
		t.Fatalf("set = %q, out = %q (err=%v)", d.set, out, isErr)
	}
	if tl.Risk() != tool.RiskWrite || !strings.Contains(tl.Preview(`{"model":"x"}`), "x") {
		t.Fatal("set_default_model is a write with a preview naming the model")
	}
	if out, isErr := tl.Execute(context.Background(), `{}`); !isErr {
		t.Fatalf("a missing model is refused: %q", out)
	}
}

func (f *fakeManager) DefaultModel() string                                    { return "" }
func (f *fakeManager) SessionModel() string                                    { return "" }
func (f *fakeManager) SetDefaultModel(context.Context, string) (string, error) { return "", nil }
