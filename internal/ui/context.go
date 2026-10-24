package ui

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/charmbracelet/lipgloss"

	"github.com/tinchoram/groot-cli/internal/provider"
	"github.com/tinchoram/groot-cli/internal/workspace"
)

// contextUsage is the reply to a /context command that does not parse.
const contextUsage = "usage: /context | /context all | /context window <model> <tokens>"

// contextStyle is the style of a context fill: dim below 70 percent, the
// warning style from 70 and the error style from 90.
func contextStyle(tokens int64, window int) lipgloss.Style {
	switch workspace.ContextLevelOf(tokens, window) {
	case workspace.ContextFull:
		return styleError
	case workspace.ContextWarn:
		return styleTool
	}
	return styleDim
}

// contextBadge is the fill of an agent's context after its name: "42%". It
// shows nothing until a context size is known.
func contextBadge(tokens int64, window int) string {
	if tokens <= 0 || window <= 0 {
		return ""
	}
	return contextStyle(tokens, window).Render(fmt.Sprintf("%d%%", provider.ContextPercent(tokens, window)))
}

// joinBadges puts the non-empty badges after a name, in order, separated by a
// dim dot, with a leading space. It returns "" when every badge is empty.
func joinBadges(badges ...string) string {
	var kept []string
	for _, b := range badges {
		if b = strings.TrimSpace(b); b != "" {
			kept = append(kept, b)
		}
	}
	if len(kept) == 0 {
		return ""
	}
	return " " + strings.Join(kept, styleDim.Render(" · "))
}

// agentBadges is the spend and context badges of an agent of the active space.
func (m Model) agentBadges(agent string) string {
	ctx := m.contexts[agent]
	return joinBadges(m.agentSpendBadge(agent), contextBadge(ctx.Tokens, ctx.Window))
}

// contextLabel is the context of the active conversation in the header:
// "ctx 84k/200k 42%" when it fits beside the head, else "ctx 42%". It is empty
// before the first model answer.
func (m Model) contextLabel(head string) string {
	tokens, window := m.session.ContextTokens(), m.session.ContextWindow()
	if tokens <= 0 || window <= 0 {
		return ""
	}
	pct := provider.ContextPercent(tokens, window)
	style := contextStyle(tokens, window)
	full := fmt.Sprintf("ctx %s/%s %d%%", workspace.FormatTokens(tokens), workspace.FormatTokens(int64(window)), pct)
	short := fmt.Sprintf("ctx %d%%", pct)
	text := full
	if lipgloss.Width(head)+lipgloss.Width(" · "+full) > m.chatWidth() {
		text = short
	}
	return styleDim.Render(" · ") + style.Render(text)
}

// refreshContext re-reads the context of the agents of the active space behind
// the sidebar badges.
func (m *Model) refreshContext() {
	rows, err := m.ws.Contexts(context.Background(), false)
	m.contexts = map[string]workspace.ContextRow{}
	if err != nil {
		return
	}
	for _, r := range rows {
		m.contexts[r.Agent] = r
	}
}

// runContextCommand handles /context, /context all and /context window <model> <tokens>.
func (m *Model) runContextCommand(arg string) {
	fields := strings.Fields(arg)
	switch {
	case len(fields) == 0 || (len(fields) == 1 && fields[0] == "all"):
		rows, err := m.ws.Contexts(context.Background(), len(fields) == 1)
		if err != nil {
			m.append(styleError.Render("error: " + err.Error()))
			return
		}
		m.append(workspace.FormatContexts(rows))
		return
	case len(fields) == 3 && fields[0] == "window":
		tokens, err := strconv.Atoi(fields[2])
		if err != nil || tokens <= 0 {
			m.append(styleError.Render(contextUsage))
			return
		}
		if err := m.ws.SetContextWindow(fields[1], tokens); err != nil {
			m.append(styleError.Render("error: " + err.Error()))
			return
		}
		m.append(styleDim.Render(fmt.Sprintf("context window of %s set to %d tokens", fields[1], tokens)))
		m.refreshContext()
	default:
		m.append(styleError.Render(contextUsage))
	}
}
