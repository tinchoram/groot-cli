package admintool

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/tinchoram/groot-cli/internal/tool"
)

// expandPath turns an absolute path or one starting with ~ into an absolute
// path. home expands "~".
func expandPath(p, home string) (string, error) {
	switch {
	case p == "~" || strings.HasPrefix(p, "~/"):
		if home == "" {
			return "", errors.New("cannot expand ~: home directory unknown")
		}
		return filepath.Join(home, strings.TrimPrefix(p, "~")), nil
	case filepath.IsAbs(p):
		return filepath.Clean(p), nil
	}
	return "", fmt.Errorf("path %q must be absolute or start with ~", p)
}

// The six tools below change spaces and agents. They are all writes: the user
// confirms each call inline, and the call is audited. Each result is the state
// the workspace read back from disk.

// nameOnly is the input of the tools that take one name.
type nameOnly struct {
	Name string `json:"name"`
}

// ---- delete_space ----

type deleteSpace struct {
	base
	m Manager
}

// NewDeleteSpace returns the delete_space tool.
func NewDeleteSpace(m Manager) tool.Tool {
	return deleteSpace{base{"delete_space",
		"Delete a space from spaces.yaml. Its directory and its .groot data stay on disk. The root space cannot be deleted. Deleting the active space switches to root.",
		`{"type":"object","properties":{"name":{"type":"string","description":"the space to delete"}},"required":["name"],"additionalProperties":false}`,
		tool.RiskWrite}, m}
}

func (t deleteSpace) Preview(input string) string {
	var in nameOnly
	if err := decode(input, &in); err != nil {
		return "delete space: invalid input"
	}
	return fmt.Sprintf("delete space %s from spaces.yaml (its directory and .groot data are kept)", in.Name)
}

func (t deleteSpace) Execute(_ context.Context, input string) (string, bool) {
	var in nameOnly
	if err := decode(input, &in); err != nil {
		return err.Error(), true
	}
	if strings.TrimSpace(in.Name) == "" {
		return "name is required", true
	}
	return result(t.m.DeleteSpace(in.Name))
}

// ---- edit_space ----

type editSpaceInput struct {
	Name    string `json:"name"`
	NewName string `json:"new_name"`
	Path    string `json:"path"`
}

type editSpace struct {
	base
	m    Manager
	home string
}

// NewEditSpace returns the edit_space tool.
func NewEditSpace(m Manager, home string) tool.Tool {
	return editSpace{base{"edit_space",
		"Rename a space and/or point it at another directory. The root space cannot be renamed. A rename keeps the selection and the unread messages. A new path does not move the .groot data under the old path.",
		`{"type":"object","properties":{"name":{"type":"string","description":"the space to edit"},"new_name":{"type":"string","description":"lowercase slug; optional"},"path":{"type":"string","description":"existing directory, absolute or starting with ~; optional"}},"required":["name"],"additionalProperties":false}`,
		tool.RiskWrite}, m, home}
}

func (t editSpace) Preview(input string) string {
	var in editSpaceInput
	if err := decode(input, &in); err != nil {
		return "edit space: invalid input"
	}
	var parts []string
	if in.NewName != "" {
		parts = append(parts, fmt.Sprintf("rename space %s to %s", in.Name, in.NewName))
	}
	if in.Path != "" {
		parts = append(parts, fmt.Sprintf("set the path of space %s to %s", in.Name, in.Path))
	}
	if len(parts) == 0 {
		return fmt.Sprintf("edit space %s: nothing to change", in.Name)
	}
	return strings.Join(parts, "; ")
}

func (t editSpace) Execute(_ context.Context, input string) (string, bool) {
	var in editSpaceInput
	if err := decode(input, &in); err != nil {
		return err.Error(), true
	}
	if strings.TrimSpace(in.Name) == "" {
		return "name is required", true
	}
	if in.NewName == "" && in.Path == "" {
		return "give new_name or path: nothing to change", true
	}
	path := ""
	if in.Path != "" {
		p, err := expandPath(in.Path, t.home)
		if err != nil {
			return err.Error(), true
		}
		path = p
	}
	return result(t.m.EditSpace(in.Name, in.NewName, path))
}

// ---- delete_agent ----

type deleteAgent struct {
	base
	m Manager
}

// NewDeleteAgent returns the delete_agent tool.
func NewDeleteAgent(m Manager) tool.Tool {
	return deleteAgent{base{"delete_agent",
		"Delete an agent from the catalog and unassign it from every space. Its open conversations end; its memory and sessions stay in each space's database. Managed agents (groot, debug) cannot be deleted.",
		`{"type":"object","properties":{"name":{"type":"string","description":"the agent to delete"}},"required":["name"],"additionalProperties":false}`,
		tool.RiskWrite}, m}
}

func (t deleteAgent) Preview(input string) string {
	var in nameOnly
	if err := decode(input, &in); err != nil {
		return "delete agent: invalid input"
	}
	return fmt.Sprintf("delete agent %s from the catalog and from every space", in.Name)
}

func (t deleteAgent) Execute(_ context.Context, input string) (string, bool) {
	var in nameOnly
	if err := decode(input, &in); err != nil {
		return err.Error(), true
	}
	if strings.TrimSpace(in.Name) == "" {
		return "name is required", true
	}
	return result(t.m.DeleteAgent(in.Name))
}

