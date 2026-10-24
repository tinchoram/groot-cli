package ui

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

// TestEnterOnSpaceMovesFocusToAgents: enter on a SPACES row selects the space
// and puts the cursor on the agents of that space.
func TestEnterOnSpaceMovesFocusToAgents(t *testing.T) {
	m, ws := newTestModel(t, &fakeSession{model: "m"})
	_ = ws.AssignAgent("reviewer")
	m, _ = press(m, tea.KeyTab) // SPACES focused
	m, _ = press(m, tea.KeyEnter)
	if !m.focusSidebar || m.section != sectionAgents {
		t.Fatalf("after enter on a space, focus is on AGENTS: focus=%v section=%v", m.focusSidebar, m.section)
	}
	if m.cursor != m.activeIndex() {
		t.Fatalf("the cursor sits on the active agent: %d", m.cursor)
	}
}

// TestEnterOnAgentMovesFocusToInput: enter on an AGENTS row selects the agent
// and gives the keyboard to the input box.
func TestEnterOnAgentMovesFocusToInput(t *testing.T) {
	m, ws := newTestModel(t, &fakeSession{model: "m"})
	_ = ws.AssignAgent("reviewer")
	m, _ = press(m, tea.KeyTab) // SPACES
	m, _ = press(m, tea.KeyTab) // AGENTS
	m, _ = press(m, tea.KeyDown)
	m, _ = press(m, tea.KeyEnter)
	if ws.agent != "reviewer" {
		t.Fatalf("the agent is selected: %s", ws.agent)
	}
	if m.focusSidebar || m.focusTerm {
		t.Fatal("after enter on an agent, the input has the keyboard")
	}
}
