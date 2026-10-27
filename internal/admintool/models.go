package admintool

import (
	"context"
	"fmt"
	"strings"

	"github.com/tinchoram/groot-cli/internal/provider"
	"github.com/tinchoram/groot-cli/internal/tool"
)

// NewListModels returns the list_models tool: the models the provider offers,
// with their list prices.
func NewListModels(m Manager) tool.Tool {
	return listModels{base{"list_models",
		"List the models the provider offers, with their list prices per million tokens. Use it before set_agent_model, set_space_model or set_default_model to get a valid id. It only reads the provider's list and changes nothing.",
		`{"type":"object","properties":{},"additionalProperties":false}`,
		tool.RiskRead}, m}
}

type listModels struct {
	base
	m Manager
}

func (listModels) Preview(string) string { return "list models" }

func (t listModels) Execute(ctx context.Context, input string) (string, bool) {
	if strings.TrimSpace(input) != "" {
		if err := decode(input, &struct{}{}); err != nil {
			return err.Error(), true
		}
	}
	models, err := t.m.ListModels(ctx)
	if err != nil {
		return result("", err)
	}
	if len(models) == 0 {
		return result("no models", nil)
	}
	def, session := t.m.DefaultModel(), t.m.SessionModel()
	lines := make([]string, 0, len(models))
	for _, mi := range models {
		line := fmt.Sprintf("%s — %s · %s", mi.ID, mi.DisplayName, provider.PriceNote(mi.ID))
		if mi.ID == def {
			line += " [default]"
		}
		if mi.ID == session {
			line += " [session]"
		}
		lines = append(lines, line)
	}
	return result(strings.Join(lines, "\n"), nil)
}

// NewSetAgentModel returns the set_agent_model tool: it saves the model an agent
// runs on into the agent's file.
func NewSetAgentModel(m Manager) tool.Tool {
	return setAgentModel{base{"set_agent_model",
		"Set the model an agent runs on. The change is saved in the agent's file and applies to its next conversation, and the model id is checked against the provider's list when that list can be reached. Use list_models first to pick an id.",
		`{"type":"object","properties":{"agent":{"type":"string","description":"Agent whose model changes"},"model":{"type":"string","description":"Model id, as list_models shows it"}},"required":["agent","model"],"additionalProperties":false}`,
		tool.RiskWrite}, m}
}

type setAgentModelInput struct {
	Agent string `json:"agent"`
	Model string `json:"model"`
}

type setAgentModel struct {
	base
	m Manager
}

func (t setAgentModel) Preview(input string) string {
	var in setAgentModelInput
	if err := decode(input, &in); err != nil {
		return "set model: invalid input"
	}
	return "set model of " + in.Agent + " to " + in.Model
}

func (t setAgentModel) Execute(ctx context.Context, input string) (string, bool) {
	var in setAgentModelInput
	if err := decode(input, &in); err != nil {
		return err.Error(), true
	}
	if in.Agent == "" || in.Model == "" {
		return "agent and model are required", true
	}
	warn, err := t.m.SetAgentModel(ctx, in.Agent, in.Model)
	if err != nil {
		return result("", err)
	}
	out := fmt.Sprintf("model of %s set to %s", in.Agent, in.Model)
	if warn != "" {
		out += "\n" + warn
	}
	return result(out, nil)
}
