package admintool

import (
	"context"
	"fmt"

	"github.com/tinchoram/groot-cli/internal/tool"
)

const globalMemorySchema = `{"type":"object","properties":{"agent":{"type":"string"}},"required":["agent"],"additionalProperties":false}`

// NewGrantGlobalMemoryTool returns grant_global_memory: it lets an agent read the
// global memory of the root database. The change is saved in the agent's file and
// applies to its open conversations right away.
func NewGrantGlobalMemoryTool(m Manager) tool.Tool {
	return globalMemoryTool{base{"grant_global_memory",
		"Let an agent read global memory (entries that groot keeps for every space). The change is saved in the agent's file and applies right away, including its open conversations.",
		globalMemorySchema, tool.RiskWrite}, m, true}
}

// NewRevokeGlobalMemoryTool returns revoke_global_memory: it takes global memory
// away from an agent. groot always reads it and cannot lose it.
func NewRevokeGlobalMemoryTool(m Manager) tool.Tool {
	return globalMemoryTool{base{"revoke_global_memory",
		"Stop an agent from reading global memory. The change is saved in the agent's file and applies right away, including its open conversations. groot always reads global memory.",
		globalMemorySchema, tool.RiskWrite}, m, false}
}

type globalMemoryInput struct {
	Agent string `json:"agent"`
}

type globalMemoryTool struct {
	base
	m  Manager
	on bool
}

func (t globalMemoryTool) Preview(input string) string {
	var in globalMemoryInput
	if err := decode(input, &in); err != nil {
		return "global memory: invalid input"
	}
	if t.on {
		return "global memory on for " + in.Agent
	}
	return "global memory off for " + in.Agent
}

func (t globalMemoryTool) Execute(_ context.Context, input string) (string, bool) {
	var in globalMemoryInput
	if err := decode(input, &in); err != nil {
		return err.Error(), true
	}
	if in.Agent == "" {
		return "agent is required", true
	}
	if err := t.m.SetAgentGlobalMemory(in.Agent, t.on); err != nil {
		return err.Error(), true
	}
	if t.on {
		return fmt.Sprintf("global memory enabled for %s", in.Agent), false
	}
	return fmt.Sprintf("global memory disabled for %s", in.Agent), false
}
