package workspace

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"slices"
	"strings"

	"github.com/tinchoram/groot-cli/internal/inbox"
	"github.com/tinchoram/groot-cli/internal/tool"
)

// handSender is the sender recorded for messages the user writes with /inbox send.
const handSender = "user"

// errUnknownRecipient is returned when an address does not name an agent
// assigned to a known space.
var errUnknownRecipient = errors.New("unknown recipient; use <space>/<agent>")

// messenger is the tool.Messenger of one running conversation. It knows the
// sender's space and agent, so addresses in "to" resolve relative to it.
type messenger struct {
	w     *Workspace
	space string
	agent string
}

// messengerFor returns the messenger of the agent running in space.
func (w *Workspace) messengerFor(space, agent string) tool.Messenger {
	return &messenger{w: w, space: space, agent: agent}
}

// Send delivers a message and returns the recipient as "<space>/<agent>".
func (m *messenger) Send(ctx context.Context, to, subject, body string) (string, int64, error) {
	space, agent, err := m.w.resolveRecipient(m.space, to)
	if err != nil {
		return "", 0, err
	}
	id, err := m.w.inbox.Send(ctx, inbox.Message{
		FromSpace: m.space, FromAgent: m.agent,
		ToSpace: space, ToAgent: agent,
		Subject: subject, Body: body,
	})
	if err != nil {
		return "", 0, err
	}
	if h := m.w.opts.Hooks.OnInbox; h != nil {
		h()
	}
	m.w.onTaskMessage(ctx, m.space, m.agent, space, agent, subject, body)
	m.w.afterDelivery(space+"/"+agent, deliveredAt(id, m.space, m.agent, space, agent, subject, body))
	return space + "/" + agent, id, nil
}

// Unread returns the unread messages of the sender's agent.
func (m *messenger) Unread(ctx context.Context) ([]inbox.Message, error) {
	return m.w.inbox.Unread(ctx, m.space, m.agent)
}

// MarkRead marks messages as read.
func (m *messenger) MarkRead(ctx context.Context, ids []int64) error {
	return m.w.inbox.MarkRead(ctx, ids)
}

// resolveRecipient maps an address to a space and agent. "groot" is the groot
// agent in the root space; "debug" is the debug agent in whichever space it is
// assigned to; a bare name is an agent in senderSpace; "<space>/<agent>" is
// explicit. The agent must be assigned to that space.
func (w *Workspace) resolveRecipient(senderSpace, to string) (string, string, error) {
	to = strings.TrimSpace(to)
	var spaceName, agent string
	switch {
	case to == managedAgent:
		spaceName, agent = rootSpaceName, managedAgent
	case to == debugAgent:
		spaceName, agent = w.debugSpace(), debugAgent
	case strings.Contains(to, "/"):
		i := strings.Index(to, "/")
		spaceName, agent = to[:i], to[i+1:]
	default:
		spaceName, agent = senderSpace, to
	}
	if spaceName == "" || agent == "" {
		return "", "", errUnknownRecipient
	}
	sp, ok := w.opts.Spaces.Get(spaceName)
	if !ok || !slices.Contains(sp.Agents, agent) {
		return "", "", errUnknownRecipient
	}
	return spaceName, agent, nil
}

// UnreadCounts returns the unread count per "<space>/<agent>".
func (w *Workspace) UnreadCounts(ctx context.Context) (map[string]int, error) {
	return w.inbox.UnreadCounts(ctx)
}

// Inbox returns the newest messages of the active agent, read or not.
func (w *Workspace) Inbox(ctx context.Context, limit int) ([]inbox.Message, error) {
	cur, agent, err := w.activeAgent()
	if err != nil {
		return nil, err
	}
	return w.inbox.List(ctx, cur, agent, limit)
}

// SendByHand delivers a message the user wrote, as sent from the active space.
// Addressing follows the same rules as send_message.
func (w *Workspace) SendByHand(ctx context.Context, to, subject, body string) (string, error) {
	cur, ok := w.opts.Spaces.Current()
	if !ok {
		return "", errors.New("no space selected: use /space use <name>")
	}
	recipient, _, err := w.messengerFor(cur.Name, handSender).Send(ctx, to, subject, body)
	return recipient, err
}

// activeAgent returns the active space and selected agent.
func (w *Workspace) activeAgent() (string, string, error) {
	cur, ok := w.opts.Spaces.Current()
	if !ok {
		return "", "", errors.New("no space selected: use /space use <name>")
	}
	w.mu.Lock()
	agent := w.active
	w.mu.Unlock()
	if agent == "" {
		return "", "", errors.New("no agent in this space: assign one with /space assign <agent>")
	}
	return cur.Name, agent, nil
}

// openInbox opens the inbox database under the home directory.
func openInbox(home string) (*inbox.Store, error) {
	st, err := inbox.Open(filepath.Join(home, "inbox.db"))
	if err != nil {
		return nil, fmt.Errorf("open inbox: %w", err)
	}
	return st, nil
}
