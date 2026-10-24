package ui

import (
	"strings"

	"github.com/tinchoram/groot-cli/internal/gate"
)

// answerForSession approves the pending call and every later call of the same
// tool in this conversation, within the scope work mode gives it. Admin tools
// are not offered this answer, so the key does nothing for them.
func (m *Model) answerForSession() {
	if m.pending == nil || !gate.Grantable(m.pending.req.Tool) {
		return
	}
	name := m.pending.req.Tool
	if err := m.ws.GrantSession(m.pending.key, name); err != nil {
		m.append(styleError.Render("error: " + err.Error()))
		return
	}
	m.pending.reply <- true
	m.append(styleDim.Render("  ↳ approved for this session (" + name + ")"))
	m.pending = nil
	m.resize()
}

// runAllowCommand lists the session approvals of this conversation, or with
// "clear" drops them.
func (m *Model) runAllowCommand(arg string) {
	key := m.ws.ConversationKey()
	switch strings.TrimSpace(arg) {
	case "":
		grants := m.ws.SessionGrants(key)
		if len(grants) == 0 {
			m.append(styleDim.Render("no approvals for this session"))
			return
		}
		m.append(styleDim.Render("approved for this session: " + strings.Join(grants, ", ")))
	case "clear":
		m.ws.ClearSessionGrants(key)
		m.append(styleDim.Render("session approvals cleared"))
	default:
		m.append(styleError.Render("usage: /allow | /allow clear"))
	}
}

// approvalChoices is the answer key list shown with an approval: a is offered
// only for tools that can be granted.
func approvalChoices(req gate.Request) string {
	if gate.Grantable(req.Tool) {
		return "(y/n/a)"
	}
	return "(y/n)"
}
