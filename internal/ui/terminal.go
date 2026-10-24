package ui

import (
	"slices"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/tinchoram/groot-cli/internal/term"
)

// terminalShare is the part of the total width the terminal column takes.
const terminalShare = 45

// minTerminalWidth keeps the column usable on narrow terminals.
const minTerminalWidth = 30

// termOutputMsg says the shell's screen changed and should be redrawn.
type termOutputMsg struct{}

// focus panes, in tab order.
type pane int

const (
	paneSidebar pane = iota
	paneInput
	paneTerminal
)

// termShown reports whether the terminal column is on screen. It needs the
// chat beside it, so ctrl-e hides it with the chat.
func (m Model) termShown() bool {
	return m.termOpen && m.chatOpen && m.tm != nil
}

// termWidth is the width of the terminal column, or 0 when hidden.
func (m Model) termWidth() int {
	if !m.termShown() {
		return 0
	}
	return m.termColumnWidth()
}

// termColumnWidth is the width the column takes when it is shown: the width
// the user set, or the default share of the window.
func (m Model) termColumnWidth() int {
	return m.clampTerm(m.rawTerm())
}

// toggleTerminal shows or hides the terminal column. The first time, it starts
// the user's shell in the active space directory; the shell then keeps running
// while the column is hidden, and when the space changes.
func (m *Model) toggleTerminal() tea.Cmd {
	if m.termOpen {
		m.termOpen = false
		m.focusTerm = false
		m.focusSidebar = false
		m.resize()
		m.saveLayout()
		return nil
	}
	if m.tm == nil {
		space, _ := m.ws.ActiveSpace()
		tm := m.newTerm()
		cols, rows := m.termInner(m.termColumnWidth())
		if err := tm.Start(cols, rows, space.Path); err != nil {
			_ = tm.Close()
			m.append(styleError.Render("error: could not start the shell: " + err.Error()))
			return nil
		}
		m.tm = tm
	}
	m.termOpen = true
	m.focusTerm = true
	m.focusSidebar = false
	m.resize()
	m.saveLayout()
	return waitTerminalOutput(m.tm)
}

// termInner is the size of the shell's screen inside the column's border.
func (m Model) termInner(width int) (cols, rows int) {
	return max(width-2, 1), max(m.height-2, 1)
}

// resizeTerminal keeps the shell's screen the size of its column.
func (m *Model) resizeTerminal() {
	if m.tm == nil || !m.termShown() {
		return
	}
	cols, rows := m.termInner(m.termWidth())
	_ = m.tm.Resize(cols, rows)
}

// waitTerminalOutput blocks until the shell prints something. It is re-armed
// after every redraw while the shell lives.
func waitTerminalOutput(tm term.Terminal) tea.Cmd {
	return func() tea.Msg {
		<-tm.Output()
		return termOutputMsg{}
	}
}

// handleTerminalOutput redraws for new output, or closes the column when the
// shell has exited.
func (m *Model) handleTerminalOutput() tea.Cmd {
	if m.tm == nil {
		return nil
	}
	if !m.tm.Alive() {
		_ = m.tm.Close()
		m.tm = nil
		m.termOpen = false
		m.focusTerm = false
		m.append(styleDim.Render("terminal closed"))
		m.resize()
		return nil
	}
	m.refresh()
	return waitTerminalOutput(m.tm)
}

// closeTerminal releases the shell, if one is running.
func (m *Model) closeTerminal() {
	if m.tm != nil {
		_ = m.tm.Close()
		m.tm = nil
	}
}

// forwardToTerminal sends a key to the shell. Keys with no byte form are dropped.
func (m *Model) forwardToTerminal(msg tea.KeyMsg) {
	if m.tm == nil {
		return
	}
	if b := term.KeyBytes(msg); len(b) > 0 {
		_, _ = m.tm.Write(b)
	}
}

// currentPane is the pane that has the keyboard.
func (m Model) currentPane() pane {
	switch {
	case m.focusTerm:
		return paneTerminal
	case m.focusSidebar:
		return paneSidebar
	}
	return paneInput
}

// focusStop is one place the keyboard can be: a sidebar section, the input, or
// the terminal column. The sidebar contributes two stops, one per section.
type focusStop struct {
	pane    pane
	section sidebarSection
}

