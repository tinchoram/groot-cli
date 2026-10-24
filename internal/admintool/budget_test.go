package admintool

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/tinchoram/groot-cli/internal/tool"
)

func (f *fakeManager) SetSpaceBudget(string, float64) error { return nil }

func (f *fakeManager) SetAgentBudget(string, float64) error { return nil }

// budgetManager records the budgets the tools save.
type budgetManager struct {
	fakeManager
	spaces map[string]float64
	agents map[string]float64
	err    error
}

func (m *budgetManager) SetSpaceBudget(space string, usd float64) error {
	if m.err != nil {
		return m.err
	}
	if m.spaces == nil {
		m.spaces = map[string]float64{}
	}
	m.spaces[space] = usd
	return nil
}

func (m *budgetManager) SetAgentBudget(agent string, usd float64) error {
	if m.err != nil {
		return m.err
	}
	if m.agents == nil {
		m.agents = map[string]float64{}
	}
	m.agents[agent] = usd
	return nil
}

func TestSetSpaceBudgetSavesAndReports(t *testing.T) {
	m := &budgetManager{}
	tl := NewSetSpaceBudget(m)
	out, isErr := tl.Execute(context.Background(), `{"space":"payments","usd":2.5}`)
	if isErr {
		t.Fatalf("unexpected error: %s", out)
	}
	if m.spaces["payments"] != 2.5 {
		t.Fatalf("saved = %v", m.spaces)
	}
	if !strings.Contains(out, "payments") || !strings.Contains(out, "2.50") {
		t.Fatalf("output must name the space and the budget: %s", out)
	}
	if tl.Risk() != tool.RiskWrite || !strings.Contains(tl.Preview(`{"space":"payments","usd":2.5}`), "payments") {
		t.Fatal("set_space_budget is a write whose preview names the space")
	}
}

func TestSetAgentBudgetSavesAndZeroClears(t *testing.T) {
	m := &budgetManager{}
	if out, isErr := NewSetAgentBudget(m).Execute(context.Background(), `{"agent":"reader","usd":1}`); isErr {
		t.Fatalf("unexpected error: %s", out)
	}
	if m.agents["reader"] != 1 {
		t.Fatalf("saved = %v", m.agents)
	}
	out, isErr := NewSetAgentBudget(m).Execute(context.Background(), `{"agent":"reader","usd":0}`)
	if isErr || m.agents["reader"] != 0 || !strings.Contains(out, "cleared") {
		t.Fatalf("0 must clear the budget: %q (err=%v) saved=%v", out, isErr, m.agents)
	}
}

func TestBudgetToolsRejectNegativeAndMissingValues(t *testing.T) {
	cases := []struct {
		name  string
		tl    tool.Tool
		input string
	}{
		{"negative space budget", NewSetSpaceBudget(&budgetManager{}), `{"space":"payments","usd":-1}`},
		{"negative agent budget", NewSetAgentBudget(&budgetManager{}), `{"agent":"reader","usd":-0.5}`},
		{"space missing", NewSetSpaceBudget(&budgetManager{}), `{"usd":1}`},
		{"agent missing", NewSetAgentBudget(&budgetManager{}), `{"usd":1}`},
		{"unknown field", NewSetAgentBudget(&budgetManager{}), `{"agent":"reader","usd":1,"extra":true}`},
	}
	for _, c := range cases {
		if out, isErr := c.tl.Execute(context.Background(), c.input); !isErr {
			t.Fatalf("%s must be refused: %q", c.name, out)
		}
	}
}

func TestBudgetToolReportsAManagerError(t *testing.T) {
	m := &budgetManager{err: errors.New("no space named \"missing\"")}
	out, isErr := NewSetSpaceBudget(m).Execute(context.Background(), `{"space":"missing","usd":1}`)
	if !isErr || !strings.Contains(out, "missing") {
		t.Fatalf("a refused save must be reported: %q (err=%v)", out, isErr)
	}
}
