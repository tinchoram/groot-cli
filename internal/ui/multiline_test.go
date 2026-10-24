package ui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func altEnter(m Model) Model {
	next, _ := m.Update(tea.KeyMsg{Type: tea.KeyEnter, Alt: true})
	return next.(Model)
}

func TestAltEnterAndCtrlJInsertNewlines(t *testing.T) {
	chat := &fakeSession{model: "m"}
	m, _ := newTestModel(t, chat)
	m = typeText(m, "first")
	m = altEnter(m)
	m = typeText(m, "second")
	if m.input.Value() != "first\nsecond" {
		t.Fatalf("alt+enter: input = %q", m.input.Value())
	}
	next, _ := m.Update(tea.KeyMsg{Type: tea.KeyCtrlJ})
	m = next.(Model)
	if m.input.Value() != "first\nsecond\n" {
		t.Fatalf("ctrl+j: input = %q", m.input.Value())
	}
	if len(chat.sends) != 0 {
		t.Fatal("a newline key must not send")
	}
}

func TestEnterSendsMultilineText(t *testing.T) {
	chat := &fakeSession{model: "m", reply: "ok"}
	m, _ := newTestModel(t, chat)
	m = typeText(m, "a")
	m = altEnter(m)
	m = typeText(m, "b")
	m, cmd := press(m, tea.KeyEnter)
	next, _ := m.Update(findMsg[answerMsg](t, cmd))
	_ = next
	if len(chat.sends) != 1 || chat.sends[0] != "a\nb" {
		t.Fatalf("sends = %q", chat.sends)
	}
	if !strings.Contains(stripANSI(m.transcriptText()), "❯ a") || !strings.Contains(stripANSI(m.transcriptText()), "  b") {
		t.Fatalf("the echo prints the lines as typed:\n%s", m.transcriptText())
	}
}

func TestInputGrowsToEightLinesThenScrolls(t *testing.T) {
	m, _ := newTestModel(t, &fakeSession{model: "m"})
	m = typeText(m, "l1")
	if m.input.Height() != 1 {
		t.Fatalf("one line must be one row, got %d", m.input.Height())
	}
	for i := 2; i <= 12; i++ {
		m = altEnter(m)
		m = typeText(m, "l"+string(rune('0'+i%10)))
	}
	if m.input.LineCount() != 12 {
		t.Fatalf("lines = %d", m.input.LineCount())
	}
	if m.input.Height() != 8 {
		t.Fatalf("the box grows to 8 rows and then scrolls, got %d", m.input.Height())
	}
}

func TestHistoryUpRecallsOnlyFromTheFirstLine(t *testing.T) {
	m, _ := newTestModel(t, &fakeSession{model: "m"})
	m.hist.Push("earlier")
	m = typeText(m, "line one")
	m = altEnter(m)
	m = typeText(m, "line two")

	m, _ = press(m, tea.KeyUp)
	if m.input.Value() != "line one\nline two" {
		t.Fatalf("up on the second line must move the cursor, not recall: %q", m.input.Value())
	}
	m, _ = press(m, tea.KeyUp)
	if m.input.Value() != "earlier" {
		t.Fatalf("up on the first line recalls the history: %q", m.input.Value())
	}
}
