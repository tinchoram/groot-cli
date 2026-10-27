package admintool

import (
	"context"
	"fmt"

	"github.com/tinchoram/groot-cli/internal/tool"
)

// budgetInput is shared by set_space_budget and set_agent_budget. USD is a
// pointer so that a missing value is refused instead of read as "clear".
type budgetInput struct {
	Space string   `json:"space"`
	Agent string   `json:"agent"`
	USD   *float64 `json:"usd"`
}

const budgetSchemaUSD = `"usd":{"type":"number","minimum":0,"description":"Budget in US dollars; 0 clears it"}`

// NewSetSpaceBudget returns set_space_budget: it caps the total spend of all
// agents of one space.
func NewSetSpaceBudget(m Manager) tool.Tool {
	return setSpaceBudget{base{"set_space_budget",
		"Set the spend budget of one space: the total in dollars that all its agents may spend there. Zero clears it. When the budget is spent, its agents stop before their next model call until the budget is raised.",
		`{"type":"object","properties":{"space":{"type":"string","description":"Space whose budget changes"},` + budgetSchemaUSD + `},"required":["space","usd"],"additionalProperties":false}`,
		tool.RiskWrite}, m}
}

type setSpaceBudget struct {
	base
	m Manager
}

func (t setSpaceBudget) Preview(input string) string {
	var in budgetInput
	if err := decode(input, &in); err != nil {
		return "set space budget: invalid input"
	}
	if in.USD != nil && *in.USD == 0 {
		return "clear the budget of space " + in.Space
	}
	return fmt.Sprintf("set the budget of space %s to $%.2f", in.Space, derefUSD(in.USD))
}

func (t setSpaceBudget) Execute(_ context.Context, input string) (string, bool) {
	var in budgetInput
	if err := decode(input, &in); err != nil {
		return err.Error(), true
	}
	if in.Space == "" {
		return "space is required", true
	}
	if in.USD == nil || *in.USD < 0 {
		return "usd is required and cannot be negative (0 clears the budget)", true
	}
	if err := t.m.SetSpaceBudget(in.Space, *in.USD); err != nil {
		return err.Error(), true
	}
	if *in.USD == 0 {
		return fmt.Sprintf("budget of space %s cleared", in.Space), false
	}
	return fmt.Sprintf("budget of space %s set to $%.2f", in.Space, *in.USD), false
}

// NewSetAgentBudget returns set_agent_budget: it caps the spend of one agent in
// every space it works in.
func NewSetAgentBudget(m Manager) tool.Tool {
	return setAgentBudget{base{"set_agent_budget",
		"Set the spend budget of one agent, in dollars, counted separately in each space. Zero clears it. When the agent's budget is spent in a space, the agent stops there before its next model call until the budget is raised.",
		`{"type":"object","properties":{"agent":{"type":"string","description":"Agent whose budget changes"},` + budgetSchemaUSD + `},"required":["agent","usd"],"additionalProperties":false}`,
		tool.RiskWrite}, m}
}

type setAgentBudget struct {
	base
	m Manager
}

func (t setAgentBudget) Preview(input string) string {
	var in budgetInput
	if err := decode(input, &in); err != nil {
		return "set agent budget: invalid input"
	}
	if in.USD != nil && *in.USD == 0 {
		return "clear the budget of agent " + in.Agent
	}
	return fmt.Sprintf("set the budget of agent %s to $%.2f", in.Agent, derefUSD(in.USD))
}

func (t setAgentBudget) Execute(_ context.Context, input string) (string, bool) {
	var in budgetInput
	if err := decode(input, &in); err != nil {
		return err.Error(), true
	}
	if in.Agent == "" {
		return "agent is required", true
	}
	if in.USD == nil || *in.USD < 0 {
		return "usd is required and cannot be negative (0 clears the budget)", true
	}
	if err := t.m.SetAgentBudget(in.Agent, *in.USD); err != nil {
		return err.Error(), true
	}
	if *in.USD == 0 {
		return fmt.Sprintf("budget of agent %s cleared", in.Agent), false
	}
	return fmt.Sprintf("budget of agent %s set to $%.2f", in.Agent, *in.USD), false
}

func derefUSD(v *float64) float64 {
	if v == nil {
		return 0
	}
	return *v
}
