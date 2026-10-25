package ui

import (
	"strings"
	"testing"
)

func TestReadInboxSendsTheFixedPromptOnce(t *testing.T) {
	chat := &fakeSession{model: "m"}
	m, ws := newTestModel(t, chat)
	ws.unread = map[string]int{"default/devjunior": 2}
	m = sendLine(t, m, "/readinbox")
	if len(chat.sends) != 1 || chat.sends[0] != readInboxPrompt {
		t.Fatalf("sends = %q, want the read-inbox prompt once", chat.sends)
	}
}

func TestReadInboxWithoutUnreadSpendsNoTurn(t *testing.T) {
	chat := &fakeSession{model: "m"}
	m, _ := newTestModel(t, chat)
	m = sendLine(t, m, "/readinbox")
	if len(chat.sends) != 0 {
		t.Fatalf("no unread messages must not start a turn, sends = %q", chat.sends)
	}
	if out := stripANSI(m.transcriptText()); !strings.Contains(out, "no unread messages for devjunior; /inbox lists the last 10") {
		t.Fatalf("dim note missing:\n%s", out)
	}
}

func TestReadInboxIsInCompletion(t *testing.T) {
	m, _ := newTestModel(t, &fakeSession{model: "m"})
	m = typeText(m, "/readi")
	got := completionInserts(m)
	if len(got) != 1 || got[0] != "/readinbox" {
		t.Fatalf("candidates for /readi = %v, want [/readinbox]", got)
	}
}
