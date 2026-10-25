package ui

import (
	"strings"
	"testing"
	"time"

	"github.com/tinchoram/groot-cli/internal/inbox"
)

func TestAckIsShownOnceAsANoteInTheOpenConversation(t *testing.T) {
	m, ws := newTestModel(t, &fakeSession{model: "m"})
	ws.acks = []inbox.Message{{
		ID: 9, FromSpace: "default", FromAgent: "debug", Body: "FEAT-016 recorded",
		Kind: inbox.KindAck, CreatedAt: time.Date(2026, 10, 25, 18, 0, 0, 0, time.UTC),
	}}
	m.refreshUnread()
	m.refreshUnread()
	out := stripANSI(m.transcriptText())
	if n := strings.Count(out, "ack from debug: FEAT-016 recorded"); n != 1 {
		t.Fatalf("the ack note appears %d times, want once:\n%s", n, out)
	}
}

func TestAckNoteStaysOneLine(t *testing.T) {
	m, ws := newTestModel(t, &fakeSession{model: "m"})
	ws.acks = []inbox.Message{{FromAgent: "debug", Body: "line one\nline two", Kind: inbox.KindAck}}
	m.refreshUnread()
	if !strings.Contains(stripANSI(m.transcriptText()), "ack from debug: line one line two") {
		t.Fatalf("a multi-line ack must collapse to one note:\n%s", m.transcriptText())
	}
}

func TestInboxListingTagsAcksInHistory(t *testing.T) {
	m, ws := newTestModel(t, &fakeSession{model: "m"})
	ws.inbox = []inbox.Message{
		{ID: 4, FromSpace: "default", FromAgent: "debug", Body: "FEAT-016 recorded", Kind: inbox.KindAck,
			CreatedAt: time.Date(2026, 10, 25, 18, 0, 0, 0, time.UTC), ReadAt: &time.Time{}},
		{ID: 3, FromSpace: "root", FromAgent: "groot", Subject: "new space", Body: "create", Kind: inbox.KindMessage,
			CreatedAt: time.Date(2026, 10, 24, 14, 0, 0, 0, time.UTC)},
	}
	m = sendLine(t, m, "/inbox")
	out := stripANSI(m.transcriptText())
	if !strings.Contains(out, "#4 · from default/debug · 2026-10-25 18:00 · ack · read") {
		t.Fatalf("the ack is not tagged in the listing:\n%s", out)
	}
	if strings.Contains(out, "#3 · from root/groot · 2026-10-24 14:00 · ack") {
		t.Fatal("a message must not carry the ack tag")
	}
}
