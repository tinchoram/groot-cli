package admintool

import (
	"context"
	"fmt"
	"strings"

	"github.com/tinchoram/groot-cli/internal/tool"
)

// NewGetContext returns get_context: the context table of a space, which is
// the same text as /context.
func NewGetContext(m Manager) tool.Tool {
	return getContext{base{"get_context",
		"Read the context of each agent in a space: its model, context window, the size of its last model answer, the percentage used, the compaction strategy in effect and whether a compaction would run now. Use it when the user asks about context use or compaction. Omit space for the active space.",
		`{"type":"object","properties":{"space":{"type":"string","description":"space name; omit for the active space"}},"additionalProperties":false}`,
		tool.RiskRead}, m}
}

type getContext struct {
	base
	m Manager
}

type contextInput struct {
	Space string `json:"space"`
	Model string `json:"model"`
	// Tokens is a pointer so that a missing value is refused, not read as zero.
	Tokens *int `json:"tokens"`
}

func (t getContext) Preview(input string) string {
	var in contextInput
	if err := decode(input, &in); err != nil || in.Space == "" {
		return "read the context of the active space"
	}
	return "read the context of space " + in.Space
}

func (t getContext) Execute(ctx context.Context, input string) (string, bool) {
	var in contextInput
	if err := decode(input, &in); err != nil {
		return err.Error(), true
	}
	out, err := t.m.ContextReport(ctx, in.Space)
	if err != nil {
		return err.Error(), true
	}
	return out, false
}

// NewSetContextWindow returns set_context_window: it saves the context window
// of a model, or of a prefix of its id, in groot.yaml.
func NewSetContextWindow(m Manager) tool.Tool {
	return setContextWindow{base{"set_context_window",
		"Set the context window, in tokens, of a model or of a model id prefix, saved in groot.yaml under models.context_window. It overrides the built-in table. Conversations use it from their next model call.",
		`{"type":"object","properties":{"model":{"type":"string","description":"Model id, or a prefix of one"},"tokens":{"type":"integer","minimum":1,"description":"Context window size in tokens; at least 1"}},"required":["model","tokens"],"additionalProperties":false}`,
		tool.RiskWrite}, m}
}

type setContextWindow struct {
	base
	m Manager
}

func (t setContextWindow) Preview(input string) string {
	var in contextInput
	if err := decode(input, &in); err != nil {
		return "set context window: invalid input"
	}
	return fmt.Sprintf("set the context window of %s to %d tokens", in.Model, derefInt(in.Tokens))
}

func (t setContextWindow) Execute(_ context.Context, input string) (string, bool) {
	var in contextInput
	if err := decode(input, &in); err != nil {
		return err.Error(), true
	}
	if strings.TrimSpace(in.Model) == "" {
		return "model is required", true
	}
	if in.Tokens == nil || *in.Tokens <= 0 {
		return "tokens is required and must be a positive number", true
	}
	if err := t.m.SetContextWindow(in.Model, *in.Tokens); err != nil {
		return err.Error(), true
	}
	return fmt.Sprintf("context window of %s set to %d tokens", in.Model, *in.Tokens), false
}

func derefInt(v *int) int {
	if v == nil {
		return 0
	}
	return *v
}
