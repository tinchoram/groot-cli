package admintool

import (
	"context"
	"fmt"

	"github.com/tinchoram/groot-cli/internal/tool"
)

// NewSetDefaultModel returns set_default_model: it makes a model the default for
// new conversations and writes it into the configuration file.
func NewSetDefaultModel(m Manager) tool.Tool {
	return setDefaultModel{base{"set_default_model",
		"Set the default model: the one new conversations use when their agent sets none. It is written to the configuration file. The id is checked against the provider's list when that list can be reached.",
		`{"type":"object","properties":{"model":{"type":"string"}},"required":["model"],"additionalProperties":false}`,
		tool.RiskWrite}, m}
}

type setDefaultModel struct {
	base
	m Manager
}

type defaultModelInput struct {
	Model string `json:"model"`
}

func (t setDefaultModel) Preview(input string) string {
	var in defaultModelInput
	if err := decode(input, &in); err != nil {
		return "set default model: invalid input"
	}
	return "set default model to " + in.Model
}

func (t setDefaultModel) Execute(ctx context.Context, input string) (string, bool) {
	var in defaultModelInput
	if err := decode(input, &in); err != nil {
		return err.Error(), true
	}
	if in.Model == "" {
		return "model is required", true
	}
	warn, err := t.m.SetDefaultModel(ctx, in.Model)
	if err != nil {
		return result("", err)
	}
	out := fmt.Sprintf("default model set to %s", in.Model)
	if warn != "" {
		out += "\n" + warn
	}
	return result(out, nil)
}
