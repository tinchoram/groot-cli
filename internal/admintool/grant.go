package admintool

import (
	"context"
	"fmt"

	"github.com/tinchoram/groot-cli/internal/tool"
)

const toolEditSchema = `{"type":"object","properties":{"agent":{"type":"string"},"tool":{"type":"string"}},"required":["agent","tool"],"additionalProperties":false}`

// NewGrantTool returns grant_tool: it gives an agent one tool. Admin tools are
// granted the same way, but only to agents the user has not protected.
func NewGrantTool(m Manager) tool.Tool {
	return toolEdit{base{"grant_tool",
		"Give an agent one tool it does not have yet. The change is saved in the agent's file and applies right away, including its open conversations, which keep their session.",
		toolEditSchema, tool.RiskWrite}, m, true}
}

// NewRevokeTool returns revoke_tool: it takes one tool away from an agent. The
// required tools of the managed agents cannot be taken away.
func NewRevokeTool(m Manager) tool.Tool {
	return toolEdit{base{"revoke_tool",
		"Take one tool away from an agent. The change is saved in the agent's file and applies right away, including its open conversations, which keep their session.",
		toolEditSchema, tool.RiskWrite}, m, false}
}

type toolEditInput struct {
	Agent string `json:"agent"`
	Tool  string `json:"tool"`
}

type toolEdit struct {
	base
	m     Manager
	grant bool
}

func (t toolEdit) Preview(input string) string {
	var in toolEditInput
	if err := decode(input, &in); err != nil {
		return "tool change: invalid input"
	}
	if t.grant {
		return "grant " + in.Tool + " to " + in.Agent
	}
	return "revoke " + in.Tool + " from " + in.Agent
}

func (t toolEdit) Execute(_ context.Context, input string) (string, bool) {
	var in toolEditInput
	if err := decode(input, &in); err != nil {
		return err.Error(), true
	}
	if in.Agent == "" || in.Tool == "" {
		return "agent and tool are required", true
	}
	var add, remove []string
	if t.grant {
		add = []string{in.Tool}
	} else {
		remove = []string{in.Tool}
	}
	if err := t.m.EditAgentTools(in.Agent, add, remove); err != nil {
		return err.Error(), true
	}
	if t.grant {
		return fmt.Sprintf("granted %s to %s", in.Tool, in.Agent), false
	}
	return fmt.Sprintf("revoked %s from %s", in.Tool, in.Agent), false
}
