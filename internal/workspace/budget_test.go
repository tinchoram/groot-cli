package workspace

import (
	"context"
	"math"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/tinchoram/groot-cli/internal/memory"
)

func TestBudgetLevels(t *testing.T) {
	cases := []struct {
		name  string
		spent float64
		limit float64
		want  BudgetLevel
	}{
		{"no budget is unlimited", 5, 0, BudgetUnlimited},
		{"well under", 0.5, 2, BudgetOK},
		{"warn band starts at 80 percent", 1.6, 2, BudgetWarn},
		{"exactly the limit is exhausted", 2, 2, BudgetExhausted},
		{"over the limit is exhausted", 2.5, 2, BudgetExhausted},
	}
	for _, c := range cases {
		b := Budget{Scope: "agent", Name: "reader", Spent: c.spent, Limit: c.limit}
		if got := b.Level(); got != c.want {
			t.Fatalf("%s: level = %v, want %v", c.name, got, c.want)
		}
	}
}

func TestBudgetOverrunUsesTheNextTurnEstimate(t *testing.T) {
	if !(Budget{Spent: 1.6, Limit: 2}).Overrun(0.5) {
		t.Fatal("1.6 + 0.5 passes 2")
	}
	if (Budget{Spent: 1, Limit: 2}).Overrun(0.5) {
		t.Fatal("1 + 0.5 does not pass 2")
	}
	if (Budget{Spent: 9, Limit: 0}).Overrun(9) {
		t.Fatal("an unlimited budget is never overrun")
	}
}

func TestExhaustedPicksTheBudgetThatStops(t *testing.T) {
	agent := Budget{Scope: "agent", Name: "reader", Spent: 0.1, Limit: 1}
	space := Budget{Scope: "space", Name: "default", Spent: 3, Limit: 2}
	got, ok := exhausted([]Budget{agent, space})
	if !ok || got.Scope != "space" {
		t.Fatalf("exhausted = %+v, %v; want the space budget", got, ok)
	}
	if _, ok := exhausted([]Budget{agent}); ok {
		t.Fatal("an agent under its budget does not stop")
	}
}

func seedSpend(t *testing.T, f *fixture, agent string, usd float64) {
	t.Helper()
	ctx := context.Background()
	f.ws.mu.Lock()
	st, err := f.ws.storesFor(f.dir)
	f.ws.mu.Unlock()
	if err != nil {
		t.Fatal(err)
	}
	id := "seed-" + agent
	if err := st.mem.StartSession(ctx, memory.Session{ID: id, Agent: agent, StartedAt: time.Now().UTC()}); err != nil {
		t.Fatal(err)
	}
	if err := st.mem.SetSessionCost(ctx, id, usd); err != nil {
		t.Fatal(err)
	}
}

func TestSendRefusesAnExhaustedAgentBudgetWithoutCallingTheModel(t *testing.T) {
	f := newFixture(t, text("never"))
	if err := f.ws.SetAgentBudget("reader", 0.01); err != nil {
		t.Fatal(err)
	}
	seedSpend(t, f, "reader", 0.02)

	_, err := f.ws.Chat().Send(context.Background(), "hi")
	if err == nil || !strings.Contains(err.Error(), "budget") || !strings.Contains(err.Error(), "reader") {
		t.Fatalf("an exhausted budget must refuse the turn and name the agent: %v", err)
	}
	if len(f.mock.Calls) != 0 {
		t.Fatalf("the model was called %d times", len(f.mock.Calls))
	}
}

func TestSpaceBudgetCountsTheSpendOfEveryAgent(t *testing.T) {
	f := newFixture(t, text("never"))
	if err := f.ws.SetSpaceBudget("default", 1); err != nil {
		t.Fatal(err)
	}
	seedSpend(t, f, "ops", 1.2) // another agent of the same space

	_, err := f.ws.Chat().Send(context.Background(), "hi")
	if err == nil || !strings.Contains(err.Error(), "space") {
		t.Fatalf("the space budget must refuse the turn: %v", err)
	}
	if len(f.mock.Calls) != 0 {
		t.Fatal("the model must not be called")
	}
}

