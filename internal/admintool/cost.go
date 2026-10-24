package admintool

import (
	"context"

	"github.com/tinchoram/groot-cli/internal/tool"
)

// NewGetCost returns get_cost: the spend of a space and of its agents, with
// their budgets. It is read-only, so work mode does not ask before it.
func NewGetCost(m Manager) tool.Tool {
	return getCost{base{"get_cost",
		"Read the spend in dollars of a space and of each of its agents, with their budgets. Omit space for the active space. Use it to answer how much was spent.",
		`{"type":"object","properties":{"space":{"type":"string","description":"space name; omit for the active space"}},"additionalProperties":false}`,
		tool.RiskRead}, m}
}

type getCost struct {
	base
	m Manager
}

func (t getCost) Preview(input string) string {
	var in contextInput
	if err := decode(input, &in); err != nil || in.Space == "" {
		return "read the spend of the active space"
	}
	return "read the spend of space " + in.Space
}

func (t getCost) Execute(ctx context.Context, input string) (string, bool) {
	var in contextInput
	if err := decode(input, &in); err != nil {
		return err.Error(), true
	}
	out, err := t.m.CostReport(ctx, in.Space)
	if err != nil {
		return err.Error(), true
	}
	return out, false
}
