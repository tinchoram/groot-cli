package ui

import (
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

// pollEvery is how often the inbox badges are re-read while a conversation works.
const pollEvery = 2 * time.Second

// startTicks starts the spinner tick chain when something works and the chain
// is not running yet. The chain drives the spinner and the badge polling.
func (m *Model) startTicks() tea.Cmd {
	if m.ticking || !m.anyBusy() {
		return nil
	}
	m.ticking = true
	return m.spin.Tick
}

// poll re-reads the unread counts when pollEvery has passed since the last read.
func (m *Model) poll() {
	if time.Since(m.lastPoll) < pollEvery {
		return
	}
	m.lastPoll = time.Now()
	m.refreshUnread()
}
