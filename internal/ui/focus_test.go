package ui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"

	"github.com/tinchoram/groot-cli/internal/term"
)

// sidebarHighlightEscape is the background escape of styleSelectedRow in a
// true-colour terminal (#166534).
const sidebarHighlightEscape = "48;2;22;101;52"

func TestTabCycleVisitsBothSectionsWithoutTerminal(t *testing.T) {
	m, _ := newTestModel(t, &fakeSession{model: "m"})
	steps := []struct {
		key  tea.KeyType
		want string
	}{
		{tea.KeyTab, "spaces"},
		{tea.KeyTab, "agents"},
		{tea.KeyTab, "input"},
		{tea.KeyTab, "spaces"},
	}
	for _, s := range steps {
		m, _ = press(m, s.key)
		if got := paneName(m); got != s.want {
			t.Fatalf("after %v focus = %s, want %s", s.key, got, s.want)
		}
	}
}

func TestTabCycleIncludesTerminalWhenShown(t *testing.T) {
	m, _ := termModel(t, term.NewFake())
	m, _ = press(m, tea.KeyCtrlT) // terminal
	// Order: spaces, agents, input, terminal. Shift+tab walks it backwards
	// from the terminal, because tab inside the terminal reaches the shell.
	want := []string{"input", "agents", "spaces", "terminal"}
	for _, w := range want {
		m, _ = press(m, tea.KeyShiftTab)
		if got := paneName(m); got != w {
			t.Fatalf("shift+tab focus = %s, want %s", got, w)
		}
	}
}

func TestTabForwardReachesTerminalFromInput(t *testing.T) {
	m, _ := termModel(t, term.NewFake())
	m, _ = press(m, tea.KeyCtrlT)
	m, _ = press(m, tea.KeyShiftTab) // input
	m, _ = press(m, tea.KeyTab)
	if got := paneName(m); got != "terminal" {
		t.Fatalf("tab from input should reach the terminal, got %s", got)
	}
}

func TestEscFromEitherSectionReturnsToInput(t *testing.T) {
	m, _ := newTestModel(t, &fakeSession{model: "m"})
	for _, key := range []tea.KeyType{tea.KeyTab, tea.KeyTab} { // spaces, agents
		m, _ = press(m, key)
		if !m.focusSidebar {
			t.Fatalf("expected a sidebar section to have focus after %v", key)
		}
		m, _ = press(m, tea.KeyEsc)
		if paneName(m) != "input" {
			t.Fatalf("esc should return to the input, got %s", paneName(m))
		}
		m, _ = press(m, tea.KeyTab) // back into the sidebar for the next pass
	}
}

func TestFocusedSectionCursorRowHasBackground(t *testing.T) {
	old := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.TrueColor)
	t.Cleanup(func() { lipgloss.SetColorProfile(old) })

	m, _ := newTestModel(t, &fakeSession{model: "m"})
	m, _ = press(m, tea.KeyTab) // spaces section focused
	m, _ = press(m, tea.KeyTab) // agents section focused
	m, _ = press(m, tea.KeyEsc) // back to input: nothing highlighted
	if strings.Contains(m.View(), sidebarHighlightEscape) {
		t.Fatal("no sidebar section has focus, so no row may be highlighted")
	}
	m, _ = press(m, tea.KeyTab) // spaces section focused again
	m, _ = press(m, tea.KeyTab) // agents section focused again
	lines := strings.Split(m.View(), "\n")
	var highlighted []string
	for _, l := range lines {
		if strings.Contains(l, sidebarHighlightEscape) {
			highlighted = append(highlighted, stripANSI(l))
		}
	}
	if len(highlighted) != 1 {
		t.Fatalf("want exactly one highlighted row, got %d: %v", len(highlighted), highlighted)
	}
	if !strings.Contains(highlighted[0], "devjunior") {
		t.Fatalf("the highlighted row should be the agents cursor row, got %q", highlighted[0])
	}
}

func TestOnlyFocusedPanelHasGreenBorder(t *testing.T) {
	m, _ := termModel(t, term.NewFake())
	m, _ = press(m, tea.KeyCtrlT)
	// Walk every stop: spaces, agents, input, terminal, then the same again.
	for range 2 {
		for range 4 {
			m, _ = press(m, tea.KeyShiftTab)
			green := 0
			for _, p := range []pane{paneSidebar, paneInput, paneTerminal} {
				if m.borderColor(p) == lipgloss.Color(colorGreen) {
					green++
				}
			}
			if green != 1 {
				t.Fatalf("focus %s: %d panels have a green border, want 1", paneName(m), green)
			}
		}
	}
}
