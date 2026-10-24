package agent

import (
	"context"
	"testing"

	"github.com/tinchoram/groot-cli/internal/provider"
)

// contextState reads the context fields an agent keeps for its gauge.
func contextState(ag *Agent) (tokens int64, compacted, warned bool) {
	ag.mu.Lock()
	defer ag.mu.Unlock()
	return ag.contextTokens, ag.contextCompacted, ag.pressureWarned
}

// armContext makes ag report a context and sets the flags the gauge clears.
func armContext(ag *Agent) {
	ag.mu.Lock()
	defer ag.mu.Unlock()
	ag.contextTokens = 170_000
	ag.contextCompacted = true
	ag.pressureWarned = true
}

func TestResetZeroesTheContextGauge(t *testing.T) {
	ag := &Agent{Name: "reader", Provider: provider.NewMock(), Memory: openStore(t)}
	armContext(ag)

	ag.Reset()

	if tokens, compacted, warned := contextState(ag); tokens != 0 || compacted || warned {
		t.Fatalf("after Reset: tokens=%d compacted=%v warned=%v, want all zero", tokens, compacted, warned)
	}
	if got := ag.ContextTokens(); got != 0 {
		t.Fatalf("ContextTokens after Reset = %d, want 0", got)
	}
}

func TestDetachZeroesTheContextGauge(t *testing.T) {
	// /clear detaches the conversation: it must not keep the old context.
	ag := &Agent{Name: "reader", Provider: provider.NewMock(), Memory: openStore(t)}
	armContext(ag)

	ag.Detach()

	if tokens, compacted, warned := contextState(ag); tokens != 0 || compacted || warned {
		t.Fatalf("after Detach: tokens=%d compacted=%v warned=%v, want all zero", tokens, compacted, warned)
	}
}

func TestFirstAnswerAfterClearStartsFromZero(t *testing.T) {
	ctx := context.Background()
	store := openStore(t)
	ag := &Agent{Name: "reader", Provider: provider.NewMock(sizedStep("one", 4_000)), Memory: store}
	if _, err := ag.Send(ctx, "hi"); err != nil {
		t.Fatal(err)
	}
	ag.Detach()
	if got := ag.ContextTokens(); got != 0 {
		t.Fatalf("ContextTokens right after /clear = %d, want 0", got)
	}
}
