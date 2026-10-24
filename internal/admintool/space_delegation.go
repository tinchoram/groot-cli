package admintool

import (
	"context"
	"fmt"
	"strings"

	"github.com/tinchoram/groot-cli/internal/tool"
)

// NewSetSpaceDelegation returns set_space_delegation: it sets how a space hands a
// task to another agent and which agent tracks the handovers.
func NewSetSpaceDelegation(m Manager) tool.Tool {
	return setSpaceDelegation{base{"set_space_delegation",
		"Set how one space hands a task to another agent: delegation auto (hand over through the inbox), hint (only name the agent) or off, and the manager that tracks the handovers. The manager must be assigned to the space; manager \"none\" removes it. Omitted fields keep their value. Conversations opened from now on use it.",
		`{"type":"object","properties":{"space":{"type":"string"},"mode":{"type":"string","enum":["auto","hint","off"]},"manager":{"type":"string"}},"required":["space"],"additionalProperties":false}`,
		tool.RiskWrite}, m}
}

type spaceDelegationInput struct {
	Space   string `json:"space"`
	Mode    string `json:"mode"`
	Manager string `json:"manager"`
}

type setSpaceDelegation struct {
	base
	m Manager
}

func (t setSpaceDelegation) Preview(input string) string {
	var in spaceDelegationInput
	if err := decode(input, &in); err != nil {
		return "set space delegation: invalid input"
	}
	var parts []string
	if in.Mode != "" {
		parts = append(parts, "mode "+in.Mode)
	}
	if in.Manager != "" {
		parts = append(parts, "manager "+in.Manager)
	}
	if len(parts) == 0 {
		return "no change to the delegation of space " + in.Space
	}
	return "set the delegation of space " + in.Space + ": " + strings.Join(parts, ", ")
}

func (t setSpaceDelegation) Execute(ctx context.Context, input string) (string, bool) {
	var in spaceDelegationInput
	if err := decode(input, &in); err != nil {
		return err.Error(), true
	}
	if in.Space == "" {
		return "space is required", true
	}
	switch in.Mode {
	case "", "auto", "hint", "off":
	default:
		return fmt.Sprintf("mode %q is invalid (use auto, hint or off)", in.Mode), true
	}
	if err := t.m.SetSpaceDelegation(ctx, in.Space, in.Mode, in.Manager); err != nil {
		return result("", err)
	}
	var parts []string
	if in.Mode != "" {
		parts = append(parts, "mode "+in.Mode)
	}
	if in.Manager != "" {
		parts = append(parts, "manager "+in.Manager)
	}
	if len(parts) == 0 {
		return fmt.Sprintf("delegation of space %s unchanged", in.Space), false
	}
	return fmt.Sprintf("delegation of space %s saved: %s", in.Space, strings.Join(parts, ", ")), false
}
