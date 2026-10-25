package ui

import (
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

// updateNoticeFor is how long the note about a newer groot stays in the status line.
const updateNoticeFor = 10 * time.Second

// updateNoticeMsg carries the version of a newer groot, without the v.
type updateNoticeMsg struct{ version string }

// updateNoticeText is the dim note shown in the status line.
func updateNoticeText(version string) string {
	return "v" + version + " available · run: groot upgrade"
}

// updateNoticeCmd waits, once, for the release check to report a newer version.
// It is a blocking command, which the Bubble Tea runtime runs off the event
// loop, so the UI never waits for the network. It returns nil when there is no
// channel, and the closed channel sends nothing.
func (m Model) updateNoticeCmd() tea.Cmd {
	ch := m.updates
	if ch == nil {
		return nil
	}
	return func() tea.Msg {
		version, ok := <-ch
		if !ok {
			return nil
		}
		return updateNoticeMsg{version: version}
	}
}

// clearNoteAfterFor removes the status line note once d has passed.
func clearNoteAfterFor(d time.Duration) tea.Cmd {
	return tea.Tick(d, func(time.Time) tea.Msg { return clearNoteMsg{} })
}
