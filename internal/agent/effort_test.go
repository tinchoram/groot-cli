package agent

import (
	"context"
	"testing"

	"github.com/tinchoram/groot-cli/internal/gate"
	"github.com/tinchoram/groot-cli/internal/provider"
)

// The agent's effort travels with every model call it makes.
func TestAgentEffortIsPassedToEveryCall(t *testing.T) {
	mock := provider.NewMock(textResponse("one"))
	a := newAgent(mock, registry(t), gate.AutoApprove{})
	a.Effort = "high"

	if _, err := a.Send(context.Background(), "hi"); err != nil {
		t.Fatal(err)
	}
	if len(mock.Calls) != 1 || mock.Calls[0].Effort != "high" {
		t.Fatalf("calls = %+v, want one call with effort high", mock.Calls)
	}
}

func TestAgentWithoutEffortSendsNone(t *testing.T) {
	mock := provider.NewMock(textResponse("one"))
	a := newAgent(mock, registry(t), gate.AutoApprove{})

	if _, err := a.Send(context.Background(), "hi"); err != nil {
		t.Fatal(err)
	}
	if mock.Calls[0].Effort != "" {
		t.Fatalf("effort = %q, want empty so the provider value applies", mock.Calls[0].Effort)
	}
}
