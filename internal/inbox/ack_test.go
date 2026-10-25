package inbox

import (
	"context"
	"database/sql"
	"path/filepath"
	"strings"
	"testing"
)

func ackTo(to, body string) Message {
	return Message{FromSpace: "payments", FromAgent: "debug", ToSpace: "payments", ToAgent: to, Body: body, Kind: KindAck}
}

func TestOpenMigratesAnOlderDatabaseToKinds(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "inbox.db")
	// The schema before the kind column existed.
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`CREATE TABLE messages (
		id INTEGER PRIMARY KEY AUTOINCREMENT, from_space TEXT NOT NULL, from_agent TEXT NOT NULL,
		to_space TEXT NOT NULL, to_agent TEXT NOT NULL, subject TEXT NOT NULL, body TEXT NOT NULL,
		created_at INTEGER NOT NULL, read_at INTEGER)`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO messages (from_space, from_agent, to_space, to_agent, subject, body, created_at)
		VALUES ('payments', 'groot', 'payments', 'reviewer', 'old', 'kept', 1)`); err != nil {
		t.Fatal(err)
	}
	_ = db.Close()

	s, err := Open(path)
	if err != nil {
		t.Fatalf("Open of an older database: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	unread, err := s.Unread(ctx, "payments", "reviewer")
	if err != nil || len(unread) != 1 || unread[0].Kind != KindMessage || unread[0].Body != "kept" {
		t.Fatalf("old row after migration = %+v (%v), want one message of kind %q", unread, err, KindMessage)
	}
	if _, err := s.Send(ctx, ackTo("reviewer", "FEAT-016 recorded")); err != nil {
		t.Fatalf("ack after migration: %v", err)
	}
}

func TestAcksAreLeftOutOfUnreadAndCounts(t *testing.T) {
	ctx := context.Background()
	s := openTemp(t)
	if _, err := s.Send(ctx, msg("reviewer", "real")); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Send(ctx, ackTo("reviewer", "FEAT-016 recorded")); err != nil {
		t.Fatal(err)
	}
	unread, err := s.Unread(ctx, "payments", "reviewer")
	if err != nil || len(unread) != 1 || unread[0].Subject != "real" {
		t.Fatalf("Unread = %+v (%v), want only the message", unread, err)
	}
	if n, _ := s.CountUnread(ctx, "payments", "reviewer"); n != 1 {
		t.Fatalf("CountUnread = %d, want 1", n)
	}
	if counts, _ := s.UnreadCounts(ctx); counts["payments/reviewer"] != 1 {
		t.Fatalf("UnreadCounts = %v, want 1 for payments/reviewer", counts)
	}
}

func TestPendingAcksAreListedOnceAndMarkedRead(t *testing.T) {
	ctx := context.Background()
	s := openTemp(t)
	if _, err := s.Send(ctx, ackTo("reviewer", "FEAT-016 recorded")); err != nil {
		t.Fatal(err)
	}
	pending, err := s.PendingAcks(ctx, "payments", "reviewer")
	if err != nil || len(pending) != 1 || pending[0].Body != "FEAT-016 recorded" || pending[0].Kind != KindAck {
		t.Fatalf("PendingAcks = %+v (%v)", pending, err)
	}
	ids := []int64{pending[0].ID}
	if err := s.MarkAcksRead(ctx, ids); err != nil {
		t.Fatal(err)
	}
	if again, _ := s.PendingAcks(ctx, "payments", "reviewer"); len(again) != 0 {
		t.Fatalf("PendingAcks after MarkAcksRead = %+v, want none", again)
	}
	all, err := s.List(ctx, "payments", "reviewer", 10)
	if err != nil || len(all) != 1 || all[0].Kind != KindAck || all[0].ReadAt == nil {
		t.Fatalf("history keeps the ack as read: %+v (%v)", all, err)
	}
}

func TestMarkAcksReadLeavesMessagesUnread(t *testing.T) {
	ctx := context.Background()
	s := openTemp(t)
	id, err := s.Send(ctx, msg("reviewer", "real"))
	if err != nil {
		t.Fatal(err)
	}
	if err := s.MarkAcksRead(ctx, []int64{id}); err != nil {
		t.Fatal(err)
	}
	if n, _ := s.CountUnread(ctx, "payments", "reviewer"); n != 1 {
		t.Fatalf("a message was marked read by MarkAcksRead; unread = %d", n)
	}
}

func TestAckBodyIsCappedAtTwoHundredRunes(t *testing.T) {
	ctx := context.Background()
	s := openTemp(t)
	if _, err := s.Send(ctx, ackTo("reviewer", strings.Repeat("é", MaxAckRunes))); err != nil {
		t.Fatalf("a %d-rune ack must be accepted: %v", MaxAckRunes, err)
	}
	if _, err := s.Send(ctx, ackTo("reviewer", strings.Repeat("x", MaxAckRunes+1))); err == nil {
		t.Fatal("a longer ack must be refused")
	}
	long := msg("reviewer", "")
	long.Body = strings.Repeat("x", MaxAckRunes+1)
	if _, err := s.Send(ctx, long); err != nil {
		t.Fatalf("a long plain message is not limited: %v", err)
	}
}

func TestUnknownKindIsRefused(t *testing.T) {
	s := openTemp(t)
	m := msg("reviewer", "x")
	m.Kind = "broadcast"
	if _, err := s.Send(context.Background(), m); err == nil {
		t.Fatal("an unknown kind must be refused")
	}
}

func TestRenameAgentKeepsPendingAcks(t *testing.T) {
	ctx := context.Background()
	s := openTemp(t)
	if _, err := s.Send(ctx, ackTo("reviewer", "ok")); err != nil {
		t.Fatal(err)
	}
	if _, err := s.RenameAgent(ctx, "reviewer", "qa"); err != nil {
		t.Fatal(err)
	}
	pending, err := s.PendingAcks(ctx, "payments", "qa")
	if err != nil || len(pending) != 1 {
		t.Fatalf("pending acks after rename = %+v (%v)", pending, err)
	}
}
