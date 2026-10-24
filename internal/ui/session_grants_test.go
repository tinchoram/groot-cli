package ui

import (
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/tinchoram/groot-cli/internal/gate"
	"github.com/tinchoram/groot-cli/internal/tool"
)

func (f *fakeWorkspace) GrantSession(key, name string) error {
	if !gate.Grantable(name) {
		return fmt.Errorf("%s cannot be approved for the session", name)
	}
	if f.grants == nil {
		f.grants = map[string][]string{}
	}
	f.grants[key] = append(f.grants[key], name)
	return nil
}

func (f *fakeWorkspace) SessionGrants(key string) []string { return f.grants[key] }

func (f *fakeWorkspace) ClearSessionGrants(key string) { delete(f.grants, key) }

// A confirm for another conversation is parked in its own run, so these tests
// use the active conversation key to see the box in the model.
func TestApprovalAGrantsTheToolForTheSession(t *testing.T) {
	m, ws := newTestModel(t, &fakeSession{model: "m"})
	key := ws.ConversationKey()
	reply := make(chan bool, 1)
	req := gate.Request{Tool: "write_file", Risk: tool.RiskWrite, Input: `{"path":"notes/a.md"}`, Detail: "--- a\n+++ b\n-old\n+new"}
	next, _ := m.Update(confirmMsg{key: key, req: req, reply: reply})
	m = next.(Model)

	m, _ = pressRune(m, 'a')
	select {
	case ok := <-reply:
		if !ok {
			t.Fatal("a must approve the call")
		}
	default:
		t.Fatal("a did not answer the approval")
	}
	if m.pending != nil {
		t.Fatal("pending must clear after a decision")
	}
	if !slices.Equal(ws.SessionGrants(key), []string{"write_file"}) {
		t.Fatalf("grants = %v", ws.SessionGrants(key))
	}
	if !strings.Contains(m.transcriptText(), "approved for this session (write_file)") {
		t.Fatalf("transcript line missing:\n%s", m.transcriptText())
	}
}

func TestApprovalAIsNotOfferedForAdminTools(t *testing.T) {
	m, ws := newTestModel(t, &fakeSession{model: "m"})
	key := ws.ConversationKey()
	reply := make(chan bool, 1)
	req := gate.Request{Tool: "create_space", Risk: tool.RiskWrite, Detail: "name: x"}
	next, _ := m.Update(confirmMsg{key: key, req: req, reply: reply})
	m = next.(Model)
	if m.pending == nil {
		t.Fatal("the admin approval must be pending in the active conversation")
	}
	if strings.Contains(m.View(), "(y/n/a)") || strings.Contains(m.View(), "a: yes for this session") {
		t.Fatalf("admin approvals must not offer a:\n%s", m.View())
	}
	m, _ = pressRune(m, 'a')
	if m.pending == nil || len(reply) != 0 {
		t.Fatal("a must do nothing for an admin tool: the call keeps asking")
	}
	if len(ws.SessionGrants(key)) != 0 {
		t.Fatal("an admin tool must not be granted")
	}
}

func TestAllowCommandListsAndClearsGrants(t *testing.T) {
	m, ws := newTestModel(t, &fakeSession{model: "m"})
	key := ws.ConversationKey()
	_ = ws.GrantSession(key, "bash")
	_ = ws.GrantSession(key, "write_file")

	m.runAllowCommand("")
	if !strings.Contains(m.transcriptText(), "approved for this session: bash, write_file") {
		t.Fatalf("list missing:\n%s", m.transcriptText())
	}
	m.runAllowCommand("clear")
	if len(ws.SessionGrants(key)) != 0 {
		t.Fatal("/allow clear must drop the grants")
	}
	if !strings.Contains(m.transcriptText(), "session approvals cleared") {
		t.Fatalf("clear line missing:\n%s", m.transcriptText())
	}
}
