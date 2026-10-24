// Package admintool exposes spaces and agents to the administrator agent as
// tools. It depends on the Manager interface only; the workspace implements it,
// so this package never imports the workspace.
package admintool

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/tinchoram/groot-cli/internal/agentimport"
	"github.com/tinchoram/groot-cli/internal/api"
	"github.com/tinchoram/groot-cli/internal/provider"
	"github.com/tinchoram/groot-cli/internal/tool"
)

// SpaceInfo describes a space.
type SpaceInfo struct {
	Name   string
	Path   string
	Agents []string
}

// AgentInfo describes an agent definition.
type AgentInfo struct {
	Name        string
	Description string
	Model       string
	Tools       []string
}

// AgentSpec is the input for creating an agent.
type AgentSpec struct {
	Name        string
	Description string
	Prompt      string
	Tools       []string
	Model       string
}

// LibraryInfo describes an agent in the library of installable agents.
type LibraryInfo struct {
	Name        string
	Description string
	Tools       []string
	Source      string // builtin, user, remote, public or warning
	Collection  string
	Author      string
	URL         string // the agent file of a remote or public entry
	License     string
	ValidatedBy string
}

// ImportResult reports an imported agent and what was changed on the way.
type ImportResult struct {
	Name     string
	Warnings []string
}

// Manager is the operations the admin tools perform.
type Manager interface {
	ListLibrary() ([]LibraryInfo, error)
	InstallAgent(name, space string, overwrite bool) error
	ImportAgent(plan agentimport.Planned, overwrite bool) (ImportResult, error)
	ListSpaces() []SpaceInfo
	CreateSpace(name, path string) error
	AssignAgent(space, agent string) error
	UnassignAgent(space, agent string) error
	ListAgents() []AgentInfo
	CreateAgent(spec AgentSpec) error
	UpgradeAgent(name string) ([]string, error)
	SetWorkMode(space, agent string, on bool) error
	// ListModels returns the models the provider offers.
	ListModels(ctx context.Context) ([]provider.ModelInfo, error)
	// SetAgentModel saves the model of an agent. It returns a warning when the
	// model could not be checked against the provider's list.
	SetAgentModel(ctx context.Context, agent, model string) (string, error)
	// SetSpaceRoute sets the model and agent of a task size in a space; empty
	// values clear them. It returns a warning when the model could not be checked.
	SetSpaceRoute(ctx context.Context, space, size, model, agent string) (string, error)
	// SetSpaceModel sets the default model of a space's agents; empty clears it.
	SetSpaceModel(ctx context.Context, space, model string) (string, error)
	// SetSpaceBudget sets the budget of all agents of a space; 0 clears it.
	SetSpaceBudget(space string, usd float64) error
	// SetAgentBudget sets the budget of an agent; 0 clears it.
	SetAgentBudget(agent string, usd float64) error
	// AgentTools lists the tools of an agent.
	AgentTools(name string) ([]string, error)
	// EditAgentTools adds and removes tools of an agent. The managed agents keep
	// their required tools.
	EditAgentTools(name string, add, remove []string) error
	// DefaultModel is the model used when an agent sets none; SessionModel is the
	// model of the conversation that runs the tool.
	DefaultModel() string
	SessionModel() string
	// SetDefaultModel writes the default model into the configuration file.
	SetDefaultModel(ctx context.Context, model string) (string, error)
}

// All returns every admin tool. home is the user's home directory, used to
// expand "~" in paths.
func All(m Manager, home string) []tool.Tool {
	return []tool.Tool{
		NewListSpaces(m),
		NewCreateSpace(m, home),
		NewAssignAgent(m),
		NewUnassignAgent(m),
		NewListAgents(m),
		NewCreateAgent(m),
		NewImportAgent(m),
		NewListLibrary(m),
		NewInstallAgent(m),
		NewUpgradeAgent(m),
		NewSetWorkMode(m),
		NewListModels(m),
		NewSetAgentModel(m),
		NewSetSpaceRoute(m),
		NewSetSpaceModel(m),
		NewSetSpaceBudget(m),
		NewSetAgentBudget(m),
		NewGrantTool(m),
		NewRevokeTool(m),
		NewSetDefaultModel(m),
	}
}

// decode parses strict JSON: unknown fields are an error.
func decode(input string, v any) error {
	dec := json.NewDecoder(bytes.NewReader([]byte(input)))
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		return fmt.Errorf("invalid input: %w", err)
	}
	return nil
}

func result(out string, err error) (string, bool) {
	if err != nil {
		return err.Error(), true
	}
	return out, false
}

// base holds the parts every tool shares.
type base struct {
	name, description, schema string
	risk                      tool.Risk
}

