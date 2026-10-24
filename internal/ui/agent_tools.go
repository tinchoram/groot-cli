package ui

import (
	"strings"

	"github.com/tinchoram/groot-cli/internal/catalog"
)

// agentToolsCommand handles /agent tools <name> add|remove <tool>[,<tool>…]. The
// tools are validated and the agent's file is saved by the workspace.
func (m *Model) agentToolsCommand(rest []string) {
	if len(rest) < 3 || (rest[1] != "add" && rest[1] != "remove") {
		m.append(styleError.Render("usage: /agent tools <name> add|remove <tool>[,<tool>…]"))
		return
	}
	name, op := rest[0], rest[1]
	tools := catalog.SplitTools(rest[2:])
	var err error
	if op == "add" {
		err = m.ws.EditAgentTools(name, tools, nil)
	} else {
		err = m.ws.EditAgentTools(name, nil, tools)
	}
	if err != nil {
		m.append(styleError.Render("error: " + err.Error()))
		return
	}
	m.append(styleDim.Render("saved: " + op + " " + strings.Join(tools, ", ") + " for " + name + " (next conversation)"))
}
