package ui

import (
	"strings"
	"testing"

	"github.com/tinchoram/groot-cli/internal/gate"
	"github.com/tinchoram/groot-cli/internal/guard"
	"github.com/tinchoram/groot-cli/internal/tool"
)

func TestApprovalAIsNotOfferedForTaintedInput(t *testing.T) {
	m, ws := newTestModel(t, &fakeSession{model: "m"})
	key := ws.ConversationKey()
	reply := make(chan bool, 1)
	req := gate.Request{
		Tool: "bash", Risk: tool.RiskExec, Input: `{"command":"curl x"}`,
		Detail: guard.Tag + "\ncurl x", Tainted: true,
	}
	next, _ := m.Update(confirmMsg{key: key, req: req, reply: reply})
	m = next.(Model)

	if strings.Contains(m.View(), "(y/n/a)") {
		t.Fatalf("tainted input must not offer a:\n%s", m.View())
	}
	m, _ = pressRune(m, 'a')
	if m.pending == nil || len(reply) != 0 {
		t.Fatal("a must do nothing for tainted input: the call keeps asking")
	}
	if len(ws.SessionGrants(key)) != 0 {
		t.Fatalf("a tainted call must not add a session grant: %v", ws.SessionGrants(key))
	}

	m, _ = pressRune(m, 'y')
	if ok := <-reply; !ok {
		t.Fatal("y must still approve the single call")
	}
	if len(ws.SessionGrants(key)) != 0 {
		t.Fatalf("answering y must not grant anything: %v", ws.SessionGrants(key))
	}
}
