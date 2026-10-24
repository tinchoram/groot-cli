package ui

import (
	"context"
	"strconv"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
)

// run is the state of one conversation that is not on screen. The conversation
// on screen keeps the same fields in the Model itself.
type run struct {
	busy    bool
	pending *confirmState
	partial string
}

// onKey applies fn to the state of conversation key. The conversation on screen
// is changed in place. Any other conversation is swapped into the Model while fn
// runs, so the same handlers serve both, and its state is saved afterwards.
func (m *Model) onKey(key string, fn func()) {
	if key == "" || key == m.key {
		fn()
		return
	}
	bg := m.runs[key]
	if bg == nil {
		bg = &run{}
	}
	savedBusy, savedPending, savedPartial := m.busy, m.pending, m.partial
	savedLines, savedKey := m.lines, m.key
	m.lines, m.key = m.transcripts[key], key
	m.busy, m.pending, m.partial = bg.busy, bg.pending, bg.partial

	fn()

	m.transcripts[key] = m.lines
	m.storeRun(key, &run{busy: m.busy, pending: m.pending, partial: m.partial})
	m.lines, m.key = savedLines, savedKey
	m.busy, m.pending, m.partial = savedBusy, savedPending, savedPartial
	// The viewport was drawn with the background state; draw the screen again.
	m.refresh()
}

// storeRun keeps the state of a conversation that is off screen, or drops it
// when there is nothing left to remember.
func (m *Model) storeRun(key string, r *run) {
	if !r.busy && r.pending == nil && r.partial == "" {
		delete(m.runs, key)
		return
	}
	m.runs[key] = r
}

// park saves the state of the conversation being left.
func (m *Model) park(key string) {
	if key == "" {
		return
	}
	m.storeRun(key, &run{busy: m.busy, pending: m.pending, partial: m.partial})
}

// unpark loads the state of the conversation being entered.
func (m *Model) unpark(key string) {
	bg := m.runs[key]
	delete(m.runs, key)
	m.busy, m.pending, m.partial = false, nil, ""
	if bg != nil {
		m.busy, m.pending, m.partial = bg.busy, bg.pending, bg.partial
	}
}

// keyState reports whether conversation key is working and whether it waits
// for an approval.
func (m Model) keyState(key string) (busy, pending bool) {
	if key == m.key {
		return m.busy, m.pending != nil
	}
	if r := m.runs[key]; r != nil {
		return r.busy, r.pending != nil
	}
	return false, false
}

// busyKeys lists every conversation that is working, the one on screen included.
func (m Model) busyKeys() []string {
	var out []string
	if m.busy {
		out = append(out, m.key)
	}
	for k, r := range m.runs {
		if r.busy && k != m.key {
			out = append(out, k)
		}
	}
	return out
}

// othersBusy counts the working conversations other than the one on screen.
func (m Model) othersBusy() int {
	n := 0
	for k, r := range m.runs {
		if k != m.key && r.busy {
			n++
		}
	}
	return n
}

// stateBadge is the mark after an agent's name: a tan ? while it waits for an
// approval, or the spinner frame while it works.
func (m Model) stateBadge(space, agent string) string {
	busy, pending := m.keyState(space + "/" + agent)
	switch {
	case pending:
		return " " + styleTool.Render("?")
	case busy:
		return " " + styleAccentBold.Render(m.spin.View())
	}
	return ""
}

// spaceBadge marks a space whose agents work or wait: a green dot with the
// number of working agents when there are several, and a tan ? for approvals.
func (m Model) spaceBadge(space string) string {
	prefix := space + "/"
	working, waiting := 0, false
	for _, k := range m.busyKeys() {
		if strings.HasPrefix(k, prefix) {
			working++
		}
	}
	for _, k := range m.waitingKeys() {
		if strings.HasPrefix(k, prefix) {
			waiting = true
		}
	}
	var b strings.Builder
	if working > 0 {
		b.WriteString(" " + styleAccentBold.Render("●"))
		if working > 1 {
			b.WriteString(" " + styleAccentBold.Render(strconv.Itoa(working)))
		}
	}
	if waiting {
		b.WriteString(" " + styleTool.Render("?"))
	}
	unread := 0
	for k, n := range m.unread {
		if strings.HasPrefix(k, prefix) {
			unread += n
		}
	}
	if unread > 0 {
		b.WriteString(" " + styleInboxBadge.Render(inboxMark+" "+strconv.Itoa(unread)))
	}
	return b.String()
}

// waitingKeys lists the conversations that wait for an approval.
func (m Model) waitingKeys() []string {
	var out []string
	if m.pending != nil {
		out = append(out, m.key)
	}
	for k, r := range m.runs {
		if r.pending != nil && k != m.key {
			out = append(out, k)
		}
	}
	return out
}

// anyBusy reports whether any conversation works, so that the spinner keeps moving.
func (m Model) anyBusy() bool {
	return m.busy || len(m.busyKeys()) > 0
}

// finishTurn releases the cancel function of a finished turn and, when the
// conversation is the one on screen, re-reads the workspace.
func (m *Model) finishTurn(key string) {
	if cancel, ok := m.cancels[key]; ok {
		cancel()
		delete(m.cancels, key)
	}
	if key == "" || key == m.ws.ConversationKey() {
		m.syncConversation()
		m.refreshUnread()
		m.refresh()
	}
}

// denyAll ends every turn at shutdown: pending approvals are denied, and the
// turns are cancelled.
func (m *Model) denyAll() {
	if m.pending != nil {
		m.answer(false)
	}
	for k, r := range m.runs {
		if r.pending != nil {
			r.pending.reply <- false
			r.pending = nil
		}
		m.storeRun(k, r)
	}
	for k, cancel := range m.cancels {
		cancel()
		delete(m.cancels, k)
	}
}

// msgKey is the conversation an event belongs to, or "" for events that are not
// tied to one conversation.
func msgKey(msg tea.Msg) string {
	switch m := msg.(type) {
	case streamMsg:
		return m.key
	case toolCallMsg:
		return m.key
	case autoMsg:
		return m.key
	case warnMsg:
		return m.key
	case confirmMsg:
		return m.key
	case answerMsg:
		return m.key
	}
	return ""
}

// startTurn gives a turn a cancellable context, kept until the turn finishes.
func (m *Model) startTurn(key string) context.Context {
	ctx, cancel := context.WithCancel(context.Background())
	m.cancels[key] = cancel
	return ctx
}