func (b base) Definition() api.ToolDef {
	return api.ToolDef{Name: b.name, Description: b.description, InputSchema: json.RawMessage(b.schema)}
}

func (b base) Risk() tool.Risk { return b.risk }

// RiskFor is the static risk: admin tools do not vary by input.
func (b base) RiskFor(string) tool.Risk { return b.risk }

// ---- list_spaces ----

type listSpaces struct {
	base
	m Manager
}

// NewListSpaces returns the list_spaces tool.
func NewListSpaces(m Manager) tool.Tool {
	return listSpaces{base{"list_spaces", "List every space with its path and assigned agents.", `{"type":"object","properties":{}}`, tool.RiskRead}, m}
}

func (t listSpaces) Preview(string) string { return "list spaces" }

func (t listSpaces) Execute(context.Context, string) (string, bool) {
	spaces := t.m.ListSpaces()
	if len(spaces) == 0 {
		return "no spaces", false
	}
	var lines []string
	for _, s := range spaces {
		agents := strings.Join(s.Agents, ", ")
		if agents == "" {
			agents = "none"
		}
		lines = append(lines, fmt.Sprintf("%s  %s  agents: %s", s.Name, s.Path, agents))
	}
	return strings.Join(lines, "\n"), false
}

// ---- create_space ----

type createSpace struct {
	base
	m    Manager
	home string
}

// NewCreateSpace returns the create_space tool.
func NewCreateSpace(m Manager, home string) tool.Tool {
	return createSpace{base{"create_space",
		"Create a space: a named working directory. The path must exist. Use an absolute path or one starting with ~.",
		`{"type":"object","properties":{"name":{"type":"string","description":"lowercase slug, letters digits and dashes"},"path":{"type":"string","description":"existing directory; absolute or starting with ~"}},"required":["name","path"],"additionalProperties":false}`,
		tool.RiskWrite}, m, home}
}

type spaceInput struct {
	Name string `json:"name"`
	Path string `json:"path"`
}

func (t createSpace) Preview(input string) string {
	var in spaceInput
	if err := decode(input, &in); err != nil {
		return "create space: invalid input"
	}
	return fmt.Sprintf("create space %s at %s", in.Name, in.Path)
}

func (t createSpace) Execute(_ context.Context, input string) (string, bool) {
	var in spaceInput
	if err := decode(input, &in); err != nil {
		return err.Error(), true
	}
	if strings.TrimSpace(in.Name) == "" {
		return "name is required", true
	}
	path, err := t.expand(in.Path)
	if err != nil {
		return err.Error(), true
	}
	return result(fmt.Sprintf("created space %s at %s", in.Name, path), t.m.CreateSpace(in.Name, path))
}

func (t createSpace) expand(p string) (string, error) {
	switch {
	case p == "~" || strings.HasPrefix(p, "~/"):
		if t.home == "" {
			return "", errors.New("cannot expand ~: home directory unknown")
		}
		return filepath.Join(t.home, strings.TrimPrefix(p, "~")), nil
	case filepath.IsAbs(p):
		return filepath.Clean(p), nil
	}
	return "", fmt.Errorf("path %q must be absolute or start with ~", p)
}

// ---- assign_agent / unassign_agent ----

type assignInput struct {
	Space string `json:"space"`
	Agent string `json:"agent"`
}

type assignAgent struct {
	base
	m Manager
}

// NewAssignAgent returns the assign_agent tool.
func NewAssignAgent(m Manager) tool.Tool {
	return assignAgent{base{"assign_agent",
		"Assign a catalog agent to a space, so it shows in that space's sidebar.",
		`{"type":"object","properties":{"space":{"type":"string"},"agent":{"type":"string"}},"required":["space","agent"],"additionalProperties":false}`,
		tool.RiskWrite}, m}
}

func (t assignAgent) Preview(input string) string {
	var in assignInput
	if err := decode(input, &in); err != nil {
		return "assign agent: invalid input"
	}
	return fmt.Sprintf("assign %s to space %s", in.Agent, in.Space)
}

func (t assignAgent) Execute(_ context.Context, input string) (string, bool) {
	var in assignInput
	if err := decode(input, &in); err != nil {
		return err.Error(), true
	}
	if in.Space == "" || in.Agent == "" {
		return "space and agent are required", true
	}
	return result(fmt.Sprintf("assigned %s to %s", in.Agent, in.Space), t.m.AssignAgent(in.Space, in.Agent))
}

type unassignAgent struct {
	base
	m Manager
}

