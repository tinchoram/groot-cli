package ui

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/tinchoram/groot-cli/internal/provider"
)

// modelsTimeout bounds the calls to the provider from /models and /model.
const modelsTimeout = 10 * time.Second

// runModelsCommand lists the models the provider offers, with their prices.
// The session's current model is marked.
func (m *Model) runModelsCommand() {
	ctx, cancel := context.WithTimeout(context.Background(), modelsTimeout)
	defer cancel()
	models, err := m.ws.ListModels(ctx)
	if err != nil {
		m.append(styleError.Render("error: " + err.Error()))
		return
	}
	if len(models) == 0 {
		m.append(styleDim.Render("no models"))
		return
	}
	current := m.session.Model()
	def := m.ws.DefaultModel()
	for _, mi := range models {
		mark := "  "
		if mi.ID == current {
			mark = "● "
		}
		tags := ""
		if mi.ID == def {
			tags += " [default]"
		}
		if mi.ID == current {
			tags += " [session]"
		}
		m.append(styleDim.Render(fmt.Sprintf("%s%s  %s  %s%s", mark, mi.ID, mi.DisplayName, provider.PriceNote(mi.ID), tags)))
	}
}

// runModelCommand handles /model. With no argument it shows the current model;
// "list" and "ls" list the models like /models. Otherwise the id is checked
// against the provider's list and refused with suggestions when it is unknown.
// --force skips the check. --agent also saves the model into the active agent's
// file. A refused id changes nothing.
func (m *Model) runModelCommand(arg string) {
	fields := strings.Fields(arg)
	persist, force, setDefault := false, false, false
parse:
	for len(fields) > 0 {
		switch fields[len(fields)-1] {
		case "--agent":
			persist = true
		case "--force":
			force = true
		case "--default":
			setDefault = true
		default:
			break parse
		}
		fields = fields[:len(fields)-1]
	}
	if len(fields) == 1 && (fields[0] == "list" || fields[0] == "ls") && !persist && !force {
		m.runModelsCommand()
		return
	}
	if len(fields) == 0 {
		m.append(styleDim.Render("current: ") + styleAccentBold.Render(m.session.Model()))
		return
	}
	id := strings.Join(fields, " ")
	if setDefault {
		ctx, cancel := context.WithTimeout(context.Background(), modelsTimeout)
		defer cancel()
		warn, err := m.ws.SetDefaultModel(ctx, id, force)
		if err != nil {
			m.append(styleError.Render("error: " + err.Error()))
			return
		}
		m.append(styleDim.Render("default model set to " + id + " in " + m.ws.ConfigFile() +
			" · new conversations use it; open conversations and agents with their own model keep theirs"))
		if warn != "" {
			m.append(styleDim.Render(warn))
		}
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), modelsTimeout)
	defer cancel()
	warn, err := m.ws.CheckModel(ctx, id, force)
	if err != nil {
		m.append(styleError.Render("error: " + err.Error()))
		return
	}
	if persist {
		agent := m.ws.ActiveAgent()
		if err := m.ws.SaveAgentModel(agent, id); err != nil {
			m.append(styleError.Render("error: " + err.Error()))
			return
		}
		m.append(styleDim.Render("model saved to " + agent))
	}
	if warn != "" {
		m.append(styleDim.Render(warn))
	}
	if err := m.session.SetModel(id); err != nil {
		m.append(styleError.Render("error: " + err.Error()))
		return
	}
	m.append(styleDim.Render("model: ") + styleAccentBold.Render(id))
}
