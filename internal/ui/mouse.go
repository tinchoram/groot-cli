package ui

import (
	"fmt"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/tinchoram/groot-cli/internal/term"
)

// Column widths are kept in these bounds. The chat is the column that gives
// way last: it never goes under minChatCols while the other columns are open.
const (
	minSidebarCols = 20
	minTermCols    = 20
	minChatCols    = 40
	noteDuration   = 1500 * time.Millisecond
	clickWindow    = 500 * time.Millisecond
)

// area is the column under the pointer.
type area int

const (
	areaNone area = iota
	areaSidebar
	areaChat
	areaTerminal
)

// dragKind is a divider being dragged or hovered.
type dragKind int

const (
	dragNone dragKind = iota
	dragSidebarEdge
	dragTermEdge
)

// clearNoteMsg removes the copy note once it has been shown long enough.
type clearNoteMsg struct{}

func clearNoteAfter() tea.Cmd {
	return tea.Tick(noteDuration, func(time.Time) tea.Msg { return clearNoteMsg{} })
}

// rawSidebar is the sidebar width as stored (or the default), before clamping.
func (m Model) rawSidebar() int {
	if m.sidebarW > 0 {
		return m.sidebarW
	}
	return sidebarWidth
}

// rawTerm is the terminal width as stored, or 45% of the window when unset.
func (m Model) rawTerm() int {
	if m.termW > 0 {
		return m.termW
	}
	return m.width * terminalShare / 100
}

// clampTerm keeps the terminal column between its minimum and what the other
// columns leave free.
func (m Model) clampTerm(v int) int {
	side := 0
	if m.sidebarOpen {
		side = m.rawSidebar()
	}
	maxT := max(m.width-side-minChatCols, minTermCols)
	return min(max(v, minTermCols), maxT)
}

// clampSidebar keeps the sidebar between its minimum and what the chat and the
// terminal leave free.
func (m Model) clampSidebar(v int) int {
	maxS := max(m.width-m.termWidth()-minChatCols, minSidebarCols)
	return min(max(v, minSidebarCols), maxS)
}

// sidebarCols is the width of the sidebar column (the whole window when the chat
// is hidden, 0 when the sidebar is hidden).
func (m Model) sidebarCols() int {
	switch {
	case !m.chatOpen:
		return m.width
	case !m.sidebarOpen:
		return 0
	}
	return m.clampSidebar(m.rawSidebar())
}

// chatEnd is the first column of the terminal (or the window's right edge).
func (m Model) chatEnd() int {
	return m.sidebarCols() + m.chatWidth()
}

// areaAt names the column at screen column x.
func (m Model) areaAt(x int) area {
	switch {
	case !m.chatOpen:
		return areaSidebar
	case m.sidebarOpen && x < m.sidebarCols():
		return areaSidebar
	case m.termShown() && x >= m.chatEnd():
		return areaTerminal
	}
	return areaChat
}

// dividerAt names the divider under column x, if there is one.
func (m Model) dividerAt(x int) dragKind {
	if !m.chatOpen {
		return dragNone
	}
	if m.sidebarOpen && x == m.sidebarCols()-1 {
		return dragSidebarEdge
	}
	if m.termShown() && x == m.chatEnd() {
		return dragTermEdge
	}
	return dragNone
}

// handleMouse routes a mouse event: clicks focus and select, drags select text
// or move dividers, and the wheel scrolls the chat.
func (m Model) handleMouse(ev tea.MouseMsg) (tea.Model, tea.Cmd) {
	switch ev.Action {
	case tea.MouseActionPress:
		if ev.Button == tea.MouseButtonWheelUp || ev.Button == tea.MouseButtonWheelDown {
			return m.wheel(ev)
		}
		if ev.Button != tea.MouseButtonLeft {
			return m, nil
		}
		if d := m.dividerAt(ev.X); d != dragNone {
			m.drag, m.hover = d, d
			m.clearSelection()
			return m, nil
		}
		return m.press(ev)

	case tea.MouseActionMotion:
		if m.drag != dragNone {
			m.dragTo(ev.X)
			return m, nil
		}
		if m.forwardMouse {
			m.sendTermMouse(term.MouseDrag, ev.Button, ev.X, ev.Y)
			return m, nil
		}
		if m.selecting {
			m.extendSelection(ev.X, ev.Y)
			m.refresh()
			return m, nil
		}
		m.hover = m.dividerAt(ev.X)
		return m, nil

	case tea.MouseActionRelease:
		if m.drag != dragNone {
			m.drag = dragNone
			m.hover = m.dividerAt(ev.X)
			m.resize()
			m.saveLayout()
			return m, nil
		}
		if m.forwardMouse {
			m.forwardMouse = false
			m.sendTermMouse(term.MouseUp, ev.Button, ev.X, ev.Y)
			return m, nil
		}
		if m.selecting {
			m.selecting = false
			return m.finishSelection()
		}
	}
	return m, nil
}

