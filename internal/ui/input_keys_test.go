package ui

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

// Home and End (fn+← and fn+→ on a Mac keyboard) move the cursor inside the
// input when it holds text. With an empty input they scroll the transcript,
// as before.
func TestHomeAndEndMoveTheCursorInsideTheInput(t *testing.T) {
	m, _ := newTestModel(t, &fakeSession{model: "m"})
	m = typeText(m, "hello world")
	if got := m.input.LineInfo().ColumnOffset; got != 11 {
		t.Fatalf("cursor after typing at column %d, want 11", got)
	}
	m, _ = press(m, tea.KeyHome)
	if got := m.input.LineInfo().ColumnOffset; got != 0 {
		t.Fatalf("home must move the cursor to the start, got column %d", got)
	}
	m, _ = press(m, tea.KeyEnd)
	if got := m.input.LineInfo().ColumnOffset; got != 11 {
		t.Fatalf("end must move the cursor to the end, got column %d", got)
	}
	if m.input.Value() != "hello world" {
		t.Fatalf("home and end must not change the text: %q", m.input.Value())
	}
}

func TestHomeWithEmptyInputLeavesTheInputAlone(t *testing.T) {
	m, _ := newTestModel(t, &fakeSession{model: "m"})
	m, _ = press(m, tea.KeyHome)
	m, _ = press(m, tea.KeyEnd)
	if m.input.Value() != "" {
		t.Fatalf("input changed: %q", m.input.Value())
	}
}
