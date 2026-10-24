package ui

import (
	"testing"
	"time"

	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"
)

func TestBadgesRefreshAfterFeedbackCommand(t *testing.T) {
	m, ws := newTestModel(t, &fakeSession{model: "m"})
	ws.unread = map[string]int{"root/debug": 1}
	m = sendLine(t, m, "/bug the approval hides the path")
	if m.unread["root/debug"] != 1 {
		t.Fatalf("the badge of debug should appear right after /bug: %v", m.unread)
	}
}

func TestBadgeMessageRereadsTheCounts(t *testing.T) {
	m, ws := newTestModel(t, &fakeSession{model: "m"})
	ws.unread = map[string]int{"default/reviewer": 2}
	next, _ := m.Update(badgeMsg{})
	if got := next.(Model).unread["default/reviewer"]; got != 2 {
		t.Fatalf("a delivery re-reads the badges: %d", got)
	}
}

func TestBusyPollingRereadsEveryTwoSeconds(t *testing.T) {
	m, ws := newTestModel(t, &fakeSession{model: "m"})
	m.busy = true
	ws.unread = map[string]int{"root/debug": 3}
	m.lastPoll = time.Now().Add(-3 * time.Second)
	next, _ := m.Update(spinner.TickMsg{})
	if got := next.(Model).unread["root/debug"]; got != 3 {
		t.Fatalf("a busy tick re-reads the badges after two seconds: %d", got)
	}
	m2 := next.(Model)
	ws.unread = map[string]int{"root/debug": 4}
	next, _ = m2.Update(spinner.TickMsg{})
	if got := next.(Model).unread["root/debug"]; got != 3 {
		t.Fatalf("no second read within two seconds: %d", got)
	}
}

func TestSendToolHookSignalsTheUI(t *testing.T) {
	b := NewBridge()
	b.Inbox()
	if _, ok := (<-b.Events()).(badgeMsg); !ok {
		t.Fatal("the delivery hook reaches the UI as a badge refresh")
	}
	var _ tea.Msg = badgeMsg{}
}
