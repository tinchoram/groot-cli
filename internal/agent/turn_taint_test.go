package agent

import (
	"encoding/json"
	"testing"

	"github.com/tinchoram/groot-cli/internal/api"
	"github.com/tinchoram/groot-cli/internal/provider"
)

// toolCallResponse asks the model to call the named tool once.
func toolCallResponse(name string) provider.Step {
	return provider.Step{Response: api.Response{
		Content:    []api.Block{{Type: api.BlockToolUse, ID: "call-1", Name: name, Input: json.RawMessage(`{}`)}},
		StopReason: api.StopToolUse,
	}}
}

func TestTurnTaintFollowsUntrustedOutputOfTheTurn(t *testing.T) {
	web := &webTool{name: "fetch_url", out: "page text"}
	mock := provider.NewMock(
		toolCallResponse("fetch_url"), // turn 1 reads untrusted output
		textResponse("summarized"),
		textResponse("hello"), // turn 2 reads nothing untrusted
	)
	a := newAgent(mock, registry(t, web), &recordGate{allow: true})

	if a.TurnTainted() {
		t.Fatal("a new agent starts untainted")
	}
	if _, err := a.Send(t.Context(), "fetch the page"); err != nil {
		t.Fatal(err)
	}
	if !a.TurnTainted() {
		t.Fatal("the turn read untrusted tool output; TurnTainted must be true")
	}
	if _, err := a.Send(t.Context(), "say hello"); err != nil {
		t.Fatal(err)
	}
	if a.TurnTainted() {
		t.Fatal("taint is per turn: the next turn starts clean")
	}
}

func TestTrustedToolOutputDoesNotTaintTheTurn(t *testing.T) {
	a := newAgent(provider.NewMock(), registry(t, readOnlyBash{}), &recordGate{allow: true})
	a.runTool(t.Context(), api.Block{Type: api.BlockToolUse, ID: "1", Name: "bash", Input: json.RawMessage(`{"command":"echo hi"}`)})
	if a.TurnTainted() {
		t.Fatal("bash is a trusted tool here; its output must not taint the turn")
	}
}
