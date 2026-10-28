package ui

import (
	tea "github.com/charmbracelet/bubbletea"

	"github.com/tinchoram/groot-cli/internal/gate"
)

// Bridge carries events from the agent goroutines into the Bubble Tea program.
// Every event is tagged with the key of its conversation ("space/agent"), so
// the UI routes it to that conversation even when another one is on screen.
// The workspace calls these methods; Run forwards Events() to program.Send.
// Confirm blocks the agent until the user answers in the UI.
type Bridge struct {
	events chan tea.Msg
}

// NewBridge returns a Bridge with a buffered event queue.
func NewBridge() *Bridge {
	return &Bridge{events: make(chan tea.Msg, 256)}
}

// Events exposes the queue for the program pump.
func (b *Bridge) Events() <-chan tea.Msg { return b.events }

// Text forwards a streamed text delta.
func (b *Bridge) Text(key, delta string) {
	if delta != "" {
		b.events <- streamMsg{key: key, delta: delta}
	}
}

// ToolCall announces a tool call with its preview.
func (b *Bridge) ToolCall(key, name, preview string) {
	b.events <- toolCallMsg{key: key, name: name, preview: preview}
}

// Inbox reports that a message was delivered: the badges are re-read.
func (b *Bridge) Inbox() {
	b.events <- badgeMsg{}
}

// Jobs reports that the background jobs of a conversation changed: the badges are re-read.
func (b *Bridge) Jobs(string) {
	b.events <- badgeMsg{}
}

// Notice shows a plain line in the transcript of a conversation.
func (b *Bridge) Notice(key, text string) {
	b.events <- warnMsg{key: key, text: text}
}

// Auto reports a call that work mode approved without asking.
func (b *Bridge) Auto(key, name, preview string) {
	b.events <- autoMsg{key: key, name: name, preview: preview}
}

// Done ends a turn: its answer joins the event stream after its deltas.
func (b *Bridge) Done(key, text string, err error) {
	b.events <- answerMsg{key: key, text: text, err: err}
}

// Warn shows a non-fatal warning.
func (b *Bridge) Warn(key, text string) {
	b.events <- warnMsg{key: key, text: text}
}

// Confirm asks the user to allow req in conversation key and blocks until they
// answer. Nothing is approved on the user's behalf.
func (b *Bridge) Confirm(key string, req gate.Request) bool {
	reply := make(chan bool, 1)
	b.events <- confirmMsg{key: key, req: req, reply: reply}
	return <-reply
}
