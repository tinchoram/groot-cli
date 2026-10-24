package ui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func TestCtrlDClosesWithModelSummariesAndShowsTheStatus(t *testing.T) {
	m, ws := newTestModel(t, &fakeSession{model: "m"})
	m, cmd := press(m, tea.KeyCtrlD)
	if !m.closing || cmd == nil {
		t.Fatal("ctrl-d starts the graceful close")
	}
	if !strings.Contains(stripANSI(m.View()), "closing… (esc to skip summaries)") {
		t.Fatalf("the status line should say the session is closing:\n%s", stripANSI(m.View()))
	}
	msg, ok := cmd().(closedMsg)
	if !ok || msg.err != nil {
		t.Fatalf("the close finishes with closedMsg, got %#v", msg)
	}
	if !ws.closed || ws.fastClosed {
		t.Fatal("ctrl-d ends the conversations with the model summaries, not the fast path")
	}
}

func TestEscDuringClosingSkipsTheSummaries(t *testing.T) {
	m, ws := newTestModel(t, &fakeSession{model: "m"})
	ws.blockClose = true
	m, cmd := press(m, tea.KeyCtrlD)
	done := make(chan tea.Msg, 1)
	go func() { done <- cmd() }()

	m, next := press(m, tea.KeyEsc)
	if next != nil || !m.closing {
		t.Fatal("esc while closing only skips the summaries")
	}
	select {
	case msg := <-done:
		if _, ok := msg.(closedMsg); !ok {
			t.Fatalf("expected closedMsg, got %#v", msg)
		}
	}
	if !ws.closed {
		t.Fatal("the skipped close still ends the conversations")
	}
}

func TestExitCommandCloseGracefully(t *testing.T) {
	m, _ := newTestModel(t, &fakeSession{model: "m"})
	m = sendLine(t, m, "/exit")
	if !m.closing {
		t.Fatal("/exit closes with model summaries, like ctrl-d")
	}
}
