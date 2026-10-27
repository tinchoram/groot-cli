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
	head := "  " + styleAccentBold.Render(spaceName) + styleDim.Render(" ▸ ") +
		styleAccentBold.Render(agent) + styleDim.Render(" · "+m.session.Model()) + m.taskLabel() + m.workLabel() + m.sandboxLabel()
	return head + m.contextLabel(head)
}

// sandboxLabel warns, dimmed, in the header when bash runs without a sandbox.
// A working backend is not shown: the default should stay quiet.
func (m Model) sandboxLabel() string {
	if m.sandbox != "none" {
		return ""
	}
	return styleDim.Render(" · sandbox: none")
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

// statusLine is the single row above the input box. It starts with the agent
// badge, then a spinner while the agent works, otherwise the model and the
// session usage in dim text.
func (m Model) statusLine() string {
	lead := "  "
	if b := m.agentBadge(); b != "" {
		lead += b + " "
	}
	if b := m.needsYouLabel(); b != "" {
		lead += b + " "
	}
	switch {
	case m.closing:
		return lead + styleDim.Render("closing… (esc to skip summaries)")
	case m.pending != nil:
		return ""
	case m.busy:
		return lead + m.spin.View() + " " + styleDim.Render("thinking...")
	case m.othersBusy() > 0:
		return lead + styleDim.Render(strconv.Itoa(m.othersBusy())+" agents working in background")
	case m.note != "" && time.Now().Before(m.noteUntil):
		return lead + styleDim.Render(m.note)
	}
	parts := []string{m.session.Model()}
	if usage := statusUsage(m.session.Usage(), m.session.CostUSD(), m.showCost); usage != "" {
		parts = append(parts, usage)
	}
	return lead + styleDim.Render(strings.Join(parts, " · "))
}

// agentBadge is the name of the active agent as a highlighted label, or ""
// when no agent is selected.
func (m Model) agentBadge() string {
	agent := m.ws.ActiveAgent()
	if agent == "" {
		return ""
	}
	return styleAgentBadge.Render(agent)
}

// inputArea is the three-row bordered box plus its hint. An inline approval
// (no diff) replaces the input with a brown confirm box.
func (m Model) inputArea() string {
	w := max(m.chatWidth()-2, 20)
	if m.pending != nil {
		prompt := styleConfirmMark.Render(" ? ") + m.pending.prompt + styleDim.Render(" "+approvalChoices(m.pending.req))
		if line := destructiveLine(m.pending.req); line != "" {
			prompt = styleError.Render(" "+line) + "\n" + prompt
		}
		box := styleModalBorder.BorderForeground(lipgloss.Color(colorBrown)).Width(w).Render(prompt)
		hint := "  y: yes · n / esc / enter: no"
		if approvalChoices(m.pending.req) == "(y/n/a)" {
			hint = "  y: yes · a: yes for this session · n / esc / enter: no"
		}
		return box + "\n" + styleDim.Render(hint)
	}
	box := styleModalBorder.BorderForeground(m.inputBorder()).Width(w).Render(m.styledInput())
	hint := "  enter: send · shift-enter: newline · ↑/↓: history · tab: sidebar · ctrl-b: sidebar · ctrl-e: chat · ctrl-d: exit"
	list := ""
	if lines := m.completionLines(); len(lines) > 0 {
		hint = "  tab: complete · ↑/↓: choose · esc: close the list · ctrl-d: exit"
		list = strings.Join(lines, "\n") + "\n"
	}
	if m.focusTerm {
		hint = "  shift-tab: leave · ctrl-t: hide · ctrl-b / ctrl-e: groot · everything else: shell"
	}
	if m.focusSidebar {
		hint = "  ↑/↓: move · enter: select (space → agents, agent → input) · s/a: spaces/agents · tab/esc: back to input · ctrl-d: exit"
	}
	return list + box + "\n" + m.hintLine(hint)
}

// styleSidebarBorder is the sidebar frame: rounded, dark brown.
var styleSidebarBorder = lipgloss.NewStyle().
	Border(lipgloss.RoundedBorder()).
	BorderForeground(lipgloss.Color(colorDarkBrown)).
	Padding(0, 1)
