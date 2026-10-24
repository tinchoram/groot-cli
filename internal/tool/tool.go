// Package tool defines the tool port, the registry and the builtin tools.
// Every tool output is passed through Redact before it reaches the model.
package tool

import (
	"context"
	"fmt"
	"regexp"
	"sync"

	"github.com/tinchoram/groot-cli/internal/api"
)

// Risk classifies what a tool can do. The permission gate keys off it.
type Risk int

const (
	RiskRead Risk = iota
	RiskWrite
	RiskExec
)

func (r Risk) String() string {
	switch r {
	case RiskRead:
		return "read"
	case RiskWrite:
		return "write"
	case RiskExec:
		return "exec"
	}
	return "unknown"
}

// Tool is one capability the model can invoke.
type Tool interface {
	Definition() api.ToolDef
	// Risk is the static risk of the tool.
	Risk() Risk
	// RiskFor is the risk of one call. Tools whose risk depends on the input,
	// such as bash, classify it here; the others return Risk().
	RiskFor(input string) Risk
	// Preview describes the call for the user before it runs: a command, or a
	// unified diff for writes.
	Preview(input string) string
	// Execute runs the call. input is the raw JSON the model produced. The
	// boolean reports whether the output is an error message.
	Execute(ctx context.Context, input string) (string, bool)
}

// Registry holds tools in registration order.
type Registry struct {
	mu    sync.RWMutex
	tools map[string]Tool
	order []string
}

// NewRegistry returns an empty registry.
func NewRegistry() *Registry {
	return &Registry{tools: map[string]Tool{}}
}

// Register adds t. Names must be unique.
func (r *Registry) Register(t Tool) error {
	name := t.Definition().Name
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, dup := r.tools[name]; dup {
		return fmt.Errorf("tool %q already registered", name)
	}
	r.tools[name] = t
	r.order = append(r.order, name)
	return nil
}

// Get returns the tool with the given name.
func (r *Registry) Get(name string) (Tool, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	t, ok := r.tools[name]
	return t, ok
}

// Names lists tool names in registration order.
func (r *Registry) Names() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return append([]string(nil), r.order...)
}

// Definitions returns the model-facing definitions in registration order.
func (r *Registry) Definitions() []api.ToolDef {
	r.mu.RLock()
	defer r.mu.RUnlock()
	defs := make([]api.ToolDef, 0, len(r.order))
	for _, n := range r.order {
		defs = append(defs, r.tools[n].Definition())
	}
	return defs
}

var (
	// jsonSecret matches a JSON-quoted secret-like key and its string value,
	// e.g. `"token": "abc"`. The key stays visible; only the value is masked.
	jsonSecret = regexp.MustCompile(`(?i)("[A-Za-z0-9_.-]*(?:password|token|secret|api[_-]?key|access[_-]?key)[A-Za-z0-9_.-]*"\s*:\s*)"[^"]*"`)
	// envSecret matches env-style assignments whose name contains a secret word,
	// e.g. `AWS_SECRET_ACCESS_KEY=...` or `GITHUB_TOKEN=...`.
	envSecret = regexp.MustCompile(`(?i)\b([A-Z0-9_]*(?:PASSWORD|TOKEN|SECRET|API_?KEY)[A-Z0-9_]*)\s*=\s*\S+`)
	// assignedSecret matches `password: x`, `api_key=x`, `token = x` and similar.
	assignedSecret = regexp.MustCompile(`(?i)(password|token|secret|api[_-]?key)\s*[:=]\s*\S+`)
	bearerToken    = regexp.MustCompile(`(?i)bearer\s+[A-Za-z0-9._~+/=-]+`)
)

// Redact masks obvious credentials in s. Keys stay visible so the output
// still says what was hidden.
func Redact(s string) string {
	s = jsonSecret.ReplaceAllString(s, `${1}"***"`)
	s = envSecret.ReplaceAllString(s, "${1}=[REDACTED]")
	s = assignedSecret.ReplaceAllString(s, "${1}=[REDACTED]")
	return bearerToken.ReplaceAllString(s, "Bearer [REDACTED]")
}
