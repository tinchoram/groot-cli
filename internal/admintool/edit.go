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

// deleteSpaceInput is the input of delete_space. Without Confirm the call only
// reports; PurgeData is honoured only together with Confirm.
type deleteSpaceInput struct {
	Name      string `json:"name"`
	Confirm   bool   `json:"confirm"`
	PurgeData bool   `json:"purge_data"`
}

// NewDeleteSpace returns the delete_space tool.
func NewDeleteSpace(m Manager) tool.Tool {
	return deleteSpace{base{"delete_space",
		"Delete a space from spaces.yaml. The first call, without confirm: true, changes nothing and reports the assigned agents and open tasks; a second call with confirm: true deletes it. The directory stays on disk, and so does the .groot data unless purge_data is true; the root space cannot be deleted. Deleting the active space switches to root.",
		`{"type":"object","properties":{"name":{"type":"string","description":"Space to delete"},"confirm":{"type":"boolean","description":"true to delete; omit for the report only"},"purge_data":{"type":"boolean","description":"With confirm true, also delete <space path>/.groot (memory and audit log); default false"}},"required":["name"],"additionalProperties":false}`,
		tool.RiskWrite}, m}
}

func (t deleteSpace) Preview(input string) string {
	var in deleteSpaceInput
	if err := decode(input, &in); err != nil {
		return "delete space: invalid input"
	}
	if !in.Confirm {
		return fmt.Sprintf("report for deleting space %s (nothing is changed by this call; the report lists its agents and open tasks)", in.Name)
	}
	if in.PurgeData {
		return fmt.Sprintf("delete space %s from spaces.yaml and purge its .groot data (its directory is kept)", in.Name)
	}
	return fmt.Sprintf("delete space %s from spaces.yaml (its directory and .groot data are kept)", in.Name)
}

func (t deleteSpace) Execute(_ context.Context, input string) (string, bool) {
	var in deleteSpaceInput
	if err := decode(input, &in); err != nil {
		return err.Error(), true
	}
	if strings.TrimSpace(in.Name) == "" {
		return "name is required", true
	}
	purge := in.PurgeData && in.Confirm
	return result(t.m.DeleteSpace(in.Name, in.Confirm, purge))
}

// ---- edit_space ----

type editSpaceInput struct {
	Name    string `json:"name"`
	NewName string `json:"new_name"`
	Path    string `json:"path"`
	// Network is the space's network setting; nil keeps it.
	Network *bool `json:"network"`
}

type editSpace struct {
	base
	m    Manager
	home string
}

