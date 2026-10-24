package workspace

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/tinchoram/groot-cli/internal/inbox"
)

// errAgentBusy is returned when a turn is sent to a conversation that is working.
var errAgentBusy = errors.New("agent is working; wait or switch")

const (
	// inboxInjectMax is how many unread messages one turn receives.
	inboxInjectMax = 5
	// inboxInjectBytes caps the text of the messages one turn receives.
	inboxInjectBytes = 8 * 1024
)

// inboxBlock is one delivered message as the model reads it.
func inboxBlock(m inbox.Message) string {
	subject := m.Subject
	if strings.TrimSpace(subject) == "" {
		subject = "(none)"
	}
	return fmt.Sprintf("[inbox] from %s/%s · %s\nSubject: %s\n%s",
		m.FromSpace, m.FromAgent, m.CreatedAt.Local().Format("2006-01-02 15:04"), subject, m.Body)
}

// injectInbox puts the unread messages of the conversation in front of the
// prompt, oldest first, and marks the messages it delivers as read. A turn gets
// at most inboxInjectMax messages and inboxInjectBytes of text; the rest stays
// unread and the turn is told so.
func (c *Conversation) injectInbox(ctx context.Context, prompt string) string {
	if c.inbox == nil {
		return prompt
	}
	unread, err := c.inbox.Unread(ctx, c.space, c.name)
	if err != nil || len(unread) == 0 {
		return prompt
	}
	var blocks []string
	var ids []int64
	used := 0
	for _, m := range unread {
		if len(blocks) == inboxInjectMax {
			break
		}
		block := inboxBlock(m)
		if used+len(block) > inboxInjectBytes {
			if len(blocks) > 0 {
				break
			}
			block = block[:inboxInjectBytes] + "…"
		}
		blocks = append(blocks, block)
		ids = append(ids, m.ID)
		used += len(block)
	}
	if err := c.inbox.MarkRead(ctx, ids); err != nil {
		c.warn(fmt.Sprintf("inbox: messages not marked read: %v", err))
	}
	parts := append([]string(nil), blocks...)
	if rest := len(unread) - len(blocks); rest > 0 {
		parts = append(parts, fmt.Sprintf("[inbox] %d more unread messages stay in the inbox; read them with read_inbox when they matter.", rest))
	}
	if prompt != "" {
		parts = append(parts, prompt)
	}
	return strings.Join(parts, "\n\n")
}

// warn reports a problem of the conversation through its agent's warning hook.
func (c *Conversation) warn(msg string) {
	if c.Agent.OnWarning != nil {
		c.Agent.OnWarning(msg)
	}
}

// afterDelivery runs when a message reaches the conversation key. In work mode,
// and when the conversation is idle, it starts a turn that reads the inbox. A
// turn triggered this way counts against work.max_auto_turns for the session.
func (w *Workspace) afterDelivery(key string, msg inbox.Message) {
	w.mu.Lock()
	conv := w.convs[key]
	w.mu.Unlock()
	if conv == nil || !conv.WorkMode() {
		return
	}
	limit := int64(w.opts.Config.Work.MaxAutoTurns)
	if conv.autoTurns.Load() >= limit {
		w.warnKey(key, fmt.Sprintf("work mode: the automatic turn limit (%d per session) was reached; the message from %s/%s waits in the inbox", limit, msg.FromSpace, msg.FromAgent))
		return
	}
	if !conv.busy.CompareAndSwap(false, true) {
		return // a turn is running; it will read the inbox when it starts
	}
	conv.autoTurns.Add(1)
	w.notifyKey(key, fmt.Sprintf("⟶ triggered by message #%d from %s/%s", msg.ID, msg.FromSpace, msg.FromAgent))
	go func() {
		defer conv.busy.Store(false)
		text, err := conv.send(context.Background(), "")
		if h := w.opts.Hooks.OnAnswer; h != nil {
			h(key, text, err)
		}
	}()
}

// notifyKey writes a plain line to the transcript of a conversation.
func (w *Workspace) notifyKey(key, text string) {
	if h := w.opts.Hooks.OnNotice; h != nil {
		h(key, text)
		return
	}
	w.warnKey(key, text)
}

// deliveredAt is the message as it arrived, for the trigger.
func deliveredAt(id int64, fromSpace, fromAgent, toSpace, toAgent, subject, body string) inbox.Message {
	return inbox.Message{
		ID: id, FromSpace: fromSpace, FromAgent: fromAgent, ToSpace: toSpace, ToAgent: toAgent,
		Subject: subject, Body: body, CreatedAt: time.Now(),
	}
}
