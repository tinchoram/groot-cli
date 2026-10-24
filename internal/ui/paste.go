package ui

import (
	"fmt"
	"strings"
	"unicode/utf8"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// A paste longer than pasteMaxLines lines or pasteMaxChars characters is kept
// aside. The input shows a placeholder instead, and sending the message puts
// the full text back.
const (
	pasteMaxLines  = 3
	pasteMaxChars  = 200
	pasteEchoRunes = 80
)

// stylePasteTag colors the placeholder of a collapsed paste.
var stylePasteTag = lipgloss.NewStyle().Foreground(lipgloss.Color(colorTan))

// pastedText is one collapsed paste: the placeholder shown in the input and the
// text it stands for.
type pastedText struct {
	label string
	text  string
}

// isLongPaste reports whether a paste is collapsed into a placeholder.
func isLongPaste(text string) bool {
	return lineCount(text) > pasteMaxLines || utf8.RuneCountInString(text) > pasteMaxChars
}

func lineCount(text string) int {
	return strings.Count(strings.ReplaceAll(text, "\r\n", "\n"), "\n") + 1
}

// insertPaste puts the placeholder of a long paste at the input cursor. Pastes
// in one message are numbered #1, #2, … until the message is sent.
func (m Model) insertPaste(text string) Model {
	text = strings.ReplaceAll(text, "\r\n", "\n")
	m.pasteSeq++
	label := fmt.Sprintf("[Pasted text #%d +%d lines]", m.pasteSeq, lineCount(text))
	m.pastes = append(append([]pastedText(nil), m.pastes...), pastedText{label: label, text: text})
	m.input, _ = m.input.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(label)})
	return m
}

// expandPastes puts the full text of every placeholder back into text.
func expandPastes(text string, pastes []pastedText) string {
	for _, p := range pastes {
		text = strings.Replace(text, p.label, p.text, 1)
	}
	return text
}

// removePasteAtCursor drops a whole paste when backspace follows its
// placeholder. It reports false when the cursor is not right after one.
func (m Model) removePasteAtCursor() (Model, bool) {
	row := m.input.Line()
	lines := strings.Split(m.input.Value(), "\n")
	if row >= len(lines) {
		return m, false
	}
	info := m.input.LineInfo()
	runes := []rune(lines[row])
	col := min(info.StartColumn+info.ColumnOffset, len(runes))
	before := string(runes[:col])
	for i, p := range m.pastes {
		if !strings.HasSuffix(before, p.label) {
			continue
		}
		// The textarea deletes one character per backspace, so the placeholder
		// goes in as many backspaces as it has characters.
		for range utf8.RuneCountInString(p.label) {
			m.input, _ = m.input.Update(tea.KeyMsg{Type: tea.KeyBackspace})
		}
		m.pastes = append(append([]pastedText(nil), m.pastes[:i]...), m.pastes[i+1:]...)
		return m, true
	}
	return m, false
}

// styledInput is the input line with each placeholder in tan.
func (m Model) styledInput() string {
	view := m.input.View()
	for _, p := range m.pastes {
		view = strings.Replace(view, p.label, stylePasteTag.Render(p.label), 1)
	}
	return view
}

// pasteEchoLines are the dim lines under a sent message: the first line of each
// collapsed paste, so the transcript shows what was pasted without all of it.
func pasteEchoLines(pastes []pastedText) []string {
	out := make([]string, 0, len(pastes))
	for _, p := range pastes {
		first := strings.TrimSpace(strings.SplitN(p.text, "\n", 2)[0])
		if r := []rune(first); len(r) > pasteEchoRunes {
			first = string(r[:pasteEchoRunes]) + "…"
		}
		out = append(out, styleDim.Render("  ↳ "+first))
	}
	return out
}
