package ui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"

	"github.com/tinchoram/groot-cli/internal/term"
)

// withColors makes lipgloss emit real escape codes so a highlight is visible
// in View output; tests normally run with colors stripped.
func withColors(t *testing.T) {
	t.Helper()
	old := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.TrueColor)
	t.Cleanup(func() { lipgloss.SetColorProfile(old) })
}

func TestChatSelectionIsVisibleWhileDraggingAndAfterCopy(t *testing.T) {
	withColors(t)
	f := newMouseFixture(t, nil)
	f.m.lines = []string{"hello world"}
	f.m.refresh()
	y := f.rowOf("hello world")
	x := f.m.sidebarCols()
	f.press(x, y)
	f.drag(x+4, y)
	want := styleSelection.Render("hello")
	if !strings.Contains(f.m.View(), want) {
		t.Fatalf("selection not drawn while dragging:\n%q", stripCells(f.m.View()))
	}
	f.release(x+4, y)
	if !strings.Contains(f.m.View(), want) {
		t.Fatal("selection must stay visible after the copy")
	}
	f.send(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("x")})
	if strings.Contains(f.m.View(), want) {
		t.Fatal("a key must clear the drawn selection")
	}
}

func TestTerminalSelectionIsVisible(t *testing.T) {
	withColors(t)
	shell := term.NewFake("user@mac default % ls", "README.md")
	f := newMouseFixture(t, shell)
	f.send(tea.KeyMsg{Type: tea.KeyCtrlT})
	x := f.m.chatWidth() + f.m.sidebarCols() + 1
	f.press(x, 1)
	f.drag(x+3, 1)
	if !strings.Contains(f.m.View(), styleSelection.Render("user")) {
		t.Fatalf("terminal selection not drawn:\n%q", stripCells(f.m.View()))
	}
}

func TestHighlightRowsSpansSeveralRows(t *testing.T) {
	withColors(t)
	rows := []string{"abc", "defg", "hij"}
	got := highlightRows(rows, rows, sel(0, 1, 2, 0))
	want := []string{
		"a" + styleSelection.Render("bc"),
		styleSelection.Render("defg"),
		styleSelection.Render("h") + "ij",
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("row %d = %q, want %q", i, got[i], want[i])
		}
	}
}
