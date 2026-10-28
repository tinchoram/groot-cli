package admintool

import (
	"context"

	"github.com/tinchoram/groot-cli/internal/tool"
)

// NewSetWorkMode returns the set_work_mode tool: it turns work mode on or off
// for one agent in a space, or for every agent of the space with space_wide.
// Work mode lets the agent write inside its space and run commands that are not
// on the always-ask list, within its limits.
func NewSetWorkMode(m Manager) tool.Tool {
	return setWorkMode{base{"set_work_mode",
		"Turn work mode on or off for an agent in a space, or for every agent of the space when space_wide is true. In work mode the agent writes inside its space and runs commands without asking, within the configured limits; risky commands still ask. Use it when the user wants agents to work unattended in one space. Conversations opened later in a space inherit its space-wide setting.",
		`{"type":"object","properties":{"space":{"type":"string","description":"Space name"},"agent":{"type":"string","description":"Agent name; required unless space_wide is true"},"on":{"type":"boolean","description":"true turns work mode on, false turns it off"},"space_wide":{"type":"boolean","description":"true applies the change to every agent of the space instead of one agent"}},"required":["space","on"],"additionalProperties":false}`,
		tool.RiskWrite}, m}
}

type workInput struct {
	Space     string `json:"space"`
	Agent     string `json:"agent"`
	On        bool   `json:"on"`
	SpaceWide bool   `json:"space_wide"`
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
	if in.SpaceWide {
		return "work mode " + state + " for every agent in " + in.Space
	}
	return "work mode " + state + " for " + in.Agent + " in " + in.Space
}

func (t setWorkMode) Execute(_ context.Context, input string) (string, bool) {
	var in workInput
	if err := decode(input, &in); err != nil {
		return err.Error(), true
	}
	state := "off"
	if in.On {
		state = "on"
	}
	if in.Space == "" || (in.Agent == "" && !in.SpaceWide) {
		return "space and agent are required; the agent may be left out only with space_wide", true
	}
	if in.SpaceWide {
		return result("work mode "+state+" for every agent in "+in.Space, t.m.SetSpaceWorkMode(in.Space, in.On))
	}
	return result("work mode "+state+" for "+in.Agent+" in "+in.Space, t.m.SetWorkMode(in.Space, in.Agent, in.On))
}
