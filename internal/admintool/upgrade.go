package admintool

import (
	"context"
	"strings"

	"github.com/tinchoram/groot-cli/internal/tool"
)

// NewUpgradeAgent returns the upgrade_agent tool: it adds the managed tools to
// an agent that lacks them. It never removes a tool, and groot is upgraded by
// itself at every start.
func NewUpgradeAgent(m Manager) tool.Tool {
	return upgradeAgent{base{"upgrade_agent",
		"Add the managed tools (messaging, the task flow, recall and remember) to an agent that lacks them. Never removes a tool.",
		`{"type":"object","properties":{"name":{"type":"string"}},"required":["name"],"additionalProperties":false}`,
		tool.RiskWrite}, m}
}

type upgradeInput struct {
	Name string `json:"name"`
}

type upgradeAgent struct {
	base
	m Manager
}

func (t upgradeAgent) Preview(input string) string {
	var in upgradeInput
	if err := decode(input, &in); err != nil {
		return "upgrade agent: invalid input"
	}
	return "upgrade agent " + in.Name
}

func (t upgradeAgent) Execute(_ context.Context, input string) (string, bool) {
	var in upgradeInput
	if err := decode(input, &in); err != nil {
		return err.Error(), true
	}
	if strings.TrimSpace(in.Name) == "" {
		return "name is required", true
	}
	lines, err := t.m.UpgradeAgent(in.Name)
	if err != nil {
		return err.Error(), true
	}
	return strings.Join(lines, "\n"), false
}