func TestSendRunsWithoutBudgetAndWarnsOnceWhenTheNextTurnWouldOverrun(t *testing.T) {
	f := newFixture(t, text("one"), text("two"), text("three"))
	if err := f.ws.SetAgentBudget("reader", 1); err != nil {
		t.Fatal(err)
	}
	seedSpend(t, f, "reader", 0.9)
	conv := f.ws.Chat().(*Conversation)
	conv.lastTurnUSD = 0.2 // the last turn cost 0.2: 0.9 + 0.2 passes 1

	var notices []string
	conv.budgetNotice = func(msg string) { notices = append(notices, msg) }

	ctx := context.Background()
	for _, prompt := range []string{"first", "second"} {
		if _, err := conv.Send(ctx, prompt); err != nil {
			t.Fatalf("a turn that may overrun still runs: %v", err)
		}
	}
	if len(notices) != 1 || !strings.Contains(notices[0], "reader") {
		t.Fatalf("notices = %v, want exactly one naming the agent", notices)
	}
	if len(f.mock.Calls) != 2 {
		t.Fatalf("both turns must reach the model, got %d calls", len(f.mock.Calls))
	}
}

func TestSendWithoutBudgetIsUnchanged(t *testing.T) {
	f := newFixture(t, text("hello"))
	seedSpend(t, f, "reader", 50)
	got, err := f.ws.Chat().Send(context.Background(), "hi")
	if err != nil || got != "hello" {
		t.Fatalf("Send = %q, %v; an agent without a budget is never refused", got, err)
	}
}

func TestSetBudgetsPersistAndZeroClears(t *testing.T) {
	f := newFixture(t, text("ok"))
	if err := f.ws.SetAgentBudget("reader", 2); err != nil {
		t.Fatal(err)
	}
	if err := f.ws.SetSpaceBudget("default", 3); err != nil {
		t.Fatal(err)
	}
	if d, _ := f.ws.def("reader"); d.BudgetUSD != 2 {
		t.Fatalf("agent budget = %v, want 2", d.BudgetUSD)
	}
	if sp, _ := f.spaces.Get("default"); sp.BudgetUSD != 3 {
		t.Fatalf("space budget = %v, want 3", sp.BudgetUSD)
	}
	if err := f.ws.SetAgentBudget("reader", -1); err == nil {
		t.Fatal("a negative budget must be refused")
	}
	if err := f.ws.SetSpaceBudget("missing", 1); err == nil || !strings.Contains(err.Error(), "missing") {
		t.Fatalf("an unknown space must be named: %v", err)
	}
	if err := f.ws.SetAgentBudget("nobody", 1); err == nil {
		t.Fatal("an unknown agent must be refused")
	}
	if err := f.ws.SetAgentBudget("reader", 0); err != nil {
		t.Fatal(err)
	}
	if d, _ := f.ws.def("reader"); d.BudgetUSD != 0 {
		t.Fatalf("0 must clear the agent budget, got %v", d.BudgetUSD)
	}
}

func TestSpendViewSkipsASpaceWhoseDirectoryIsGone(t *testing.T) {
	f := newFixture(t, text("ok"))
	gone := t.TempDir()
	if err := f.spaces.Add("gone", gone, t0); err != nil {
		t.Fatal(err)
	}
	if err := os.RemoveAll(gone); err != nil {
		t.Fatal(err)
	}
	view, err := f.ws.Spend(context.Background())
	if err != nil {
		t.Fatalf("a missing directory must not fail the view: %v", err)
	}
	if _, ok := view.Spaces["gone"]; ok {
		t.Fatal("a space without its directory has no spend to show")
	}
	if _, err := os.Stat(gone); !os.IsNotExist(err) {
		t.Fatalf("the directory must not be recreated: %v", err)
	}
}

func TestSpendViewTotalsTheSpaceAndItsAgents(t *testing.T) {
	f := newFixture(t, text("ok"))
	if err := f.ws.SetAgentBudget("reader", 1); err != nil {
		t.Fatal(err)
	}
	seedSpend(t, f, "reader", 0.3)
	seedSpend(t, f, "ops", 0.2)

	view, err := f.ws.Spend(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if got := view.Spaces["default"]; math.Abs(got.Spent-0.5) > 1e-9 || got.Budget != 0 {
		t.Fatalf("space money = %+v, want 0.5 spent and no budget", got)
	}
	if got := view.Agents["reader"]; math.Abs(got.Spent-0.3) > 1e-9 || got.Budget != 1 {
		t.Fatalf("reader money = %+v, want 0.3 of 1", got)
	}
}
