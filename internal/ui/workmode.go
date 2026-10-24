package ui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"

	"github.com/tinchoram/groot-cli/internal/workspace"
)

// inputBorder is the input box frame: tan in work mode while the input has the
// keyboard, green otherwise.
func (m Model) inputBorder() lipgloss.Color {
	if m.session != nil && m.session.WorkMode() && m.currentPane() == paneInput {
		return lipgloss.Color(colorTan)
	}
	return m.borderColor(paneInput)
}

// hintLine is the hint under the input box. In work mode it says what is
// approved automatically and how to stop.
func (m Model) hintLine(hint string) string {
	if m.session == nil || !m.session.WorkMode() {
		return styleDim.Render(hint)
	}
	root := ""
	if sp, ok := m.ws.ActiveSpace(); ok {
		root = sp.Path
	}
	return styleTool.Render("  WORK MODE · auto-approving inside " + root + " · /work off to stop")
}

// workOnText is the transcript line of /work on, with the limits in force.
func (m Model) workOnText() string {
	root := ""
	if sp, ok := m.ws.ActiveSpace(); ok {
		root = sp.Path
	}
	turns, usd := m.ws.WorkLimits()
	return workspace.WorkOnLine(root, turns, usd)
}

// warningLine is the transcript text of a warning. Work mode announcements keep
// their own wording; everything else is a warning.
func warningLine(text string) string {
	if strings.HasPrefix(text, "work mode") || strings.HasPrefix(text, "⟶") {
		return text
	}
	return "warning: " + text
}

// firstLine is the first line of a tool preview, for the transcript.
func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return s[:i]
	}
	return s
}

// autoLine is the transcript line of a call that work mode approved on its own.
func autoLine(name, preview string) string {
	return fmt.Sprintf("[auto] %s %s", name, firstLine(preview))
}
