package ui

import (
	"strings"
	"testing"

	"github.com/tinchoram/groot-cli/internal/gate"
	"github.com/tinchoram/groot-cli/internal/tool"
)

func TestDestructiveLineNamesTheKindAndCommand(t *testing.T) {
	req := gate.Request{Destructive: true, DestructiveReason: "git: git push --force"}
	if got := destructiveLine(req); got != "destructive (git): git push --force" {
		t.Fatalf("destructiveLine = %q", got)
	}
	if got := destructiveLine(gate.Request{Tool: "bash"}); got != "" {
		t.Fatalf("an ordinary call must have no destructive line, got %q", got)
	}
}

func TestDestructiveApprovalShowsWarningAndOffersNoA(t *testing.T) {
	m, ws := newTestModel(t, &fakeSession{model: "m"})
	key := ws.ConversationKey()
	reply := make(chan bool, 1)
	req := gate.Request{
		Tool: "bash", Risk: tool.RiskExec, Input: `{"command":"git push --force"}`,
		Detail: "git push --force", Destructive: true, DestructiveReason: "git: git push --force",
	}
	next, _ := m.Update(confirmMsg{key: key, req: req, reply: reply})
	m = next.(Model)

	view := m.View()
	if !strings.Contains(view, "destructive (git): git push --force") {
		t.Fatalf("the approval must show the destructive line:\n%s", view)
	}
	if strings.Contains(view, "(y/n/a)") || strings.Contains(view, "a: yes for this session") {
		t.Fatalf("a destructive call must not offer a:\n%s", view)
	}

	m, _ = pressRune(m, 'a')
	if m.pending == nil || len(reply) != 0 {
		t.Fatal("a must do nothing for a destructive call: the call keeps asking")
	}
	if len(ws.SessionGrants(key)) != 0 {
		t.Fatalf("a destructive call must not add a session grant: %v", ws.SessionGrants(key))
	}

	m, _ = pressRune(m, 'y')
	if ok := <-reply; !ok {
		t.Fatal("y must still approve the single call")
	}
}
