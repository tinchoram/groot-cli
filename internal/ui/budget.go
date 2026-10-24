package ui

import (
	"context"
	"fmt"
	"maps"
	"math"
	"slices"
	"strconv"
	"strings"

	"github.com/tinchoram/groot-cli/internal/workspace"
)

// budgetUsage is the reply to a /budget command that does not parse.
const budgetUsage = "usage: /budget | /budget space <usd> | /budget agent <name> <usd>   (0 clears a budget)"

// spendBadge is the spend shown after a space or agent name: "$0.12", or
// "$0.90/$2.00" when a budget is set. It is amber from 80 percent of the budget
// and red from the budget itself. It shows nothing without spend or budget.
func spendBadge(spent, budget float64) string {
	if spent <= 0 && budget <= 0 {
		return ""
	}
	text := fmt.Sprintf("$%.2f", spent)
	if budget > 0 {
		text += fmt.Sprintf("/$%.2f", budget)
	}
	style := styleDim
	if budget > 0 {
		switch share := spent / budget; {
		case share >= 1:
			style = styleError
		case share >= 0.8:
			style = styleTool
		}
	}
	return " " + style.Render(text)
}

// spaceSpendBadge is the spend badge of a space in the sidebar.
func (m Model) spaceSpendBadge(space string) string {
	mo := m.spend.Spaces[space]
	return spendBadge(mo.Spent, mo.Budget)
}

// agentSpendBadge is the spend badge of an agent of the active space.
func (m Model) agentSpendBadge(agent string) string {
	mo := m.spend.Agents[agent]
	return spendBadge(mo.Spent, mo.Budget)
}

// refreshSpend re-reads the spend behind the sidebar badges and /budget.
func (m *Model) refreshSpend() {
	view, err := m.ws.Spend(context.Background())
	if err != nil {
		m.spend = workspace.SpendView{}
		return
	}
	m.spend = view
}

// runBudgetCommand handles /budget, /budget space <usd> and /budget agent <name> <usd>.
func (m *Model) runBudgetCommand(arg string) {
	fields := strings.Fields(arg)
	switch {
	case len(fields) == 0:
		m.refreshSpend()
		m.append(m.budgetBlock())
	case fields[0] == "space" && len(fields) == 2:
		usd, ok := parseBudget(fields[1])
		if !ok {
			m.append(styleError.Render(budgetUsage))
			return
		}
		active, ok := m.ws.ActiveSpace()
		if !ok {
			m.append(styleError.Render("no space selected: use /space use <name>"))
			return
		}
		if err := m.ws.SetSpaceBudget(active.Name, usd); err != nil {
			m.append(styleError.Render("error: " + err.Error()))
			return
		}
		m.append(styleDim.Render(budgetSetLine("space "+active.Name, usd)))
	case fields[0] == "agent" && len(fields) == 3:
		usd, ok := parseBudget(fields[2])
		if !ok {
			m.append(styleError.Render(budgetUsage))
			return
		}
		if err := m.ws.SetAgentBudget(fields[1], usd); err != nil {
			m.append(styleError.Render("error: " + err.Error()))
			return
		}
		m.append(styleDim.Render(budgetSetLine("agent "+fields[1], usd)))
	default:
		m.append(styleError.Render(budgetUsage))
		return
	}
	m.refreshSpend()
}

// parseBudget reads a budget in dollars: a number of 0 or more.
func parseBudget(s string) (float64, bool) {
	v, err := strconv.ParseFloat(s, 64)
	if err != nil || v < 0 || math.IsNaN(v) || math.IsInf(v, 0) {
		return 0, false
	}
	return v, true
}

func budgetSetLine(what string, usd float64) string {
	if usd == 0 {
		return fmt.Sprintf("budget of %s cleared", what)
	}
	return fmt.Sprintf("budget of %s set to $%.2f", what, usd)
}

// budgetBlock lists the spend and budget of the active space and its agents.
func (m Model) budgetBlock() string {
	active, ok := m.ws.ActiveSpace()
	if !ok {
		return styleError.Render("no space selected: use /space use <name>")
	}
	var sb strings.Builder
	sb.WriteString(styleDim.Render("budgets of space "+active.Name+":") + "\n")
	space := m.spend.Spaces[active.Name]
	fmt.Fprintf(&sb, "  %-14s %s\n", "space", budgetText(space))
	for _, agent := range slices.Sorted(maps.Keys(m.spend.Agents)) {
		fmt.Fprintf(&sb, "  %-14s %s\n", agent, budgetText(m.spend.Agents[agent]))
	}
	return strings.TrimRight(sb.String(), "\n")
}

// budgetText is "$0.90/$2.00" with a budget, or "$0.30 (no budget)" without.
func budgetText(mo workspace.Money) string {
	if mo.Budget <= 0 {
		return fmt.Sprintf("$%.2f (no budget)", mo.Spent)
	}
	return fmt.Sprintf("$%.2f/$%.2f", mo.Spent, mo.Budget)
}
