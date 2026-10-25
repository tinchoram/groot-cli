package agent

import (
	"context"
	"slices"
	"testing"

	"github.com/tinchoram/groot-cli/internal/api"
	"github.com/tinchoram/groot-cli/internal/gate"
	"github.com/tinchoram/groot-cli/internal/provider"
	"github.com/tinchoram/groot-cli/internal/tool"
)

// swapAfterFirstCall runs swap once the first model call has returned, which is
// the point where a turn is between two rounds.
type swapAfterFirstCall struct {
	provider.Provider
	swap func()
}

func (s *swapAfterFirstCall) Send(ctx context.Context, req provider.Request) (api.Response, error) {
	resp, err := s.Provider.Send(ctx, req)
	if s.swap != nil {
		s.swap()
		s.swap = nil
	}
	return resp, err
}

func TestSetToolsTakesEffectOnTheNextRound(t *testing.T) {
	mock := provider.NewMock(toolUseResponse("t1", "echo", "{}"), textResponse("done"))
	a := newAgent(mock, registry(t, &echoTool{risk: tool.RiskRead}), gate.AutoApprove{})
	a.Provider = &swapAfterFirstCall{Provider: mock, swap: func() {
		a.SetTools(registry(t, namedTool{name: "late"}))
	}}

	if _, err := a.Send(context.Background(), "go"); err != nil {
		t.Fatalf("Send: %v", err)
	}
	first := definitionNames(mock.Calls[0].Tools)
	second := definitionNames(mock.Calls[1].Tools)
	if !slices.Contains(first, "echo") || slices.Contains(first, "late") {
		t.Fatalf("first round must use the registry before the swap, got %v", first)
	}
	if slices.Contains(second, "echo") || !slices.Contains(second, "late") {
		t.Fatalf("second round must use the registry set mid-turn, got %v", second)
	}
	if _, ok := a.registry().Get("late"); !ok {
		t.Fatal("SetTools must replace what registry returns")
	}
}

func definitionNames(defs []api.ToolDef) []string {
	var names []string
	for _, d := range defs {
		names = append(names, d.Name)
	}
	return names
}

// namedTool is a read-only tool with a configurable name.
type namedTool struct{ name string }

func (n namedTool) Definition() api.ToolDef {
	return api.ToolDef{Name: n.name, Description: n.name, InputSchema: []byte(`{"type":"object"}`)}
}
func (n namedTool) Risk() tool.Risk                                { return tool.RiskRead }
func (n namedTool) RiskFor(string) tool.Risk                       { return tool.RiskRead }
func (n namedTool) Preview(string) string                          { return n.name }
func (n namedTool) Execute(context.Context, string) (string, bool) { return "ok", false }
