package admintool

import (
	"context"
	"fmt"
	"strings"

	"github.com/tinchoram/groot-cli/internal/space"
	"github.com/tinchoram/groot-cli/internal/tool"
)

// NewSetSpaceRoute returns set_space_route: it sets the model and agent a task
// size runs on in one space.
func NewSetSpaceRoute(m Manager) tool.Tool {
	return setSpaceRoute{base{"set_space_route",
		"Set the model and agent that a task size (small, medium or large) runs on in one space, overriding the global routes. An empty model or agent clears that field. The model id is checked against the provider's list when that list can be reached. Conversations opened from now on use it; running ones keep their route.",
		`{"type":"object","properties":{"space":{"type":"string"},"size":{"type":"string","enum":["small","medium","large"]},"model":{"type":"string"},"agent":{"type":"string"}},"required":["space","size"],"additionalProperties":false}`,
		tool.RiskWrite}, m}
}

type spaceRouteInput struct {
	Space string `json:"space"`
	Size  string `json:"size"`
	Model string `json:"model"`
	Agent string `json:"agent"`
}

type setSpaceRoute struct {
	base
	m Manager
}

func (t setSpaceRoute) Preview(input string) string {
	var in spaceRouteInput
	if err := decode(input, &in); err != nil {
		return "set space route: invalid input"
	}
	if in.Model == "" && in.Agent == "" {
		return fmt.Sprintf("clear the %s route of space %s", in.Size, in.Space)
	}
	return fmt.Sprintf("set the %s route of space %s to %s", in.Size, in.Space, describeRoute(in.Model, in.Agent))
}

func (t setSpaceRoute) Execute(ctx context.Context, input string) (string, bool) {
	var in spaceRouteInput
	if err := decode(input, &in); err != nil {
		return err.Error(), true
	}
	if in.Space == "" || in.Size == "" {
		return "space and size are required", true
	}
	if !space.ValidSize(in.Size) {
		return fmt.Sprintf("size %q is invalid (use small, medium or large)", in.Size), true
	}
	warn, err := t.m.SetSpaceRoute(ctx, in.Space, in.Size, in.Model, in.Agent)
	if err != nil {
		return result("", err)
	}
	state := "cleared"
	if in.Model != "" || in.Agent != "" {
		state = "set to " + describeRoute(in.Model, in.Agent)
	}
	out := fmt.Sprintf("route of %s %s %s", in.Space, in.Size, state)
	if warn != "" {
		out += "\n" + warn
	}
	return out, false
}

// describeRoute names the fields of a route that are set.
func describeRoute(model, agent string) string {
	var parts []string
	if model != "" {
		parts = append(parts, "model "+model)
	}
	if agent != "" {
		parts = append(parts, "agent "+agent)
	}
	return strings.Join(parts, ", ")
}

// NewSetSpaceModel returns set_space_model: it sets the default model of the
// agents in one space that set none of their own.
func NewSetSpaceModel(m Manager) tool.Tool {
	return setSpaceModel{base{"set_space_model",
		"Set the default model of the agents in one space that set no model of their own. An empty model clears it. The model id is checked against the provider's list when that list can be reached. Conversations opened from now on use it; agents with their own model keep theirs.",
		`{"type":"object","properties":{"space":{"type":"string"},"model":{"type":"string"}},"required":["space"],"additionalProperties":false}`,
		tool.RiskWrite}, m}
}

type spaceModelInput struct {
	Space string `json:"space"`
	Model string `json:"model"`
}

type setSpaceModel struct {
	base
	m Manager
}

func (t setSpaceModel) Preview(input string) string {
	var in spaceModelInput
	if err := decode(input, &in); err != nil {
		return "set space model: invalid input"
	}
	if in.Model == "" {
		return "clear the default model of space " + in.Space
	}
	return "set the default model of space " + in.Space + " to " + in.Model
}

func (t setSpaceModel) Execute(ctx context.Context, input string) (string, bool) {
	var in spaceModelInput
	if err := decode(input, &in); err != nil {
		return err.Error(), true
	}
	if in.Space == "" {
		return "space is required", true
	}
	warn, err := t.m.SetSpaceModel(ctx, in.Space, in.Model)
	if err != nil {
		return result("", err)
	}
	out := fmt.Sprintf("default model of space %s cleared", in.Space)
	if in.Model != "" {
		out = fmt.Sprintf("default model of space %s set to %s", in.Space, in.Model)
	}
	if warn != "" {
		out += "\n" + warn
	}
	return out, false
}
