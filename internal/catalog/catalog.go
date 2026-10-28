// Package catalog stores the global agent definitions. Each agent is one
// Markdown file, <home>/agents/<name>.md, with YAML frontmatter for the
// settings and the body as the system prompt. Older layouts (one folder per
// agent with agent.yaml and prompt.md) are migrated on load.
package catalog

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/tinchoram/groot-cli/internal/api"
	"github.com/tinchoram/groot-cli/internal/sandbox"
)

// AgentDef is one agent definition.
type AgentDef struct {
	Name        string
	Description string
	// Model is a full model id, or "" for the configured default. The aliases
	// opus, sonnet and haiku are resolved when a file is parsed.
	Model    string
	Tools    []string
	MaxTurns int
	// BudgetUSD caps the cumulative spend of this agent in each space. Zero
	// means no budget.
	BudgetUSD float64
	// Author and Collection say who published the definition and which
	// collection it belongs to. Both are optional and kept on save.
	Author     string
	Collection string
	// Effort is how much the model works on each call: low, medium, high, xhigh
	// or max. Empty uses the provider's effort. It overrides the provider's value.
	Effort string
	// Credentials are the credential sets (sandbox.CredentialSets) the agent's
	// commands may read. They are granted only here, in the file, or by the
	// admin tools grant_credentials and revoke_credentials.
	Credentials []string
	// GlobalMemory lets the agent read the global memory of the root database.
	// groot always reads it; other agents read it only when this is set.
	GlobalMemory bool
	// Prompt is the body of the Markdown file: the system prompt.
	Prompt string
}

// KnownTools lists the builtin tool names an agent may use.
var KnownTools = []string{
	"read_file", "list_dir", "glob", "write_file", "bash", "bash_background", "monitor", "job_list", "job_output", "job_stop", "kubectl", "remember", "recall", "memory_get", "forget", "update_memory",
	"list_spaces", "create_space", "assign_agent", "unassign_agent", "list_agents", "create_agent",
	"import_agent", "list_library", "install_agent", "send_message", "read_inbox", "current_time", "task_classify", "task_open", "task_advance", "task_verify", "task_status", "upgrade_agent", "set_work_mode", "feedback_report", "feedback_list", "feedback_update", "read_changelog", "fetch_url", "list_models", "set_agent_model", "set_space_route", "set_space_model", "set_space_delegation", "set_space_budget", "set_agent_budget", "get_context", "get_cost", "list_tasks", "audit_query", "set_context_window", "grant_tool", "revoke_tool", "set_default_model", "grant_global_memory", "revoke_global_memory",
	"delete_space", "edit_space", "delete_agent", "edit_agent", "move_agent", "copy_agent",
	"grant_credentials", "revoke_credentials",
}

