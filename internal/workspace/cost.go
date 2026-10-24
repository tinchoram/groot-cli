package workspace

import (
	"context"
	"errors"
	"fmt"
	"os"
	"slices"
	"strings"

	"github.com/tinchoram/groot-cli/internal/space"
)

// CostReport is the spend of a space and of each of its agents, with their
// budgets, as text. An empty name means the active space.
func (w *Workspace) CostReport(ctx context.Context, name string) (string, error) {
	w.mu.Lock()
	var (
		sp space.Space
		ok bool
	)
	if name == "" {
		sp, ok = w.opts.Spaces.Current()
		if !ok {
			w.mu.Unlock()
			return "", errors.New("no space selected: use /space use <name>")
		}
	} else {
		sp, ok = w.opts.Spaces.Get(name)
		if !ok {
			w.mu.Unlock()
			return "", fmt.Errorf("unknown space %q", name)
		}
	}
	w.mu.Unlock()

	// A space whose directory is gone has no ledger; opening it would recreate the directory.
	if _, err := os.Stat(sp.Path); err != nil {
		return fmt.Sprintf("space %s: no spend recorded (its directory is missing)", sp.Name), nil
	}
	w.mu.Lock()
	st, err := w.storesFor(sp.Path)
	defs := slices.Clone(w.defs)
	w.mu.Unlock()
	if err != nil {
		return "", fmt.Errorf("space %s: %w", sp.Name, err)
	}
	by, err := st.mem.SpendByAgent(ctx)
	if err != nil {
		return "", fmt.Errorf("space %s: %w", sp.Name, err)
	}
	var total float64
	for _, v := range by {
		total += v
	}

	var sb strings.Builder
	fmt.Fprintf(&sb, "space %s: %s\n", sp.Name, moneyText(total, sp.BudgetUSD))
	for _, agent := range sp.Agents {
		var budget float64
		for _, d := range defs {
			if d.Name == agent {
				budget = d.BudgetUSD
			}
		}
		fmt.Fprintf(&sb, "  %s: %s\n", agent, moneyText(by[agent], budget))
	}
	return strings.TrimRight(sb.String(), "\n"), nil
}

// moneyText is "$0.30 of $2.00" with a budget, or "$0.30 (no budget)" without.
func moneyText(spent, budget float64) string {
	if budget <= 0 {
		return fmt.Sprintf("$%.2f (no budget)", spent)
	}
	return fmt.Sprintf("$%.2f of $%.2f", spent, budget)
}

// CostReport lets the administrator agent read the spend through the admin manager.
func (a adminManager) CostReport(ctx context.Context, space string) (string, error) {
	return a.w.CostReport(ctx, space)
}