// NewUnassignAgent returns the unassign_agent tool.
func NewUnassignAgent(m Manager) tool.Tool {
	return unassignAgent{base{"unassign_agent",
		"Remove an agent from a space. The agent stays in the catalog.",
		`{"type":"object","properties":{"space":{"type":"string"},"agent":{"type":"string"}},"required":["space","agent"],"additionalProperties":false}`,
		tool.RiskWrite}, m}
}

func (t unassignAgent) Preview(input string) string {
	var in assignInput
	if err := decode(input, &in); err != nil {
		return "unassign agent: invalid input"
	}
	return fmt.Sprintf("unassign %s from space %s", in.Agent, in.Space)
}

func (t unassignAgent) Execute(_ context.Context, input string) (string, bool) {
	var in assignInput
	if err := decode(input, &in); err != nil {
		return err.Error(), true
	}
	if in.Space == "" || in.Agent == "" {
		return "space and agent are required", true
	}
	return result(fmt.Sprintf("unassigned %s from %s", in.Agent, in.Space), t.m.UnassignAgent(in.Space, in.Agent))
}

// ---- list_agents ----

type listAgents struct {
	base
	m Manager
}

// NewListAgents returns the list_agents tool.
func NewListAgents(m Manager) tool.Tool {
	return listAgents{base{"list_agents", "List agent definitions in the catalog with description, model and tools.", `{"type":"object","properties":{}}`, tool.RiskRead}, m}
}

func (t listAgents) Preview(string) string { return "list agents" }

func (t listAgents) Execute(context.Context, string) (string, bool) {
	agents := t.m.ListAgents()
	if len(agents) == 0 {
		return "no agents", false
	}
	var lines []string
	for _, a := range agents {
		model := a.Model
		if model == "" {
			model = fmt.Sprintf("default (%s)", t.m.DefaultModel())
		}
		lines = append(lines, fmt.Sprintf("%s  %s  model: %s  tools: %s", a.Name, a.Description, model, strings.Join(a.Tools, ", ")))
	}
	return strings.Join(lines, "\n"), false
}

// ---- create_agent ----

type agentInput struct {
	Name        string   `json:"name"`
	Description string   `json:"description"`
	Prompt      string   `json:"prompt"`
	Tools       []string `json:"tools"`
	Model       string   `json:"model"`
}

type createAgent struct {
	base
	m Manager
}

// NewCreateAgent returns the create_agent tool.
func NewCreateAgent(m Manager) tool.Tool {
	return createAgent{base{"create_agent",
		"Create an agent in the catalog. Give a slug name, a one-line description, the system prompt, a subset of the builtin tools, and optionally a model.",
		`{"type":"object","properties":{"name":{"type":"string"},"description":{"type":"string"},"prompt":{"type":"string"},"tools":{"type":"array","items":{"type":"string"}},"model":{"type":"string"}},"required":["name","description","prompt","tools"],"additionalProperties":false}`,
		tool.RiskWrite}, m}
}

const previewPromptLines = 3

func (t createAgent) Preview(input string) string {
	var in agentInput
	if err := decode(input, &in); err != nil {
		return "create agent: invalid input"
	}
	lines := strings.Split(strings.TrimSpace(in.Prompt), "\n")
	if len(lines) > previewPromptLines {
		lines = append(lines[:previewPromptLines], "…")
	}
	return fmt.Sprintf("create agent %s: %s\ntools: %s\nprompt:\n%s",
		in.Name, in.Description, strings.Join(in.Tools, ", "), strings.Join(lines, "\n"))
}

func (t createAgent) Execute(_ context.Context, input string) (string, bool) {
	var in agentInput
	if err := decode(input, &in); err != nil {
		return err.Error(), true
	}
	if strings.TrimSpace(in.Name) == "" {
		return "name is required", true
	}
	if len(in.Tools) == 0 {
		return "at least one tool is required", true
	}
	spec := AgentSpec{Name: in.Name, Description: in.Description, Prompt: in.Prompt, Tools: in.Tools, Model: in.Model}
	return result(fmt.Sprintf("created agent %s", in.Name), t.m.CreateAgent(spec))
}

// ---- import_agent ----

type importInput struct {
	Source    string `json:"source"`
	Overwrite bool   `json:"overwrite"`
}

type importAgent struct {
	base
	m Manager
}

// NewImportAgent returns the import_agent tool.
func NewImportAgent(m Manager) tool.Tool {
	return importAgent{base{"import_agent",
		"Import an agent file (Markdown with YAML frontmatter, as used by Claude Code and OpenCode) from a local path or an http(s) URL. Read, Write, Edit, MultiEdit and Bash map to groot tools; other tools are dropped with a warning.",
		`{"type":"object","properties":{"source":{"type":"string","description":"local path or http(s) URL of the agent file"},"overwrite":{"type":"boolean","description":"replace an existing agent with the same name"}},"required":["source"],"additionalProperties":false}`,
		tool.RiskWrite}, m}
}