// focusOrder lists the visible stops in tab order: SPACES, AGENTS, input,
// terminal (only when shown).
func (m Model) focusOrder() []focusStop {
	var order []focusStop
	if m.sidebarOpen {
		order = append(order,
			focusStop{pane: paneSidebar, section: sectionSpaces},
			focusStop{pane: paneSidebar, section: sectionAgents})
	}
	if m.chatOpen {
		order = append(order, focusStop{pane: paneInput})
	}
	if m.termShown() {
		order = append(order, focusStop{pane: paneTerminal})
	}
	return order
}

// currentStop is the stop that has the keyboard.
func (m Model) currentStop() focusStop {
	switch {
	case m.focusTerm:
		return focusStop{pane: paneTerminal}
	case m.focusSidebar:
		return focusStop{pane: paneSidebar, section: m.section}
	}
	return focusStop{pane: paneInput}
}

// cycleFocus moves focus one stop forward (tab) or, with backward set, one
// stop back (shift+tab).
func (m *Model) cycleFocus(backward bool) {
	order := m.focusOrder()
	if len(order) == 0 {
		return
	}
	next := order[0]
	if i := slices.Index(order, m.currentStop()); i >= 0 {
		step := 1
		if backward {
			step = len(order) - 1
		}
		next = order[(i+step)%len(order)]
	}
	m.setStop(next)
}

// setStop gives the keyboard to a stop. Focusing a sidebar section selects
// its active row as the cursor.
func (m *Model) setStop(s focusStop) {
	m.focusSidebar = s.pane == paneSidebar
	m.focusTerm = s.pane == paneTerminal
	if m.focusSidebar {
		m.section = s.section
		m.cursor = m.activeIndex()
	}
	m.refresh()
}

// borderColor is the frame colour of a panel: green while it has the keyboard,
// dark brown otherwise. Only one panel can hold the keyboard.
func (m Model) borderColor(p pane) lipgloss.Color {
	if m.currentPane() == p {
		return lipgloss.Color(colorGreen)
	}
	return lipgloss.Color(colorDarkBrown)
}

// viewTerminal draws the terminal column: a rounded frame whose top border
// carries the title, with the shell's screen inside.
func (m Model) viewTerminal(width, height int) string {
	title := "terminal"
	if sp, ok := m.ws.ActiveSpace(); ok {
		title += " · " + sp.Name
	}
	border := m.borderColor(paneTerminal)
	if m.hover == dragTermEdge || m.drag == dragTermEdge {
		border = lipgloss.Color(colorGreen)
	}
	innerW, innerH := max(width-2, 1), max(height-2, 1)
	var rows []string
	if m.tm != nil {
		rows = m.tm.Render()
		if m.sel.active && m.selArea == areaTerminal {
			rows = highlightRows(rows, m.termRows(), m.sel)
		}
	}
	body := make([]string, innerH)
	for i := range body {
		row := ""
		if i < len(rows) {
			row = rows[i]
		}
		body[i] = fitCells(row, innerW)
	}
	bs := lipgloss.NewStyle().Foreground(border)
	top := bs.Render("╭") + bs.Render(titleFits(title, innerW)) + bs.Render("╮")
	var sb strings.Builder
	sb.WriteString(top)
	for _, row := range body {
		sb.WriteString("\n" + bs.Render("│") + row + bs.Render("│"))
	}
	sb.WriteString("\n" + bs.Render("╰"+strings.Repeat("─", innerW)+"╯"))
	return sb.String()
}

// titleFits builds the border segment that holds the title: "─ title ─…─".
func titleFits(title string, inner int) string {
	label := " " + title + " "
	if lipgloss.Width(label)+1 > inner {
		label = truncateCells(label, inner-1)
	}
	return "─" + label + strings.Repeat("─", max(inner-lipgloss.Width(label)-1, 0))
}

// fitCells pads or cuts a styled row to exactly n cells.
func fitCells(s string, n int) string {
	w := lipgloss.Width(s)
	if w > n {
		return truncateCells(s, n)
	}
	return s + strings.Repeat(" ", n-w)
}

// handleTerminalKey implements the terminal pane's keys: ctrl-t, ctrl-b and
// ctrl-e are groot's, tab moves focus, and everything else goes to the shell.
func (m Model) handleTerminalKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	m.forwardToTerminal(msg)
	return m, nil
}
