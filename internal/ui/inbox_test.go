package ui

import (
	"strings"
	"testing"
	"time"

	"github.com/tinchoram/groot-cli/internal/inbox"
)

func TestSidebarShowsUnreadBadgeForActiveSpaceAgents(t *testing.T) {
	chat := &fakeSession{model: "m"}
	m, ws := newTestModel(t, chat)
	ws.unread = map[string]int{"default/devjunior": 2, "other/reviewer": 5}
	m.refreshUnread()
	out := stripANSI(m.View())
	if !strings.Contains(out, "✉ 2") {
		t.Fatalf("badge missing for devjunior:\n%s", out)
	}
	if strings.Contains(out, "✉ 5") {
		t.Fatal("badges belong to the active space only")
	}
}

func TestSidebarHasNoBadgeWithoutUnread(t *testing.T) {
	m, _ := newTestModel(t, &fakeSession{model: "m"})
	m.refreshUnread()
	if strings.Contains(stripANSI(m.View()), "✉") {
		t.Fatal("no unread messages, no badge")
	}
}

func TestInboxCommandListsLastMessagesWithMarkdownBody(t *testing.T) {
	m, ws := newTestModel(t, &fakeSession{model: "m"})
	ws.inbox = []inbox.Message{{
		ID: 3, FromSpace: "root", FromAgent: "groot", Subject: "new space",
		Body: "Please **create** `payments`", CreatedAt: time.Date(2026, 10, 24, 14, 0, 0, 0, time.UTC),
	}}
	m = sendLine(t, m, "/inbox")
	out := m.transcriptText()
	for _, want := range []string{"inbox · last 1 messages", "#3 · from root/groot", "unread", "Subject: new space", "create"} {
		if !strings.Contains(out, want) {
			t.Fatalf("/inbox output misses %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "**create**") {
		t.Fatal("the body should be rendered as Markdown, not shown raw")
	}
}

func TestInboxCommandWithoutMessages(t *testing.T) {
	m, _ := newTestModel(t, &fakeSession{model: "m"})
	m = sendLine(t, m, "/inbox")
	if !strings.Contains(m.transcriptText(), "inbox is empty") {
		t.Fatal("empty inbox should say so")
	}
}

func TestInboxSendByHandDeliversAndSplitsSubject(t *testing.T) {
	m, ws := newTestModel(t, &fakeSession{model: "m"})
	m = sendLine(t, m, "/inbox send reviewer Review diff -- check the migration")
	if len(ws.handSent) != 1 || ws.handSent[0] != "reviewer|Review diff|check the migration" {
		t.Fatalf("hand send = %v", ws.handSent)
	}
	if !strings.Contains(m.transcriptText(), "delivered to default/reviewer") {
		t.Fatalf("confirmation missing:\n%s", m.transcriptText())
	}
}

func TestInboxSendRejectsMissingBody(t *testing.T) {
	m, ws := newTestModel(t, &fakeSession{model: "m"})
	m = sendLine(t, m, "/inbox send reviewer subject")
	if len(ws.handSent) != 0 {
		t.Fatal("a message without the -- separator must not be sent")
	}
	if !strings.Contains(m.transcriptText(), "usage: /inbox send") {
		t.Fatal("usage hint missing")
	}
}
