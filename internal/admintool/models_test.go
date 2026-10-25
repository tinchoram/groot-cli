package admintool

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/tinchoram/groot-cli/internal/provider"
	"github.com/tinchoram/groot-cli/internal/tool"
)

// The existing fake satisfies the Manager interface with empty model answers.
func (f *fakeManager) ListModels(context.Context) ([]provider.ModelInfo, error) { return nil, nil }

func (f *fakeManager) SetAgentModel(context.Context, string, string) (string, error) {
	return "", nil
}

func (f *fakeManager) AgentTools(string) ([]string, error) { return nil, nil }

func (f *fakeManager) EditAgentTools(string, []string, []string) error { return nil }

// modelManager is the fake the model tools run against: a fixed model list and
// a record of the agents whose model was set.
type modelManager struct {
	fakeManager
	models  []provider.ModelInfo
	listErr error
	saved   map[string]string
	warn    string
}

func (m *modelManager) ListModels(context.Context) ([]provider.ModelInfo, error) {
	return m.models, m.listErr
}

func (m *modelManager) SetAgentModel(_ context.Context, agent, model string) (string, error) {
	if m.saved == nil {
		m.saved = map[string]string{}
	}
	m.saved[agent] = model
	return m.warn, nil
}

func TestListModelsShowsPricesAndUnknowns(t *testing.T) {
	m := &modelManager{models: []provider.ModelInfo{
		{ID: "claude-sonnet-5-5", DisplayName: "Sonnet 5.5"},
		{ID: "mystery-model", DisplayName: "Mystery"},
	}}
	out, isErr := NewListModels(m).Execute(context.Background(), `{}`)
	if isErr {
		t.Fatalf("unexpected error: %s", out)
	}
	for _, want := range []string{"claude-sonnet-5-5", "Sonnet 5.5", "$2.00 in / $10.00 out per 1M", "mystery-model", "price unknown"} {
		if !strings.Contains(out, want) {
			t.Fatalf("list_models output lacks %q:\n%s", want, out)
		}
	}
}

func TestListModelsReportsAFailure(t *testing.T) {
	m := &modelManager{listErr: errors.New("no key")}
	out, isErr := NewListModels(m).Execute(context.Background(), `{}`)
	if !isErr || !strings.Contains(out, "no key") {
		t.Fatalf("got %q (err=%v)", out, isErr)
	}
}

func TestSetAgentModelSavesAndReportsWarnings(t *testing.T) {
	m := &modelManager{warn: "model not verified: offline"}
	tl := NewSetAgentModel(m)
	out, isErr := tl.Execute(context.Background(), `{"agent":"reader","model":"claude-haiku-5-5"}`)
	if isErr {
		t.Fatalf("unexpected error: %s", out)
	}
	if m.saved["reader"] != "claude-haiku-5-5" {
		t.Fatalf("saved = %v", m.saved)
	}
	if !strings.Contains(out, "model of reader set to claude-haiku-5-5") || !strings.Contains(out, "model not verified") {
		t.Fatalf("output = %q", out)
	}
	if tl.Risk() != tool.RiskWrite || !strings.Contains(tl.Preview(`{"agent":"reader","model":"x"}`), "reader") {
		t.Fatal("set_agent_model is a write with a preview naming the agent")
	}
}

func TestSetAgentModelRefusesUnknownFields(t *testing.T) {
	m := &modelManager{}
	if out, isErr := NewSetAgentModel(m).Execute(context.Background(), `{"agent":"reader","model":"x","extra":1}`); !isErr {
		t.Fatalf("unknown fields must be refused: %q", out)
	}
}

func (f *fakeManager) SetAgentGlobalMemory(string, bool) error { return nil }