// NewEditSpace returns the edit_space tool.
func NewEditSpace(m Manager, home string) tool.Tool {
	return editSpace{base{"edit_space",
		"Rename a space, point it at another directory, or both. The root space cannot be renamed. A rename keeps the selection and the unread messages, and a new path does not move the .groot data under the old path.",
		`{"type":"object","properties":{"name":{"type":"string","description":"Space to edit"},"new_name":{"type":"string","description":"New lowercase slug; optional"},"path":{"type":"string","description":"Existing directory, absolute or starting with ~; optional"},"network":{"type":"boolean","description":"Turn the space's network on or off; optional, omit to keep it"}},"required":["name"],"additionalProperties":false}`,
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
	if in.Network != nil {
		parts = append(parts, fmt.Sprintf("set the network of space %s to %s", in.Name, onOff(*in.Network)))
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
	if in.NewName == "" && in.Path == "" && in.Network == nil {
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
	if in.NewName == "" && in.Path == "" {
		return t.setNetwork(in.Name, *in.Network)
	}
	out, isErr := result(t.m.EditSpace(in.Name, in.NewName, path))
	if isErr || in.Network == nil {
		return out, isErr
	}
	// A network change applies to the space under its new name.
	target := in.Name
	if in.NewName != "" {
		target = in.NewName
	}
	net, netErr := t.setNetwork(target, *in.Network)
	return out + "\n" + net, netErr
}

// setNetwork turns the network of a space on or off through the manager.
func (t editSpace) setNetwork(name string, on bool) (string, bool) {
	if err := t.m.SetSpaceNetwork(name, on); err != nil {
		return err.Error(), true
	}
	return fmt.Sprintf("network of %s is now %s", name, onOff(on)), false
}

// onOff names a network setting as the user sees it.
func onOff(on bool) string {
	if on {
		return "on"
	}
	return "off"
}

// ---- delete_agent ----

type deleteAgent struct {
	base
	m Manager
}

// NewDeleteAgent returns the delete_agent tool.
func NewDeleteAgent(m Manager) tool.Tool {
	return deleteAgent{base{"delete_agent",
		"Delete an agent from the catalog and unassign it from every space. The first call, without confirm: true, changes nothing and reports the spaces it is assigned to with their open tasks; a second call with confirm: true deletes it, ends its open conversations, and keeps its memory and sessions in each space's database. groot cannot be deleted.",
		`{"type":"object","properties":{"name":{"type":"string","description":"Agent to delete"},"confirm":{"type":"boolean","description":"true to delete; omit for the report only"}},"required":["name"],"additionalProperties":false}`,
		tool.RiskWrite}, m}
}

// deleteAgentInput is the input of delete_agent.
type deleteAgentInput struct {
	Name    string `json:"name"`
	Confirm bool   `json:"confirm"`
}

func (t deleteAgent) Preview(input string) string {
	var in deleteAgentInput
	if err := decode(input, &in); err != nil {
		return "delete agent: invalid input"
	}
	if !in.Confirm {
		return fmt.Sprintf("report for deleting agent %s (nothing is changed by this call; the report lists its spaces and open tasks)", in.Name)
	}
	return fmt.Sprintf("delete agent %s from the catalog and from every space", in.Name)
}

func (t deleteAgent) Execute(_ context.Context, input string) (string, bool) {
	var in deleteAgentInput
	if err := decode(input, &in); err != nil {
		return err.Error(), true
	}
	if strings.TrimSpace(in.Name) == "" {
		return "name is required", true
	}
	return result(t.m.DeleteAgent(in.Name, in.Confirm))
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
		"Change an agent's description or prompt, or rename it. It keeps its model, tools and budget. Managed agents (groot and debug) keep their name, and only their description and prompt can change.",
		`{"type":"object","properties":{"name":{"type":"string","description":"Agent to edit"},"new_name":{"type":"string","description":"New slug name; optional"},"description":{"type":"string","description":"One-line description; optional"},"prompt":{"type":"string","description":"The full new system prompt; optional"}},"required":["name"],"additionalProperties":false}`,
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
	Agent      string `json:"agent"`
	From       string `json:"from"`
	To         string `json:"to"`
	WithMemory bool   `json:"with_memory"`
}

type moveAgent struct {
	base
	m Manager
}

// NewMoveAgent returns the move_agent tool.
func NewMoveAgent(m Manager) tool.Tool {
	return moveAgent{base{"move_agent",
		"Move an agent from one space to another in one step. The agent must be assigned to the source space and not yet to the target. Its conversation in the source space ends, and its sessions stay stored there; with with_memory: true, its memory, sessions and tasks are also copied to the target, and the source keeps its copies.",
		`{"type":"object","properties":{"agent":{"type":"string","description":"Agent to move"},"from":{"type":"string","description":"Space the agent leaves"},"to":{"type":"string","description":"Space the agent joins"},"with_memory":{"type":"boolean","description":"true copies memory, sessions and tasks to the target; default false"}},"required":["agent","from","to"],"additionalProperties":false}`,
		tool.RiskWrite}, m}
}

func (t moveAgent) Preview(input string) string {
	var in moveInput
	if err := decode(input, &in); err != nil {
		return "move agent: invalid input"
	}
	preview := fmt.Sprintf("move agent %s from space %s to space %s", in.Agent, in.From, in.To)
	if in.WithMemory {
		preview += " and copy its memory, sessions and tasks to the target (the source space keeps its copies)"
	}
	return preview
}

func (t moveAgent) Execute(_ context.Context, input string) (string, bool) {
	var in moveInput
	if err := decode(input, &in); err != nil {
		return err.Error(), true
	}
	if in.Agent == "" || in.From == "" || in.To == "" {
		return "agent, from and to are required", true
	}
	return result(t.m.MoveAgent(in.Agent, in.From, in.To, in.WithMemory))
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
		"Copy an agent under a new name, with its description, prompt, model, tools and max_turns. Use it to make a variant of an agent; the budget is not copied. Optionally assign the copy to a space.",
		`{"type":"object","properties":{"agent":{"type":"string","description":"Agent to copy"},"new_name":{"type":"string","description":"Slug name of the copy"},"space":{"type":"string","description":"Space to assign the copy to; optional"}},"required":["agent","new_name"],"additionalProperties":false}`,
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