// ---- edit_agent ----

type editAgentInput struct {
	Name        string  `json:"name"`
	NewName     string  `json:"new_name"`
	Description *string `json:"description"`
	Prompt      *string `json:"prompt"`
}

type editAgent struct {
	base
	m Manager
}

// NewEditAgent returns the edit_agent tool.
func NewEditAgent(m Manager) tool.Tool {
	return editAgent{base{"edit_agent",
		"Change an agent's description or prompt, or rename it. Keeps its model, tools and budget. Managed agents (groot, debug) keep their name; only their description and prompt can change.",
		`{"type":"object","properties":{"name":{"type":"string","description":"the agent to edit"},"new_name":{"type":"string","description":"new slug name; optional"},"description":{"type":"string","description":"one-line description; optional"},"prompt":{"type":"string","description":"the full new system prompt; optional"}},"required":["name"],"additionalProperties":false}`,
		tool.RiskWrite}, m}
}

func (t editAgent) Preview(input string) string {
	var in editAgentInput
	if err := decode(input, &in); err != nil {
		return "edit agent: invalid input"
	}
	var parts []string
	if in.NewName != "" {
		parts = append(parts, fmt.Sprintf("rename agent %s to %s", in.Name, in.NewName))
	}
	if in.Description != nil {
		parts = append(parts, "description: "+*in.Description)
	}
	if in.Prompt != nil {
		parts = append(parts, fmt.Sprintf("prompt replaced (%d characters)", len(*in.Prompt)))
	}
	if len(parts) == 0 {
		return fmt.Sprintf("edit agent %s: nothing to change", in.Name)
	}
	return fmt.Sprintf("edit agent %s\n%s", in.Name, strings.Join(parts, "\n"))
}

func (t editAgent) Execute(_ context.Context, input string) (string, bool) {
	var in editAgentInput
	if err := decode(input, &in); err != nil {
		return err.Error(), true
	}
	if strings.TrimSpace(in.Name) == "" {
		return "name is required", true
	}
	if in.NewName == "" && in.Description == nil && in.Prompt == nil {
		return "give description, prompt or new_name: nothing to change", true
	}
	return result(t.m.EditAgent(AgentEdit{Name: in.Name, NewName: in.NewName, Description: in.Description, Prompt: in.Prompt}))
}

// ---- move_agent ----

type moveInput struct {
	Agent string `json:"agent"`
	From  string `json:"from"`
	To    string `json:"to"`
}

type moveAgent struct {
	base
	m Manager
}

// NewMoveAgent returns the move_agent tool.
func NewMoveAgent(m Manager) tool.Tool {
	return moveAgent{base{"move_agent",
		"Move an agent from one space to another in one step. The agent must be assigned to the source space and not yet to the target. The conversation in the source space ends; its sessions stay stored.",
		`{"type":"object","properties":{"agent":{"type":"string"},"from":{"type":"string","description":"space the agent leaves"},"to":{"type":"string","description":"space the agent joins"}},"required":["agent","from","to"],"additionalProperties":false}`,
		tool.RiskWrite}, m}
}

func (t moveAgent) Preview(input string) string {
	var in moveInput
	if err := decode(input, &in); err != nil {
		return "move agent: invalid input"
	}
	return fmt.Sprintf("move agent %s from space %s to space %s", in.Agent, in.From, in.To)
}

func (t moveAgent) Execute(_ context.Context, input string) (string, bool) {
	var in moveInput
	if err := decode(input, &in); err != nil {
		return err.Error(), true
	}
	if in.Agent == "" || in.From == "" || in.To == "" {
		return "agent, from and to are required", true
	}
	return result(t.m.MoveAgent(in.Agent, in.From, in.To))
}

// ---- copy_agent ----

type copyInput struct {
	Agent   string `json:"agent"`
	NewName string `json:"new_name"`
	Space   string `json:"space"`
}

type copyAgent struct {
	base
	m Manager
}

// NewCopyAgent returns the copy_agent tool.
func NewCopyAgent(m Manager) tool.Tool {
	return copyAgent{base{"copy_agent",
		"Copy an agent under a new name: its description, prompt, model, tools and max_turns. Optionally assign the copy to a space. The budget is not copied.",
		`{"type":"object","properties":{"agent":{"type":"string","description":"the agent to copy"},"new_name":{"type":"string","description":"slug name of the copy"},"space":{"type":"string","description":"space to assign the copy to (optional)"}},"required":["agent","new_name"],"additionalProperties":false}`,
		tool.RiskWrite}, m}
}

func (t copyAgent) Preview(input string) string {
	var in copyInput
	if err := decode(input, &in); err != nil {
		return "copy agent: invalid input"
	}
	preview := fmt.Sprintf("copy agent %s to %s", in.Agent, in.NewName)
	if in.Space != "" {
		preview += fmt.Sprintf(" and assign it to space %s", in.Space)
	}
	return preview
}

func (t copyAgent) Execute(_ context.Context, input string) (string, bool) {
	var in copyInput
	if err := decode(input, &in); err != nil {
		return err.Error(), true
	}
	if in.Agent == "" || in.NewName == "" {
		return "agent and new_name are required", true
	}
	return result(t.m.CopyAgent(in.Agent, in.NewName, in.Space))
}
