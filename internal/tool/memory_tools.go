package tool

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/tinchoram/groot-cli/internal/api"
	"github.com/tinchoram/groot-cli/internal/memory"
)

// Remember stores a fact, decision or runbook through the memory port.
type Remember struct {
	store memory.Store
	agent string
}

// NewRemember returns the remember tool. agent tags the saved entries.
func NewRemember(store memory.Store, agent string) *Remember {
	return &Remember{store: store, agent: agent}
}

type rememberInput struct {
	Kind  string `json:"kind"`
	Text  string `json:"text"`
	Scope string `json:"scope"`
}

func (*Remember) Definition() api.ToolDef {
	return api.ToolDef{
		Name:        "remember",
		Description: "Save a durable fact, decision or runbook step to long-term memory for future sessions.",
		InputSchema: json.RawMessage(`{"type":"object","properties":{"kind":{"type":"string","enum":["fact","decision","runbook"]},"text":{"type":"string"},"scope":{"type":"string","enum":["global","project","agent"]}},"required":["kind","text"]}`),
	}
}

// Risk is RiskRead: the tool only writes the local memory database that the
// user already configured, and it cannot touch the infrastructure.
func (*Remember) Risk() Risk { return RiskRead }

func (*Remember) Preview(input string) string {
	var in rememberInput
	if err := json.Unmarshal([]byte(input), &in); err != nil {
		return "remember: invalid input"
	}
	return Redact(fmt.Sprintf("remember [%s] %s", in.Kind, in.Text))
}

func (r *Remember) Execute(ctx context.Context, input string) (string, bool) {
	out, isErr := r.execute(ctx, input)
	return Redact(out), isErr
}

func (r *Remember) execute(ctx context.Context, input string) (string, bool) {
	var in rememberInput
	if err := json.Unmarshal([]byte(input), &in); err != nil {
		return "invalid input: " + err.Error(), true
	}
	kind := memory.Kind(in.Kind)
	if !kind.Valid() || kind == memory.KindSessionSummary {
		return fmt.Sprintf("kind %q is not allowed; use fact, decision or runbook", in.Kind), true
	}
	scope := memory.Scope(in.Scope)
	if in.Scope == "" {
		scope = memory.ScopeProject
	}
	if !scope.Valid() {
		return fmt.Sprintf("scope %q is not valid", in.Scope), true
	}
	if strings.TrimSpace(in.Text) == "" {
		return "text is required", true
	}
	err := r.store.Remember(ctx, memory.Entry{
		Kind: kind, Scope: scope, Agent: r.agent, Text: in.Text, Source: "tool:remember",
	})
	if err != nil {
		return err.Error(), true
	}
	return fmt.Sprintf("saved %s to %s memory", kind, scope), false
}

// Recall searches the long-term memory an agent may read.
type Recall struct {
	store memory.Store
	agent string
}

// NewRecall returns the recall tool for the named agent.
func NewRecall(store memory.Store, agent string) *Recall {
	return &Recall{store: store, agent: agent}
}

type recallInput struct {
	Query string `json:"query"`
	Limit int    `json:"limit"`
}

func (*Recall) Definition() api.ToolDef {
	return api.ToolDef{
		Name:        "recall",
		Description: "Search long-term memory for facts, decisions and runbooks saved earlier.",
		InputSchema: json.RawMessage(`{"type":"object","properties":{"query":{"type":"string"},"limit":{"type":"integer","minimum":1,"maximum":50}},"required":["query"]}`),
	}
}

func (*Recall) Risk() Risk { return RiskRead }

func (*Recall) Preview(input string) string {
	var in recallInput
	if err := json.Unmarshal([]byte(input), &in); err != nil {
		return "recall: invalid input"
	}
	return Redact("recall " + in.Query)
}

func (r *Recall) Execute(ctx context.Context, input string) (string, bool) {
	out, isErr := r.execute(ctx, input)
	return Redact(out), isErr
}

func (r *Recall) execute(ctx context.Context, input string) (string, bool) {
	var in recallInput
	if err := json.Unmarshal([]byte(input), &in); err != nil {
		return "invalid input: " + err.Error(), true
	}
	if in.Limit <= 0 || in.Limit > 50 {
		in.Limit = 10
	}
	entries, err := r.store.Recall(ctx, in.Query, r.agent, in.Limit)
	if err != nil {
		return err.Error(), true
	}
	if len(entries) == 0 {
		return "no memories match", false
	}
	var sb strings.Builder
	for _, e := range entries {
		fmt.Fprintf(&sb, "- [%s] %s\n", e.Kind, e.Text)
	}
	return strings.TrimRight(sb.String(), "\n"), false
}

func (r *Remember) RiskFor(string) Risk { return r.Risk() }

func (r *Recall) RiskFor(string) Risk { return r.Risk() }
