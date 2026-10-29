package workspace

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/tinchoram/groot-cli/internal/guard"
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

// inboxBlock is one delivered message as the model reads it. The body is
// enveloped with its sender as the source. The subject stays plain, because
// the delegation contract reads it, and the guard's summary follows a body
// with findings.
func inboxBlock(m inbox.Message, g *guard.Guard) string {
	subject := m.Subject
	if strings.TrimSpace(subject) == "" {
		subject = "(none)"
	}
	body := g.Wrap("message from "+m.FromSpace+"/"+m.FromAgent, m.Body)
	block := fmt.Sprintf("[inbox] from %s/%s · %s\nSubject: %s\n%s",
		m.FromSpace, m.FromAgent, m.CreatedAt.Local().Format("2006-01-02 15:04"), subject, body)
	if g.Enabled() {
		if fs := guard.Scan(m.Body); len(fs) > 0 {
			block += "\n" + guard.Summary(fs)
		}
	}
	return block
}

// plainLen is the size of a message as the turn cap counts it: the notice
// without the envelope and without the guard summary.
func plainLen(m inbox.Message) int {
	return len(inboxBlock(m, nil))
}

// fitBody cuts the body of a message that alone exceeds limit, so the first
// message still reaches the model, with its envelope closed.
func fitBody(m inbox.Message, limit int) inbox.Message {
	empty := m
	empty.Body = ""
	room := limit - plainLen(empty) - len("…")
	if room < 0 {
		room = 0
	}
	if len(m.Body) <= room {
		return m
	}
	cut := room
	for cut > 0 && !utf8.RuneStart(m.Body[cut]) {
		cut--
	}
	m.Body = m.Body[:cut] + "…"
	return m
}

// inboxParts returns the notices of the unread messages of the conversation,
// oldest first, and marks the messages it delivers as read. A turn gets at most
// inboxInjectMax messages and inboxInjectBytes of text; the rest stays unread
// and the turn is told so. The caller places the notices before the prompt.
func (c *Conversation) inboxParts(ctx context.Context) []string {
	if c.inbox == nil {
		return nil
	}
	unread, err := c.inbox.Unread(ctx, c.space, c.name)
	if err != nil || len(unread) == 0 {
		return nil
	}
	var blocks []string
	var ids []int64
	used := 0
	for _, m := range unread {
		if len(blocks) == inboxInjectMax {
			break
		}
		// The cap counts the message text without the guard's envelope; the
		// envelope is bounded overhead on top of it.
		orig := m
		if used+plainLen(m) > inboxInjectBytes {
			if len(blocks) > 0 {
				break
			}
			m = fitBody(m, inboxInjectBytes)
		}
		blocks = append(blocks, inboxBlock(m, c.Agent.Guard))
		ids = append(ids, m.ID)
		c.expectHandover(orig)
		used += plainLen(m)
	}
	if err := c.inbox.MarkRead(ctx, ids); err != nil {
		c.warn(fmt.Sprintf("inbox: messages not marked read: %v", err))
	}
	if c.asks != nil {
		c.askRead = append(c.askRead, c.asks.take(ids)...)
	}
	parts := append([]string(nil), blocks...)
	if rest := len(unread) - len(blocks); rest > 0 {
		parts = append(parts, fmt.Sprintf("[inbox] %d more unread messages stay in the inbox; read them with read_inbox when they matter.", rest))
	}
	return parts
}

// warn reports a problem of the conversation through its agent's warning hook.
func (c *Conversation) warn(msg string) {
	if c.Agent.OnWarning != nil {
		c.Agent.OnWarning(msg)
	}
}

// capNotice says how the limit of automatic turns is reported when it blocks a turn.
type capNotice int

const (
	capSilent capNotice = iota // the blocked turn is not reported
	capEach                    // every blocked turn is reported
	capOnce                    // the first blocked turn of the session is reported
)

// afterDelivery runs when a message reaches the agent of a space. An agent with
// no open conversation is opened first, so the message can start a turn. An idle
// conversation then starts a turn that reads the inbox, in work mode or not. The
// turn counts against work.max_auto_turns for the session. A busy one reads the
// inbox when its running turn ends (see releaseTurn).
func (w *Workspace) afterDelivery(space, agent string, msg inbox.Message) {
	key := space + "/" + agent
	w.mu.Lock()
	conv, err := w.convLocked(space, agent)
	w.mu.Unlock()
	if err != nil {
		w.warnKey(key, fmt.Sprintf("message from %s/%s waits in the inbox: %v", msg.FromSpace, msg.FromAgent, err))
		return
	}
	w.startMessageTurn(key, conv, msg)
}

// convLocked returns the conversation of an agent of a space, opening it when it
// is not open yet. The caller holds mu.
func (w *Workspace) convLocked(space, agent string) (*Conversation, error) {
	key := space + "/" + agent
	if c, ok := w.convs[key]; ok {
		return c, nil
	}
	sp, ok := w.opts.Spaces.Get(space)
	if !ok {
		return nil, fmt.Errorf("no space named %q", space)
	}
	if err := w.openLocked(sp, agent); err != nil {
		return nil, err
	}
	return w.convs[key], nil
}

// startMessageTurn starts a turn that reads the inbox, triggered by msg.
func (w *Workspace) startMessageTurn(key string, conv *Conversation, msg inbox.Message) {
	w.startAutoTurn(key, conv,
		fmt.Sprintf("⟶ triggered by message #%d from %s/%s", msg.ID, msg.FromSpace, msg.FromAgent),
		fmt.Sprintf("automatic turns: the limit (%d per session) was reached; the message from %s/%s waits in the inbox (/readinbox)", w.opts.Config.Work.MaxAutoTurns, msg.FromSpace, msg.FromAgent),
		capOnce)
}

// followUpUnread runs when a turn ends. Messages that arrived while the turn ran
// and still wait unread get one follow-up turn, so a reply that lands mid-turn
// is not stuck. It starts nothing when the inbox is empty.
func (w *Workspace) followUpUnread(key string, conv *Conversation) {
	if conv.inbox == nil {
		return
	}
	unread, err := conv.inbox.Unread(context.Background(), conv.space, conv.name)
	if err != nil || len(unread) == 0 {
		return
	}
	w.startMessageTurn(key, conv, unread[0])
}

// startAutoTurn starts a turn of an idle conversation. The turn counts against
// work.max_auto_turns for the session. At the limit it starts nothing and reports
// capped according to warn. A running turn is left alone: it reads what is
// waiting when it starts, and releaseTurn starts a follow-up for the rest.
func (w *Workspace) startAutoTurn(key string, conv *Conversation, notice, capped string, warn capNotice) {
	if conv.autoTurns.Load() >= int64(w.opts.Config.Work.MaxAutoTurns) {
		switch warn {
		case capEach:
			w.warnKey(key, capped)
		case capOnce:
			if conv.capWarned.CompareAndSwap(false, true) {
				w.warnKey(key, capped)
			}
		}
		return
	}
	if !conv.busy.CompareAndSwap(false, true) {
		return
	}
	conv.autoTurns.Add(1)
	w.notifyKey(key, notice)
	go func() {
		defer conv.releaseTurn()
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