// press handles a left click that is not on a divider.
func (m Model) press(ev tea.MouseMsg) (tea.Model, tea.Cmd) {
	m.clearSelection()
	clicks := m.registerClick(ev.X, ev.Y)
	switch m.areaAt(ev.X) {
	case areaSidebar:
		m.focusSidebar, m.focusTerm = true, false
		m.sidebarClick(ev.Y)
	case areaChat:
		m.focusSidebar, m.focusTerm = false, false
		if ev.Y >= 1 && ev.Y <= m.vp.Height {
			row := m.vp.YOffset + ev.Y - 1
			m.startSelection(areaChat, row, ev.X-m.sidebarCols(), clicks, m.contentRow(row))
		}
	case areaTerminal:
		m.focusTerm, m.focusSidebar = true, false
		if m.tm != nil && m.tm.Tracking() {
			m.forwardMouse = true
			m.sendTermMouse(term.MouseDown, ev.Button, ev.X, ev.Y)
			break
		}
		row := ev.Y - 1
		rows := m.termRows()
		if row >= 0 && row < len(rows) {
			m.startSelection(areaTerminal, row, ev.X-m.chatEnd()-1, clicks, rows[row])
		}
	}
	m.refresh()
	return m, nil
}

// registerClick counts clicks on the same cell in quick succession: 1, 2 or 3.
func (m *Model) registerClick(x, y int) int {
	now := time.Now()
	if now.Sub(m.lastClickAt) <= clickWindow && x == m.lastClickX && y == m.lastClickY && m.clickCount < 3 {
		m.clickCount++
	} else {
		m.clickCount = 1
	}
	m.lastClickAt, m.lastClickX, m.lastClickY = now, x, y
	return m.clickCount
}

// sidebarClick selects the space or agent on screen row y.
func (m *Model) sidebarClick(y int) {
	inner := m.width - 4
	if m.chatOpen {
		inner = m.sidebarCols() - 4
	}
	rows := m.sidebarRows(inner, !m.chatOpen)
	k := y - 1 // the top border is row 0
	if k < 0 || k >= len(rows) {
		return
	}
	switch rows[k].kind {
	case rowSpace:
		m.section, m.cursor = sectionSpaces, rows[k].index
		if spaces := m.ws.Spaces(); rows[k].index < len(spaces) {
			m.useSpace(spaces[rows[k].index].Name)
		}
	case rowAgent:
		m.section, m.cursor = sectionAgents, rows[k].index
		if agents := m.ws.Agents(); rows[k].index < len(agents) {
			m.selectAgent(agents[rows[k].index].Name)
		}
	}
}

// startSelection begins a selection at (row, col) in the given area. One click
// selects a cell, two clicks a word, three a line.
func (m *Model) startSelection(a area, row, col, clicks int, plain string) {
	from, to := col, col+1
	switch clicks {
	case 2:
		from, to = wordAt(plain, col)
	case 3:
		from, to = lineSpan(plain)
	}
	if from < 0 {
		from = 0
	}
	if to < from+1 {
		to = from + 1
	}
	m.selArea = a
	m.selecting = true
	m.sel = selection{
		anchor: point{row, from},
		cursor: point{row, to - 1},
		active: true,
	}
}

// extendSelection moves the moving end of the selection to screen cell (x, y).
func (m *Model) extendSelection(x, y int) {
	switch m.selArea {
	case areaChat:
		row := min(max(m.vp.YOffset+y-1, 0), max(len(m.contentPlain)-1, 0))
		m.sel.cursor = point{row, x - m.sidebarCols()}
	case areaTerminal:
		rows := m.termRows()
		row := min(max(y-1, 0), max(len(rows)-1, 0))
		m.sel.cursor = point{row, x - m.chatEnd() - 1}
	}
}

