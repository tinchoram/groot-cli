package agent

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/tinchoram/groot-cli/internal/api"
	"github.com/tinchoram/groot-cli/internal/gate"
	"github.com/tinchoram/groot-cli/internal/memory"
	"github.com/tinchoram/groot-cli/internal/provider"
	"github.com/tinchoram/groot-cli/internal/tool"
)

// captureStore keeps every message the agent persists, in order.
type captureStore struct {
	memory.NoMemory
	messages []api.Message
}

func (c *captureStore) AppendMessage(_ context.Context, _ string, _ int, m api.Message, _ api.Usage) error {
	c.messages = append(c.messages, m)
	return nil
}

// cancelTool cancels the context of the turn while it runs.
type cancelTool struct{ cancel context.CancelFunc }

func (c *cancelTool) Definition() api.ToolDef {
	return api.ToolDef{Name: "cancel_me", InputSchema: json.RawMessage(`{"type":"object"}`)}
}
func (c *cancelTool) Risk() tool.Risk          { return tool.RiskRead }
func (c *cancelTool) RiskFor(string) tool.Risk { return tool.RiskRead }
func (c *cancelTool) Preview(string) string    { return "cancel" }
func (c *cancelTool) Execute(context.Context, string) (string, bool) {
	c.cancel()
	return "done", false
}

// plainTool answers, or panics when panics is set.
type plainTool struct {
	name   string
	panics bool
}

func (p *plainTool) Definition() api.ToolDef {
	return api.ToolDef{Name: p.name, InputSchema: json.RawMessage(`{"type":"object"}`)}
}
func (p *plainTool) Risk() tool.Risk          { return tool.RiskRead }
func (p *plainTool) RiskFor(string) tool.Risk { return tool.RiskRead }
func (p *plainTool) Preview(string) string    { return p.name }
func (p *plainTool) Execute(context.Context, string) (string, bool) {
	if p.panics {
		panic("boom")
	}
	return "ok", false
}

func toolUseBlock(id, name string) api.Block {
	return api.Block{Type: api.BlockToolUse, ID: id, Name: name, Input: json.RawMessage(`{}`)}
}

func danglingHistory() []api.Message {
	return []api.Message{
		{Role: api.RoleUser, Content: []api.Block{text("hi")}},
		{Role: api.RoleAssistant, Content: []api.Block{toolUseBlock("a", "cancel_me")}},
	}
}

func TestResumeRepairsADanglingToolUse(t *testing.T) {
	a := newAgent(provider.NewMock(), registry(t), gate.AutoApprove{})
	a.Resume("s1", danglingHistory(), 1, 0)

	h := a.snapshot()
	if len(h) != 3 || h[2].Role != api.RoleUser {
		t.Fatalf("history after resume = %+v", h)
	}
	got := h[2].Content[0]
	if got.Type != api.BlockToolResult || got.ToolUseID != "a" || !got.IsError {
		t.Fatalf("the dangling call gets a cancelled result: %+v", got)
	}
}

func TestSendPersistsTheRepairBeforeThePrompt(t *testing.T) {
	store := &captureStore{}
	a := newAgent(provider.NewMock(textResponse("ok")), registry(t), gate.AutoApprove{})
	a.Memory = store
	a.Resume("s1", danglingHistory(), 1, 0)

	if _, err := a.Send(context.Background(), "again"); err != nil {
		t.Fatal(err)
	}
	if len(store.messages) < 2 {
		t.Fatalf("persisted %d messages", len(store.messages))
	}
	first := store.messages[0]
	if first.Role != api.RoleUser || first.Content[0].Type != api.BlockToolResult || first.Content[0].ToolUseID != "a" {
		t.Fatalf("the synthetic result must be stored before the prompt: %+v", first)
	}
	if second := store.messages[1]; second.Content[0].Text != "again" {
		t.Fatalf("the prompt follows the repair: %+v", second)
	}
}

func TestCancelledContextMidToolLeavesAResultForEveryCall(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	reg := registry(t, &cancelTool{cancel: cancel}, &plainTool{name: "echo_two"})
	step := provider.Step{Response: api.Response{
		Content:    []api.Block{toolUseBlock("a", "cancel_me"), toolUseBlock("b", "echo_two")},
		StopReason: api.StopToolUse,
	}}
	store := &captureStore{}
	a := &Agent{Name: "sre", Provider: provider.NewMock(step, textResponse("done")), Tools: reg,
		Gate: gate.AutoApprove{}, System: "sys", MaxTurns: 5, Memory: store}

	_, _ = a.Send(ctx, "go")

	results := findToolResults(t, a.snapshot())
	if len(results) != 2 || results[0].ToolUseID != "a" || results[0].IsError || results[0].Content != "done" {
		t.Fatalf("the finished call keeps its result: %+v", results)
	}
	if results[1].ToolUseID != "b" || !results[1].IsError || !strings.Contains(results[1].Content, "cancelled") {
		t.Fatalf("the call after the cancel gets a cancelled result: %+v", results[1])
	}
	if got := findToolResults(t, store.messages); len(got) != 2 {
		t.Fatalf("the persisted history must hold both results: %+v", got)
	}
}

func TestToolPanicBecomesAnErrorResult(t *testing.T) {
	reg := registry(t, &plainTool{name: "boom", panics: true})
	step := provider.Step{Response: api.Response{
		Content:    []api.Block{toolUseBlock("x", "boom")},
		StopReason: api.StopToolUse,
	}}
	a := &Agent{Name: "sre", Provider: provider.NewMock(step, textResponse("after")), Tools: reg,
		Gate: gate.AutoApprove{}, System: "sys", MaxTurns: 5}

	reply, err := a.Send(context.Background(), "go")
	if err != nil || reply != "after" {
		t.Fatalf("reply = %q, err = %v: a panicking tool must not end the turn", reply, err)
	}
	results := findToolResults(t, a.snapshot())
	if len(results) != 1 || !results[0].IsError || !strings.Contains(results[0].Content, "panicked") {
		t.Fatalf("results = %+v", results)
	}
}

// findToolResults returns the tool_result blocks of the last user message that
// has any.
func findToolResults(t *testing.T, msgs []api.Message) []api.Block {
	t.Helper()
	for i := len(msgs) - 1; i >= 0; i-- {
		var out []api.Block
		for _, b := range msgs[i].Content {
			if b.Type == api.BlockToolResult {
				out = append(out, b)
			}
		}
		if len(out) > 0 {
			return out
		}
	}
	return nil
}
