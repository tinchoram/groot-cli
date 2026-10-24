package ui

import (
	"context"
	"strings"
	"testing"

	"github.com/tinchoram/groot-cli/internal/workspace"
)

// Spend answers with the fake's spend view.
func (f *fakeWorkspace) Spend(context.Context) (workspace.SpendView, error) {
	return f.spend, nil
}

func (f *fakeWorkspace) SetSpaceBudget(name string, usd float64) error {
	if f.budgetErr != nil {
		return f.budgetErr
	}
	if f.budgetSet == nil {
		f.budgetSet = map[string]float64{}
	}
	f.budgetSet["space:"+name] = usd
	return nil
}

func (f *fakeWorkspace) SetAgentBudget(name string, usd float64) error {
	if f.budgetErr != nil {
		return f.budgetErr
	}
	if f.budgetSet == nil {
		f.budgetSet = map[string]float64{}
	}
	f.budgetSet["agent:"+name] = usd
	return nil
}

func TestSpendBadgeFormatsSpendAndBudget(t *testing.T) {
	if got := stripANSI(spendBadge(0.12, 0)); got != " $0.12" {
		t.Fatalf("spend without budget = %q, want \" $0.12\"", got)
	}
	if got := stripANSI(spendBadge(0.9, 2)); got != " $0.90/$2.00" {
		t.Fatalf("spend with budget = %q, want \" $0.90/$2.00\"", got)
	}
	if got := spendBadge(0, 0); got != "" {
		t.Fatalf("no spend and no budget shows nothing: %q", got)
	}
	if got := spendBadge(0, 5); got == "" {
		t.Fatal("a budget with no spend yet is still shown")
	}
}

func TestSpendBadgeColorsByShareOfBudget(t *testing.T) {
	if got := spendBadge(0.5, 1); got != " "+styleDim.Render("$0.50/$1.00") {
		t.Fatalf("under 80 percent uses the dim style: %q", got)
	}
	if got := spendBadge(0.85, 1); got != " "+styleTool.Render("$0.85/$1.00") {
		t.Fatalf("at 80 percent uses the warning style: %q", got)
	}
	if got := spendBadge(1.2, 1); got != " "+styleError.Render("$1.20/$1.00") {
		t.Fatalf("over the budget uses the error style: %q", got)
	}
}

func TestSidebarShowsSpendNextToSpaceAndAgent(t *testing.T) {
	m, ws := newTestModel(t, &fakeSession{model: "m"})
	active, _ := ws.ActiveSpace()
	agent := ws.Agents()[0].Name
	ws.spend = workspace.SpendView{
		Spaces: map[string]workspace.Money{active.Name: {Spent: 1.30}},
		Agents: map[string]workspace.Money{agent: {Spent: 0.90, Budget: 2}},
	}
	m.spend = ws.spend
	out := stripANSI(strings.Join(m.sidebarLines(40, false), "\n"))
	for _, want := range []string{active.Name + " $1.30", agent + " $0.90/$2.00"} {
		if !strings.Contains(out, want) {
			t.Fatalf("sidebar lacks %q:\n%s", want, out)
		}
	}
}

func TestBudgetCommandPrintsSpendAndBudgets(t *testing.T) {
	m, ws := newTestModel(t, &fakeSession{model: "m"})
	active, _ := ws.ActiveSpace()
	agent := ws.Agents()[0].Name
	ws.spend = workspace.SpendView{
		Spaces: map[string]workspace.Money{active.Name: {Spent: 0.5, Budget: 2}},
		Agents: map[string]workspace.Money{agent: {Spent: 0.3}},
	}
	m = sendLine(t, m, "/budget")
	out := stripANSI(m.transcriptText())
	for _, want := range []string{active.Name, "$0.50/$2.00", agent, "$0.30", "no budget"} {
		if !strings.Contains(out, want) {
			t.Fatalf("/budget lacks %q:\n%s", want, out)
		}
	}
}

func TestBudgetCommandSetsAndClears(t *testing.T) {
	m, ws := newTestModel(t, &fakeSession{model: "m"})
	active, _ := ws.ActiveSpace()
	agent := ws.Agents()[0].Name
	m = sendLine(t, m, "/budget space 2")
	m = sendLine(t, m, "/budget agent "+agent+" 0.5")
	m = sendLine(t, m, "/budget agent "+agent+" 0")
	if ws.budgetSet["space:"+active.Name] != 2 {
		t.Fatalf("space budget not set: %v", ws.budgetSet)
	}
	if v, ok := ws.budgetSet["agent:"+agent]; !ok || v != 0 {
		t.Fatalf("0 must clear the agent budget: %v", ws.budgetSet)
	}
}

func TestBudgetCommandRejectsBadValues(t *testing.T) {
	m, ws := newTestModel(t, &fakeSession{model: "m"})
	agent := ws.Agents()[0].Name
	m = sendLine(t, m, "/budget agent "+agent+" -1")
	m = sendLine(t, m, "/budget space abc")
	m = sendLine(t, m, "/budget nonsense")
	if len(ws.budgetSet) != 0 {
		t.Fatalf("bad values must not be saved: %v", ws.budgetSet)
	}
	if !strings.Contains(stripANSI(m.transcriptText()), "usage: /budget") {
		t.Fatalf("bad input must print the usage:\n%s", m.transcriptText())
	}
}
