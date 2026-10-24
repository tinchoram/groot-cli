package admintool

import (
	"context"

	"github.com/tinchoram/groot-cli/internal/tool"
)

// NewSetWorkMode returns the set_work_mode tool: it turns work mode on or off
// for one agent in a space. Work mode lets the agent write inside its space and
// run commands that are not on the always-ask list, within its limits.
func NewSetWorkMode(m Manager) tool.Tool {
	return setWorkMode{base{"set_work_mode",
		"Turn work mode on or off for an agent in a space. In work mode the agent writes inside its space and runs commands without asking, within the configured limits; risky commands still ask.",
		`{"type":"object","properties":{"space":{"type":"string"},"agent":{"type":"string"},"on":{"type":"boolean"}},"required":["space","agent","on"],"additionalProperties":false}`,
		tool.RiskWrite}, m}
}

type workInput struct {
	Space string `json:"space"`
	Agent string `json:"agent"`
	On    bool   `json:"on"`
}

type setWorkMode struct {
	base
	m Manager
}

func (t setWorkMode) Preview(input string) string {
	var in workInput
	if err := decode(input, &in); err != nil {
		return "set work mode: invalid input"
	}
	state := "off"
	if in.On {
		state = "on"
	}
	return "work mode " + state + " for " + in.Agent + " in " + in.Space
}

func (t setWorkMode) Execute(_ context.Context, input string) (string, bool) {
	var in workInput
	if err := decode(input, &in); err != nil {
		return err.Error(), true
	}
	if in.Space == "" || in.Agent == "" {
		return "space and agent are required", true
	}
	state := "off"
	if in.On {
		state = "on"
	}
	return result("work mode "+state+" for "+in.Agent+" in "+in.Space, t.m.SetWorkMode(in.Space, in.Agent, in.On))
}
