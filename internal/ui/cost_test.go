package ui

import (
	"strings"
	"testing"

	"github.com/tinchoram/groot-cli/internal/api"
	"github.com/tinchoram/groot-cli/internal/workspace"
)

func costSession() *fakeSession {
	return &fakeSession{
		model: "m",
		usage: api.Usage{InputTokens: 1200, OutputTokens: 300},
		cost:  0.0123,
	}
}

func TestStatusLineHidesCostByDefault(t *testing.T) {
	m, _ := newTestModel(t, costSession())
	out := stripANSI(m.statusLine())
	if strings.Contains(out, "$") || strings.Contains(out, "~") {
		t.Fatalf("status line shows cost by default: %q", out)
	}
	if !strings.Contains(out, "1,200 in") {
		t.Fatalf("status line must keep the tokens: %q", out)
	}
}

func TestStatusLineShowsCostWhenConfigured(t *testing.T) {
	m, _ := newTestModel(t, costSession())
	m.showCost = true
	out := stripANSI(m.statusLine())
	if !strings.Contains(out, "~$0.0123") {
		t.Fatalf("status line with ui.show_cost lacks the cost: %q", out)
	}
}

func TestSidebarSpendBadgeNeedsABudget(t *testing.T) {
	m, ws := newTestModel(t, costSession())
	agent := ws.Agents()[0].Name
	m.spend = workspace.SpendView{Agents: map[string]workspace.Money{agent: {Spent: 0.90}}}
	if got := m.agentSpendBadge(agent); got != "" {
		t.Fatalf("spend without a budget must not show: %q", got)
	}
	m.spend = workspace.SpendView{Agents: map[string]workspace.Money{agent: {Spent: 0.90, Budget: 2}}}
	if got := stripANSI(m.agentSpendBadge(agent)); got != " $0.90/$2.00" {
		t.Fatalf("budgeted spend badge = %q, want \" $0.90/$2.00\"", got)
	}
}

func TestSidebarSpendBadgeWithCostConfigShowsSpend(t *testing.T) {
	m, ws := newTestModel(t, costSession())
	agent := ws.Agents()[0].Name
	m.showCost = true
	m.spend = workspace.SpendView{Agents: map[string]workspace.Money{agent: {Spent: 0.90}}}
	if got := stripANSI(m.agentSpendBadge(agent)); got != " $0.90" {
		t.Fatalf("with ui.show_cost the spend shows: %q", got)
	}
}

func TestCostCommandPrintsSessionSpendAndBudgets(t *testing.T) {
	m, ws := newTestModel(t, costSession())
	active, _ := ws.ActiveSpace()
	agent := ws.Agents()[0].Name
	ws.spend = workspace.SpendView{
		Spaces: map[string]workspace.Money{active.Name: {Spent: 0.5, Budget: 2}},
		Agents: map[string]workspace.Money{agent: {Spent: 0.3}},
	}
	m = sendLine(t, m, "/cost")
	out := stripANSI(m.transcriptText())
	for _, want := range []string{"session cost $0.0123", active.Name, "$0.50/$2.00", agent, "$0.30", "no budget"} {
		if !strings.Contains(out, want) {
			t.Fatalf("/cost lacks %q:\n%s", want, out)
		}
	}
}

func TestUsageCommandKeepsItsCost(t *testing.T) {
	m, _ := newTestModel(t, costSession())
	m = sendLine(t, m, "/tokens")
	if out := stripANSI(m.transcriptText()); !strings.Contains(out, "$0.0123") {
		t.Fatalf("/tokens is explicit and must keep the cost:\n%s", out)
	}
}