// Preview fetches the file and shows what would be imported. An unreadable
// source is reported in the preview itself.
func (t importAgent) Preview(input string) string {
	var in importInput
	if err := decode(input, &in); err != nil {
		return "import agent: invalid input"
	}
	p, err := agentimport.Load(context.Background(), in.Source)
	if err != nil {
		return "import agent: " + err.Error()
	}
	return p.Preview()
}

func (t importAgent) Execute(ctx context.Context, input string) (string, bool) {
	var in importInput
	if err := decode(input, &in); err != nil {
		return err.Error(), true
	}
	if strings.TrimSpace(in.Source) == "" {
		return "source is required", true
	}
	plan, err := agentimport.Load(ctx, in.Source)
	if err != nil {
		return err.Error(), true
	}
	res, err := t.m.ImportAgent(plan, in.Overwrite)
	if err != nil {
		return err.Error(), true
	}
	out := fmt.Sprintf("imported agent %s", res.Name)
	for _, w := range res.Warnings {
		out += "\nwarning: " + w
	}
	return out, false
}

// ---- list_library ----

type listLibrary struct {
	base
	m Manager
}

// NewListLibrary returns the list_library tool.
func NewListLibrary(m Manager) tool.Tool {
	return listLibrary{base{"list_library",
		"List the agents that can be installed from the library, with description, tools and source (builtin or user).",
		`{"type":"object","properties":{}}`, tool.RiskRead}, m}
}

func (t listLibrary) Preview(string) string { return "list the agent library" }

func (t listLibrary) Execute(context.Context, string) (string, bool) {
	entries, err := t.m.ListLibrary()
	if err != nil {
		return err.Error(), true
	}
	if len(entries) == 0 {
		return "the library is empty", false
	}
	groups := map[string][]LibraryInfo{}
	var order []string
	for _, e := range entries {
		key := e.Collection + "\x00" + e.Author + "\x00" + e.License + "\x00" + e.ValidatedBy
		if _, ok := groups[key]; !ok {
			order = append(order, key)
		}
		groups[key] = append(groups[key], e)
	}
	var lines []string
	for _, key := range order {
		parts := strings.Split(key, "\x00")
		header := fmt.Sprintf("collection %s by %s", parts[0], parts[1])
		if parts[2] != "" {
			header += " · license " + parts[2]
		}
		if parts[3] != "" {
			header += " · validated by " + parts[3]
		}
		lines = append(lines, header)
		for _, e := range groups[key] {
			lines = append(lines, fmt.Sprintf("  %s  %s  tools: %s  (%s)", e.Name, e.Description, strings.Join(e.Tools, ", "), e.Source))
		}
	}
	return strings.Join(lines, "\n"), false
}

// ---- install_agent ----

type installInput struct {
	Name      string `json:"name"`
	Space     string `json:"space"`
	Overwrite bool   `json:"overwrite"`
}

type installAgent struct {
	base
	m Manager
}

// NewInstallAgent returns the install_agent tool.
func NewInstallAgent(m Manager) tool.Tool {
	return installAgent{base{"install_agent",
		"Copy an agent from the library into the catalog, optionally assigning it to a space. Refuses to replace an existing agent unless overwrite is true. Never replaces groot.",
		`{"type":"object","properties":{"name":{"type":"string","description":"library agent name"},"space":{"type":"string","description":"space to assign the agent to (optional)"},"overwrite":{"type":"boolean","description":"replace an existing agent with the same name"}},"required":["name"],"additionalProperties":false}`,
		tool.RiskWrite}, m}
}

func (t installAgent) Preview(input string) string {
	var in installInput
	if err := decode(input, &in); err != nil {
		return "install library agent: invalid input"
	}
	preview := fmt.Sprintf("install library agent %s into the catalog", in.Name)
	if in.Space != "" {
		preview += fmt.Sprintf(" and assign it to space %s", in.Space)
	}
	if entries, err := t.m.ListLibrary(); err == nil {
		for _, e := range entries {
			if e.Name == in.Name && (e.Source == "remote" || e.Source == "public") && e.URL != "" {
				preview += "\nsource: " + e.URL
			}
		}
	}
	return preview
}

func (t installAgent) Execute(_ context.Context, input string) (string, bool) {
	var in installInput
	if err := decode(input, &in); err != nil {
		return err.Error(), true
	}
	if strings.TrimSpace(in.Name) == "" {
		return "name is required", true
	}
	return result(fmt.Sprintf("installed %s", in.Name), t.m.InstallAgent(in.Name, in.Space, in.Overwrite))
}