// mcpToolName matches an MCP tool an agent may list: <server>__<tool> or
// <server>__* for every allowlisted tool of the server. The server part follows
// the config's server names. Whether the server is configured and the tool is
// allowlisted is checked by the workspace, which knows the config.
var mcpToolName = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,31}__(\*|[A-Za-z0-9_.-]+)$`)

// IsKnownTool reports whether an agent may list name: a builtin tool, or an MCP
// tool name of the form the config allows. An MCP name is only checked for its
// shape here.
func IsKnownTool(name string) bool {
	return slices.Contains(KnownTools, name) || mcpToolName.MatchString(name)
}

// DefaultTools is the tool set given to agents created with /agent new.
// It is read-leaning: no file writes and no shell.
func DefaultTools() []string {
	return []string{"read_file", "kubectl", "recall", "remember", "memory_get", "forget", "update_memory", "send_message", "read_inbox", "current_time", "task_classify", "task_open", "task_advance", "task_verify", "task_status", "feedback_list"}
}

var nameRe = regexp.MustCompile(`^[a-z][a-z0-9-]{0,39}$`)

// modelAliases maps the short names used in agent files to model ids.
var modelAliases = map[string]string{
	"opus":   "claude-opus-5-5",
	"sonnet": "claude-sonnet-5-5",
	"haiku":  "claude-haiku-5-5",
}

// resolveModel returns the model id for a value from an agent file. "inherit"
// and "" mean the configured default, which is "" here.
func resolveModel(v string) string {
	v = strings.ToLower(strings.TrimSpace(v))
	if v == "" || v == "inherit" {
		return ""
	}
	if id, ok := modelAliases[v]; ok {
		return id
	}
	return strings.TrimSpace(v)
}

// Validate checks a definition without touching the disk.
func Validate(d AgentDef) error {
	if !nameRe.MatchString(d.Name) {
		return fmt.Errorf("agent name %q must be a lowercase slug (letters, digits, dashes; start with a letter)", d.Name)
	}
	if len(d.Tools) == 0 {
		return fmt.Errorf("agent %q needs at least one tool", d.Name)
	}
	for _, t := range d.Tools {
		if !IsKnownTool(t) {
			return fmt.Errorf("agent %q lists unknown tool %q", d.Name, t)
		}
	}
	if d.MaxTurns < 0 {
		return fmt.Errorf("agent %q: max_turns cannot be negative", d.Name)
	}
	if d.BudgetUSD < 0 {
		return fmt.Errorf("agent %q: budget cannot be negative", d.Name)
	}
	if !api.ValidEffort(d.Effort) {
		return fmt.Errorf("agent %q: effort %q is invalid (use low, medium, high, xhigh or max, or leave it empty)", d.Name, d.Effort)
	}
	if _, err := sandbox.CredentialDirs(d.Credentials); err != nil {
		return fmt.Errorf("agent %q: credentials: %w", d.Name, err)
	}
	return nil
}

// credentialSets returns the set names of a credentials list without blanks or
// repeats, in the order written.
func credentialSets(names []string) []string {
	var out []string
	for _, n := range names {
		n = strings.TrimSpace(n)
		if n != "" && !slices.Contains(out, n) {
			out = append(out, n)
		}
	}
	return out
}

// frontmatter is the YAML block at the top of an agent file. Unknown keys are ignored.
type frontmatter struct {
	Name         string    `yaml:"name"`
	Description  string    `yaml:"description"`
	Model        string    `yaml:"model"`
	Tools        yaml.Node `yaml:"tools"`
	MaxTurns     int       `yaml:"max_turns"`
	BudgetUSD    float64   `yaml:"budget_usd"`
	Author       string    `yaml:"author"`
	Collection   string    `yaml:"collection"`
	GlobalMemory bool      `yaml:"global_memory"`
	Effort       string    `yaml:"effort"`
	Credentials  []string  `yaml:"credentials"`
}

// splitFrontmatter separates the frontmatter block from the body. It accepts
// CRLF line endings.
func splitFrontmatter(data []byte) (front, body string, err error) {
	text := strings.ReplaceAll(string(data), "\r\n", "\n")
	if !strings.HasPrefix(text, "---\n") {
		return "", "", errors.New("agent file must start with a --- frontmatter block")
	}
	rest := text[len("---\n"):]
	switch {
	case strings.Contains(rest, "\n---\n"):
		i := strings.Index(rest, "\n---\n")
		return rest[:i], rest[i+len("\n---\n"):], nil
	case strings.HasSuffix(rest, "\n---"):
		return rest[:len(rest)-len("\n---")], "", nil
	}
	return "", "", errors.New("frontmatter is not closed by a --- line")
}

// ParseAgentFile reads an agent file: frontmatter between two --- lines, then
// the system prompt. It accepts CRLF line endings. Tools may be a YAML list or
// a comma-separated string. Tool names are not checked here, so that imported
// files can be mapped before validation.
func ParseAgentFile(data []byte) (AgentDef, error) {
	front, body, err := splitFrontmatter(data)
	if err != nil {
		return AgentDef{}, err
	}
	var fm frontmatter
	if err := yaml.Unmarshal([]byte(front), &fm); err != nil {
		return AgentDef{}, fmt.Errorf("frontmatter: %w", err)
	}
	tools, err := parseTools(fm.Tools)
	if err != nil {
		return AgentDef{}, err
	}
	effort := strings.ToLower(strings.TrimSpace(fm.Effort))
	if !api.ValidEffort(effort) {
		return AgentDef{}, fmt.Errorf("effort %q is invalid (use low, medium, high, xhigh or max, or leave it out)", fm.Effort)
	}
	creds := credentialSets(fm.Credentials)
	if _, err := sandbox.CredentialDirs(creds); err != nil {
		return AgentDef{}, fmt.Errorf("credentials: %w", err)
	}
	return AgentDef{
		Name:         strings.TrimSpace(fm.Name),
		Description:  fm.Description,
		Model:        resolveModel(fm.Model),
		Tools:        tools,
		MaxTurns:     fm.MaxTurns,
		BudgetUSD:    fm.BudgetUSD,
		Author:       strings.TrimSpace(fm.Author),
		Collection:   strings.TrimSpace(fm.Collection),
		Effort:       effort,
		Credentials:  creds,
		GlobalMemory: fm.GlobalMemory,
		Prompt:       strings.TrimSpace(body),
	}, nil
}

// Meta is who published an agent file and which collection it belongs to.
type Meta struct {
	Author     string
	Collection string
}

// ParseMeta reads the author and collection keys of the frontmatter. Files
// without them return empty fields.
func ParseMeta(data []byte) (Meta, error) {
	front, _, err := splitFrontmatter(data)
	if err != nil {
		return Meta{}, err
	}
	var m struct {
		Author     string `yaml:"author"`
		Collection string `yaml:"collection"`
	}
	if err := yaml.Unmarshal([]byte(front), &m); err != nil {
		return Meta{}, fmt.Errorf("frontmatter: %w", err)
	}
	return Meta{Author: m.Author, Collection: m.Collection}, nil
}

// parseTools accepts a YAML sequence or a comma-separated scalar.
func parseTools(n yaml.Node) ([]string, error) {
	var out []string
	switch n.Kind {
	case 0:
		return nil, nil
	case yaml.ScalarNode:
		for _, part := range strings.Split(n.Value, ",") {
			if p := strings.TrimSpace(part); p != "" {
				out = append(out, p)
			}
		}
	case yaml.SequenceNode:
		var list []string
		if err := n.Decode(&list); err != nil {
			return nil, fmt.Errorf("tools: %w", err)
		}
		for _, p := range list {
			if p = strings.TrimSpace(p); p != "" {
				out = append(out, p)
			}
		}
	default:
		return nil, errors.New("tools must be a list or a comma-separated string")
	}
	return out, nil
}

// renderFrontmatter is what RenderAgentFile writes.
type renderFrontmatter struct {
	Name         string   `yaml:"name"`
	Description  string   `yaml:"description"`
	Model        string   `yaml:"model,omitempty"`
	Tools        []string `yaml:"tools"`
	MaxTurns     int      `yaml:"max_turns,omitempty"`
	BudgetUSD    float64  `yaml:"budget_usd,omitempty"`
	Author       string   `yaml:"author,omitempty"`
	Collection   string   `yaml:"collection,omitempty"`
	Effort       string   `yaml:"effort,omitempty"`
	Credentials  []string `yaml:"credentials,omitempty"`
	GlobalMemory bool     `yaml:"global_memory,omitempty"`
}

// RenderAgentFile writes a definition in the Markdown format.
func RenderAgentFile(d AgentDef) ([]byte, error) {
	front, err := yaml.Marshal(renderFrontmatter{
		Name:         d.Name,
		Description:  d.Description,
		Model:        d.Model,
		Tools:        d.Tools,
		MaxTurns:     d.MaxTurns,
		BudgetUSD:    d.BudgetUSD,
		Author:       d.Author,
		Collection:   d.Collection,
		Effort:       d.Effort,
		Credentials:  credentialSets(d.Credentials),
		GlobalMemory: d.GlobalMemory,
	})
	if err != nil {
		return nil, fmt.Errorf("encode frontmatter: %w", err)
	}
	return []byte("---\n" + string(front) + "---\n\n" + strings.TrimSpace(d.Prompt) + "\n"), nil
}

func agentsDir(home string) string { return filepath.Join(home, "agents") }

// Path returns the file of one agent definition.
func Path(home, name string) string { return filepath.Join(agentsDir(home), name+".md") }

// Exists reports whether an agent with this name is in the catalog.
func Exists(home, name string) bool {
	_, err := os.Stat(Path(home, name))
	return err == nil
}

// Save validates d and writes <name>.md, replacing any previous version and
// any legacy folder of the same name. Invalid definitions are never written.
func Save(home string, d AgentDef, prompt string) error {
	if err := Validate(d); err != nil {
		return err
	}
	d.Prompt = prompt
	raw, err := RenderAgentFile(d)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(agentsDir(home), 0o755); err != nil {
		return fmt.Errorf("create agent folder: %w", err)
	}
	if err := os.WriteFile(Path(home, d.Name), raw, 0o644); err != nil {
		return fmt.Errorf("write agent file: %w", err)
	}
	return os.RemoveAll(legacyDir(home, d.Name))
}

func legacyDir(home, name string) string { return filepath.Join(agentsDir(home), name) }

// Migrate converts legacy folders (<name>/agent.yaml + prompt.md) to
// <name>.md. A folder is removed only after its new file was written and
// parsed back. A folder whose .md already exists is left alone.
func Migrate(home string) error {
	entries, err := os.ReadDir(agentsDir(home))
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("read agent catalog: %w", err)
	}
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		name := e.Name()
		dir := legacyDir(home, name)
		raw, err := os.ReadFile(filepath.Join(dir, "agent.yaml"))
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return fmt.Errorf("read %s: %w", dir, err)
		}
		if _, err := os.Stat(Path(home, name)); err == nil {
			continue
		}
		var legacy struct {
			Name        string   `yaml:"name"`
			Description string   `yaml:"description"`
			Model       string   `yaml:"model"`
			Tools       []string `yaml:"tools"`
			MaxTurns    int      `yaml:"max_turns"`
		}
		if err := yaml.Unmarshal(raw, &legacy); err != nil {
			return fmt.Errorf("parse %s/agent.yaml: %w", dir, err)
		}
		if legacy.Name != name {
			return fmt.Errorf("%s/agent.yaml names the agent %q; folder and name must match", dir, legacy.Name)
		}
		prompt, err := os.ReadFile(filepath.Join(dir, "prompt.md"))
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("read %s/prompt.md: %w", dir, err)
		}
		d := AgentDef{
			Name:        name,
			Description: legacy.Description,
			Model:       resolveModel(legacy.Model),
			Tools:       legacy.Tools,
			MaxTurns:    legacy.MaxTurns,
			Prompt:      strings.TrimSpace(string(prompt)),
		}
		if err := Validate(d); err != nil {
			return fmt.Errorf("%s: %w", dir, err)
		}
		out, err := RenderAgentFile(d)
		if err != nil {
			return err
		}
		if err := os.WriteFile(Path(home, name), out, 0o644); err != nil {
			return fmt.Errorf("write %s: %w", Path(home, name), err)
		}
		back, err := ParseAgentFile(out)
		if err != nil || back.Name != name {
			_ = os.Remove(Path(home, name))
			return fmt.Errorf("migrated %s did not read back: %v", name, err)
		}
		if err := os.RemoveAll(dir); err != nil {
			return fmt.Errorf("remove legacy folder %s: %w", dir, err)
		}
	}
	return nil
}

// Load migrates legacy folders, then reads every <name>.md, sorted by name.
// An invalid file fails the whole load so that problems stay visible.
func Load(home string) ([]AgentDef, error) {
	if err := Migrate(home); err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(agentsDir(home))
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read agent catalog: %w", err)
	}
	var defs []AgentDef
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".md") {
			continue
		}
		stem := strings.TrimSuffix(e.Name(), ".md")
		path := filepath.Join(agentsDir(home), e.Name())
		raw, err := os.ReadFile(path)
		if err != nil {
			return nil, fmt.Errorf("read %s: %w", path, err)
		}
		d, err := ParseAgentFile(raw)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", path, err)
		}
		if d.Name != stem {
			return nil, fmt.Errorf("%s names the agent %q; file name and name must match", path, d.Name)
		}
		if err := Validate(d); err != nil {
			return nil, fmt.Errorf("%s: %w", path, err)
		}
		defs = append(defs, d)
	}
	sort.Slice(defs, func(i, j int) bool { return defs[i].Name < defs[j].Name })
	return defs, nil
}
