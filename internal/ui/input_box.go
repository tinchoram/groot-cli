package ui

import (
	"strings"
	"unicode/utf8"

	"github.com/charmbracelet/bubbles/textarea"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// The input grows from one row to inputMaxRows with its content, then scrolls.
const (
	inputMinRows = 1
	inputMaxRows = 8
	// inputPromptCols is the width of the prompt: "❯ " or the indent after it.
	inputPromptCols = 2
)

// newInputArea builds the chat input: a prompt on the first line, an indent on
// the others, and no line numbers or cursor-line highlight.
func newInputArea() textarea.Model {
	ta := textarea.New()
	ta.ShowLineNumbers = false
	ta.CharLimit = 0
	ta.MaxHeight = inputMaxRows
	ta.FocusedStyle.CursorLine = lipgloss.NewStyle()
	ta.SetPromptFunc(inputPromptCols, func(line int) string {
		if line == 0 {
			return stylePrompt.Render("❯ ")
		}
		return "  "
	})
	ta.SetHeight(inputMinRows)
	_ = ta.Focus()
	return ta
}

// isNewlineKey reports the keys that insert a newline instead of sending:
// alt+enter, shift+enter where the terminal reports it, and ctrl+j.
func isNewlineKey(msg tea.KeyMsg) bool {
	return (msg.Type == tea.KeyEnter && msg.Alt) || msg.Type == tea.KeyCtrlJ || msg.String() == "shift+enter"
}

// inputRows is how many rows the input needs: each line takes as many rows as
// it wraps to, within the bounds.
func (m Model) inputRows() int {
	w := max(m.input.Width()-inputPromptCols, 1)
	rows := 0
	for _, line := range strings.Split(m.input.Value(), "\n") {
		rows += max(1, (utf8.RuneCountInString(line)+w-1)/w)
	}
	return min(max(rows, inputMinRows), inputMaxRows)
}

// fitInput resizes the chat when the input changed its number of rows.
func (m *Model) fitInput() {
	if m.inputRows() != m.input.Height() {
		m.resize()
	}
}

// handleKeyFitted handles a key and then fits the chat to the input's rows, so a
// key that adds or removes a line resizes the transcript.
func (m Model) handleKeyFitted(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	next, cmd := m.handleKey(msg)
	if nm, ok := next.(Model); ok {
		nm.fitInput()
		return nm, cmd
	}
	return next, cmd
}

// onFirstInputLine reports whether the cursor is on the first line of the input.
// Up goes to the history only from there.
func (m Model) onFirstInputLine() bool { return m.input.Line() == 0 }

// onLastInputLine reports whether the cursor is on the last line of the input.
// Down goes to the history only from there.
func (m Model) onLastInputLine() bool { return m.input.Line() >= m.input.LineCount()-1 }