// finishSelection copies the selected text. A plain click with no drag only
// clears the selection.
func (m Model) finishSelection() (tea.Model, tea.Cmd) {
	if m.sel.anchor == m.sel.cursor && m.clickCount == 1 {
		m.clearSelection()
		return m, nil
	}
	text := extractText(m.selectionRows(), m.sel)
	if text == "" {
		m.clearSelection()
		return m, nil
	}
	_ = m.clip.Copy(text)
	m.note = fmt.Sprintf("copied %d chars", len([]rune(text)))
	m.noteUntil = time.Now().Add(noteDuration)
	m.refresh()
	return m, clearNoteAfter()
}

func (m *Model) clearSelection() {
	if m.sel.active {
		m.sel = selection{}
		m.refresh()
	}
	m.selecting = false
}

// selectionRows are the plain rows the active selection refers to.
func (m Model) selectionRows() []string {
	if m.selArea == areaTerminal {
		return m.termRows()
	}
	return m.contentPlain
}

// contentRow is one plain row of the chat content.
func (m Model) contentRow(row int) string {
	if row >= 0 && row < len(m.contentPlain) {
		return m.contentPlain[row]
	}
	return ""
}

// termRows is the shell's screen as plain text.
func (m Model) termRows() []string {
	if m.tm == nil {
		return nil
	}
	rows := m.tm.Render()
	out := make([]string, len(rows))
	for i, r := range rows {
		out[i] = stripCells(r)
	}
	return out
}

// sendTermMouse forwards a mouse event to the shell's program. The screen
// position is relative to the terminal's inner area.
func (m *Model) sendTermMouse(kind term.MouseKind, b tea.MouseButton, x, y int) {
	if m.tm == nil {
		return
	}
	button := 0
	switch b {
	case tea.MouseButtonMiddle:
		button = 1
	case tea.MouseButtonRight:
		button = 2
	}
	_ = m.tm.SendMouse(term.MouseEvent{Kind: kind, Button: button, X: x - m.chatEnd() - 1, Y: y - 1})
}

// wheel scrolls the chat, or passes the wheel to a terminal program that tracks the mouse.
func (m Model) wheel(ev tea.MouseMsg) (tea.Model, tea.Cmd) {
	switch m.areaAt(ev.X) {
	case areaChat:
		var cmd tea.Cmd
		m.vp, cmd = m.vp.Update(ev)
		return m, cmd
	case areaTerminal:
		if m.tm != nil && m.tm.Tracking() {
			kind := term.MouseWheelUp
			if ev.Button == tea.MouseButtonWheelDown {
				kind = term.MouseWheelDown
			}
			m.sendTermMouse(kind, tea.MouseButtonLeft, ev.X, ev.Y)
		}
	}
	return m, nil
}

// dragTo moves the divider being dragged to column x.
func (m *Model) dragTo(x int) {
	switch m.drag {
	case dragSidebarEdge:
		m.sidebarW = x + 1
	case dragTermEdge:
		m.termW = m.width - x
	}
	m.resize()
}

// handlePaste sends pasted text where the keyboard is. In the chat input a
// short paste has its newlines turned into spaces and a long one is collapsed
// into a placeholder; the terminal column gets the text as the terminal sent it.
func (m Model) handlePaste(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	text := string(msg.Runes)
	switch {
	case m.focusTerm && m.tm != nil:
		_ = m.tm.Paste(text)
	case m.focusSidebar:
	default:
		text = normalizeNewlines(text)
		if isLongPaste(text) {
			return m.insertPaste(text), nil
		}
		collapsed := strings.ReplaceAll(text, "\n", " ")
		var cmd tea.Cmd
		m.input, cmd = m.input.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(collapsed)})
		return m, cmd
	}
	return m, nil
}

// saveLayout writes the current columns to the layout file, when there is one.
func (m *Model) saveLayout() {
	if m.layoutPath == "" {
		return
	}
	_ = SaveLayout(m.layoutPath, Layout{
		SidebarWidth:    m.sidebarCols(),
		TerminalWidth:   m.rawTermStored(),
		SidebarVisible:  m.sidebarOpen,
		TerminalVisible: m.termOpen,
	})
}

// rawTermStored is the terminal width the user set, or 0 for the default share.
func (m Model) rawTermStored() int {
	if m.termW > 0 {
		return m.clampTerm(m.termW)
	}
	return 0
}
