package ui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"
)

// The two bands of the sidebar in a true-colour terminal.
const (
	focusedBandEscape = "48;2;22;101;52" // #166534
	idleBandEscape    = "48;2;20;83;44"  // #14532D, as the terminal profile rounds it
)

func TestUnfocusedSectionKeepsTheDarkBandOnItsActiveRow(t *testing.T) {
	old := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.TrueColor)
	t.Cleanup(func() { lipgloss.SetColorProfile(old) })

	m, _ := newTestModel(t, &fakeSession{model: "m"})
	_ = m.ws.AssignAgent("reviewer")
	m = update(t, m, tea.KeyMsg{Type: tea.KeyTab}) // spaces section focused

	var focused, idle []string
	for _, line := range strings.Split(m.View(), "\n") {
		switch {
		case strings.Contains(line, focusedBandEscape):
			focused = append(focused, stripANSI(line))
		case strings.Contains(line, idleBandEscape):
			idle = append(idle, stripANSI(line))
		}
	}
	if len(focused) != 1 || !strings.Contains(focused[0], "default") {
		t.Fatalf("the focused section's cursor row is the lighter band: %q", focused)
	}
	if len(idle) != 1 || !strings.Contains(idle[0], "devjunior") {
		t.Fatalf("the unfocused agents section keeps the dark band on its active row: %q", idle)
	}
}
