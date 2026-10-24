package workspace

import (
	"context"
	"errors"
	"fmt"
	"math"
	"os"
	"slices"

	"github.com/tinchoram/groot-cli/internal/catalog"
	"github.com/tinchoram/groot-cli/internal/memory"
)

// warnShare is the share of a budget spent from which it shows as a warning.
const warnShare = 0.8

// BudgetLevel says how far a budget is spent.
type BudgetLevel int

const (
	BudgetUnlimited BudgetLevel = iota // no limit is set
	BudgetOK                           // under the warning share
	BudgetWarn                         // at or above the warning share
	BudgetExhausted                    // at or above the limit
)

// Budget is one spend limit and the spend it applies to. A space budget covers
// the spend of all its agents; an agent budget covers the agent's spend in the
// space.
type Budget struct {
	Scope string  // "agent" or "space"
	Name  string  // the agent or the space
	Spent float64 // spend so far
	Limit float64 // 0 means no limit
}

// Level reports how the budget stands.
func (b Budget) Level() BudgetLevel {
	switch {
	case b.Limit <= 0:
		return BudgetUnlimited
	case b.Spent >= b.Limit:
		return BudgetExhausted
	case b.Spent >= warnShare*b.Limit:
		return BudgetWarn
	}
	return BudgetOK
}

// Overrun reports whether spending next more would pass the limit.
func (b Budget) Overrun(next float64) bool {
	return b.Limit > 0 && b.Spent+next > b.Limit
}

// exhausted returns the first budget that is spent up to its limit.
func exhausted(bs []Budget) (Budget, bool) {
	for _, b := range bs {
		if b.Level() == BudgetExhausted {
			return b, true
		}
	}
	return Budget{}, false
}

// describe names a budget for a message, such as "agent reader".
func (b Budget) describe() string { return b.Scope + " " + b.Name }

// budgetsFor returns the budgets of an agent in a space, each with the spend it
// stands at now.
func (w *Workspace) budgetsFor(ctx context.Context, store memory.Store, spaceName, agent string) ([]Budget, error) {
	w.mu.Lock()
	sp, _ := w.opts.Spaces.Get(spaceName)
	def, _ := w.def(agent)
	w.mu.Unlock()
	by, err := store.SpendByAgent(ctx)
	if err != nil {
		return nil, fmt.Errorf("read spend: %w", err)
	}
	var total float64
	for _, v := range by {
		total += v
	}
	return []Budget{
		{Scope: "agent", Name: agent, Spent: by[agent], Limit: def.BudgetUSD},
		{Scope: "space", Name: spaceName, Spent: total, Limit: sp.BudgetUSD},
	}, nil
}

// checkBudget refuses a turn when a budget is spent up to its limit, and warns
// once when the next turn, estimated at next, would pass a limit. The warning
// goes to the transcript; the turn still runs.
func (c *Conversation) checkBudget(ctx context.Context, next float64) error {
	if c.budgets == nil {
		return nil
	}
	bs, err := c.budgets(ctx)
	if err != nil {
		return fmt.Errorf("budget check: %w", err)
	}
	if b, ok := exhausted(bs); ok {
		hint := "/budget space <usd>"
		if b.Scope == "agent" {
			hint = "/budget agent " + b.Name + " <usd>"
		}
		return fmt.Errorf("%s budget of %s reached: spent %s of %s · raise it with %s",
			b.Scope, b.Name, FormatUSD(b.Spent), FormatUSD(b.Limit), hint)
	}
	if c.budgetWarned || c.budgetNotice == nil {
		return nil
	}
	for _, b := range bs {
		if b.Overrun(next) {
			c.budgetWarned = true
			c.budgetNotice(fmt.Sprintf("%s budget may be passed by the next turn: spent %s of %s", b.describe(), FormatUSD(b.Spent), FormatUSD(b.Limit)))
			return nil
		}
	}
	return nil
}

// Money is the spend of a space or an agent, and its budget. Budget 0 means none.
type Money struct {
	Spent  float64
	Budget float64
}

// SpendView is the spend that the sidebar and /budget show.
type SpendView struct {
	Spaces map[string]Money // every space; Spent is what all its agents spent
	Agents map[string]Money // the agents of the active space; Spent is their spend in it
}

// Spend reads the spend of every space and of the agents of the active space.
func (w *Workspace) Spend(ctx context.Context) (SpendView, error) {
	w.mu.Lock()
	spaces := w.opts.Spaces.List()
	active, hasActive := w.opts.Spaces.Current()
	defs := slices.Clone(w.defs)
	w.mu.Unlock()

	view := SpendView{Spaces: map[string]Money{}, Agents: map[string]Money{}}
	for _, sp := range spaces {
		// A space whose directory is gone has no ledger to read; opening its
		// store would recreate the directory.
		if _, err := os.Stat(sp.Path); err != nil {
			continue
		}
		w.mu.Lock()
		st, err := w.storesFor(sp.Path)
		w.mu.Unlock()
		if err != nil {
			return SpendView{}, fmt.Errorf("space %s: %w", sp.Name, err)
		}
		by, err := st.mem.SpendByAgent(ctx)
		if err != nil {
			return SpendView{}, fmt.Errorf("space %s: %w", sp.Name, err)
		}
		var total float64
		for _, v := range by {
			total += v
		}
		view.Spaces[sp.Name] = Money{Spent: total, Budget: sp.BudgetUSD}
		if hasActive && sp.Name == active.Name {
			for _, agent := range active.Agents {
				m := Money{Spent: by[agent]}
				for _, d := range defs {
					if d.Name == agent {
						m.Budget = d.BudgetUSD
					}
				}
				view.Agents[agent] = m
			}
		}
	}
	return view, nil
}

// validBudget refuses negative and non-finite amounts. Zero clears a budget.
func validBudget(usd float64) error {
	if usd < 0 || math.IsNaN(usd) || math.IsInf(usd, 0) {
		return errors.New("budget must be a number of dollars, 0 or more")
	}
	return nil
}

// SetSpaceBudget sets the budget of all agents of a space together. Zero clears it.
func (w *Workspace) SetSpaceBudget(spaceName string, usd float64) error {
	if err := validBudget(usd); err != nil {
		return err
	}
	return w.opts.Spaces.SetBudget(spaceName, usd)
}

// SetAgentBudget sets the budget of an agent in every space. It is saved in the
// agent's file; zero clears it. Open conversations see it on their next turn.
func (w *Workspace) SetAgentBudget(agent string, usd float64) error {
	if err := validBudget(usd); err != nil {
		return err
	}
	defs, err := catalog.Load(w.opts.Home)
	if err != nil {
		return err
	}
	for _, d := range defs {
		if d.Name != agent {
			continue
		}
		d.BudgetUSD = usd
		if err := catalog.Save(w.opts.Home, d, d.Prompt); err != nil {
			return err
		}
		return w.reload()
	}
	return fmt.Errorf("unknown agent %q", agent)
}

// SetSpaceBudget and SetAgentBudget let the administrator agent set budgets
// through the admin manager.
func (a adminManager) SetSpaceBudget(spaceName string, usd float64) error {
	return a.w.SetSpaceBudget(spaceName, usd)
}

func (a adminManager) SetAgentBudget(agent string, usd float64) error {
	return a.w.SetAgentBudget(agent, usd)
}
