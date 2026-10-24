package agent

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/tinchoram/groot-cli/internal/api"
	"github.com/tinchoram/groot-cli/internal/memory"
	"github.com/tinchoram/groot-cli/internal/provider"
)

// pricedStep answers with one million input tokens, which costs $0.10 on
// claude-haiku-5-5 (see provider/pricing.go).
func pricedStep(text string) provider.Step {
	s := replyStep(text)
	s.Response.Usage = api.Usage{InputTokens: 1_000_000}
	return s
}

func pricedMock(steps ...provider.Step) *provider.Mock {
	m := provider.NewMock(steps...)
	m.SetModel("claude-haiku-5-5")
	return m
}

func near(a, b float64) bool {
	d := a - b
	return d < 1e-9 && d > -1e-9
}

func TestEveryModelCallPersistsTheSessionCost(t *testing.T) {
	ctx := context.Background()
	store := openStore(t)
	ag := &Agent{Name: "reader", Provider: pricedMock(pricedStep("one")), Memory: store}
	if _, err := ag.Send(ctx, "hi"); err != nil {
		t.Fatal(err)
	}
	// The cost is stored with the model answer, before the session ends.
	if got, _ := store.SpendUSD(ctx, "reader"); !near(got, 0.10) {
		t.Fatalf("spend before the session ends = %v, want 0.10", got)
	}
}

func TestSecondSessionInTheSameProcessCountsOnlyItsOwnCost(t *testing.T) {
	ctx := context.Background()
	store := openStore(t)
	ag := &Agent{Name: "reader", Provider: pricedMock(pricedStep("one"), pricedStep("two")), Memory: store}
	if _, err := ag.Send(ctx, "first"); err != nil {
		t.Fatal(err)
	}
	ag.Detach() // /clear: the provider keeps its running total
	if _, err := ag.Send(ctx, "second"); err != nil {
		t.Fatal(err)
	}
	if got, _ := store.SpendUSD(ctx, "reader"); !near(got, 0.20) {
		t.Fatalf("spend of two sessions = %v, want 0.20 (not 0.30)", got)
	}
}

func TestResumedSessionAddsItsNewCostToTheStoredOne(t *testing.T) {
	ctx := context.Background()
	store := openStore(t)
	if err := store.StartSession(ctx, memory.Session{ID: "s-1", Agent: "reader", StartedAt: time.Now().UTC()}); err != nil {
		t.Fatal(err)
	}
	if err := store.SetSessionCost(ctx, "s-1", 0.5); err != nil {
		t.Fatal(err)
	}
	ag := &Agent{Name: "reader", Provider: pricedMock(pricedStep("again")), Memory: store}
	ag.Resume("s-1", nil, 1, 0.5)
	if _, err := ag.Send(ctx, "continue"); err != nil {
		t.Fatal(err)
	}
	if got, _ := store.SpendUSD(ctx, "reader"); !near(got, 0.60) {
		t.Fatalf("resumed spend = %v, want 0.60 (0.5 stored + 0.10 new)", got)
	}
}

func TestLimitStopsTheTurnBeforeTheModelIsCalled(t *testing.T) {
	ctx := context.Background()
	mock := pricedMock(pricedStep("never"))
	errBudget := errors.New("budget reached")
	ag := &Agent{
		Name:     "reader",
		Provider: mock,
		Memory:   openStore(t),
		Limit:    func(context.Context) error { return errBudget },
	}
	if _, err := ag.Send(ctx, "hi"); !errors.Is(err, errBudget) {
		t.Fatalf("Send error = %v, want the limit error", err)
	}
	if len(mock.Calls) != 0 {
		t.Fatalf("the model was called %d times after the limit", len(mock.Calls))
	}
}
