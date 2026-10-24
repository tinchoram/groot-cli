package ui

import (
	"strconv"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
)

// View renders the screen: the sidebar and the chat side by side, the chat
// alone, or the sidebar alone when the chat is hidden. Approvals are the inline
// box above the input.
func (m Model) View() string {
	if !m.chatOpen {
		return m.viewSidebar(m.width, m.height, true)
	}
	var cols []string
	if m.sidebarOpen {
		cols = append(cols, m.viewSidebar(m.sidebarCols(), m.height, false))
	}
	cols = append(cols, m.viewChat())
	if m.termShown() {
		cols = append(cols, m.viewTerminal(m.termWidth(), m.height))
	}
	return lipgloss.JoinHorizontal(lipgloss.Top, cols...)
}

// viewChat is the chat column: header, transcript, status line, input.
func (m Model) viewChat() string {
	w := m.chatWidth()
	return lipgloss.NewStyle().MaxWidth(w).Render(lipgloss.JoinVertical(lipgloss.Left,
		m.header(),
		m.vp.View(),
		m.statusLine(),
		m.inputArea(),
	))
}

// header names the conversation: space ▸ agent · model.
func (m Model) header() string {
	space, _ := m.ws.ActiveSpace()
	spaceName := space.Name
	if spaceName == "" {
		spaceName = "no space"
	}
	agent := m.ws.ActiveAgent()
	if agent == "" {
		agent = "no agent"
	}
	return "  " + styleAccentBold.Render(spaceName) + styleDim.Render(" ▸ ") +
		styleAccentBold.Render(agent) + styleDim.Render(" · "+m.session.Model()) + m.taskLabel() + m.workLabel()
}

// viewSidebar draws the bordered sidebar at the given outer size.
func (m Model) viewSidebar(w, h int, full bool) string {
	inner := max(w-4, 4)
	lines := m.sidebarLines(inner, full)
	if maxRows := max(h-2, 1); len(lines) > maxRows {
		lines = lines[:maxRows]
	}
	border := m.borderColor(paneSidebar)
	if m.hover == dragSidebarEdge || m.drag == dragSidebarEdge {
		border = lipgloss.Color(colorGreen)
	}
	return styleSidebarBorder.
		BorderForeground(border).
		Width(max(w-2, 4)).
		Height(max(h-2, 1)).
		Render(strings.Join(lines, "\n"))
}

// statusLine is the single row above the input box: a spinner while the
// agent works, otherwise the model and the session usage in dim text.
func (m Model) statusLine() string {
	switch {
	case m.closing:
		return "  " + styleDim.Render("closing… (esc to skip summaries)")
	case m.pending != nil:
		return ""
	case m.busy:
		return "  " + m.spin.View() + " " + styleDim.Render("thinking...")
	case m.othersBusy() > 0:
		return "  " + styleDim.Render(strconv.Itoa(m.othersBusy())+" agents working in background")
	case m.note != "" && time.Now().Before(m.noteUntil):
		return "  " + styleDim.Render(m.note)
	}
	parts := []string{m.session.Model()}
	if usage := statusUsage(m.session.Usage(), m.session.CostUSD()); usage != "" {
		parts = append(parts, usage)
	}
	return "  " + styleDim.Render(strings.Join(parts, " · "))
}

// inputArea is the three-row bordered box plus its hint. An inline approval
// (no diff) replaces the input with a brown confirm box.
func (m Model) inputArea() string {
	w := max(m.chatWidth()-2, 20)
	if m.pending != nil {
		prompt := styleConfirmMark.Render(" ? ") + m.pending.prompt + styleDim.Render(" "+approvalChoices(m.pending.req))
		box := styleModalBorder.BorderForeground(lipgloss.Color(colorBrown)).Width(w).Render(prompt)
		hint := "  y: yes · n / esc / enter: no"
		if approvalChoices(m.pending.req) == "(y/n/a)" {
			hint = "  y: yes · a: yes for this session · n / esc / enter: no"
		}
		return box + "\n" + styleDim.Render(hint)
	}
	box := styleModalBorder.BorderForeground(m.inputBorder()).Width(w).Render(m.styledInput())
	hint := "  enter: send · shift-enter: newline · ↑/↓: history · tab: sidebar · ctrl-b: sidebar · ctrl-e: chat · ctrl-d: exit"
	if m.focusTerm {
		hint = "  shift-tab: leave · ctrl-t: hide · ctrl-b / ctrl-e: groot · everything else: shell"
	}
	if m.focusSidebar {
		hint = "  ↑/↓: move · enter: select (space → agents, agent → input) · s/a: spaces/agents · tab/esc: back to input · ctrl-d: exit"
	}
	return box + "\n" + m.hintLine(hint)
}

// styleSidebarBorder is the sidebar frame: rounded, dark brown.
var styleSidebarBorder = lipgloss.NewStyle().
	Border(lipgloss.RoundedBorder()).
	BorderForeground(lipgloss.Color(colorDarkBrown)).
	Padding(0, 1)
