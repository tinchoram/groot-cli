package inbox

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
)

func openTemp(t *testing.T) *Store {
	t.Helper()
	s, err := Open(filepath.Join(t.TempDir(), "inbox.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s
}

func msg(to, subject string) Message {
	return Message{FromSpace: "root", FromAgent: "groot", ToSpace: "payments", ToAgent: to, Subject: subject, Body: "body of " + subject}
}

func TestSendUnreadMarkReadRoundTrip(t *testing.T) {
	ctx := context.Background()
	s := openTemp(t)
	id, err := s.Send(ctx, msg("reviewer", "review the diff"))
	if err != nil || id == 0 {
		t.Fatalf("Send = %d, %v", id, err)
	}
	unread, err := s.Unread(ctx, "payments", "reviewer")
	if err != nil || len(unread) != 1 || unread[0].Subject != "review the diff" || unread[0].FromAgent != "groot" {
		t.Fatalf("Unread = %+v (%v)", unread, err)
	}
	if unread[0].ReadAt != nil {
		t.Fatal("a new message is unread")
	}
	if err := s.MarkRead(ctx, []int64{id}); err != nil {
		t.Fatal(err)
	}
	unread, _ = s.Unread(ctx, "payments", "reviewer")
	if len(unread) != 0 {
		t.Fatalf("after MarkRead, unread = %d", len(unread))
	}
	all, err := s.List(ctx, "payments", "reviewer", 10)
	if err != nil || len(all) != 1 || all[0].ReadAt == nil {
		t.Fatalf("List keeps read messages: %+v (%v)", all, err)
	}
}

func TestListIsNewestFirstAndLimited(t *testing.T) {
	ctx := context.Background()
	s := openTemp(t)
	for _, subj := range []string{"one", "two", "three"} {
		if _, err := s.Send(ctx, msg("reviewer", subj)); err != nil {
			t.Fatal(err)
		}
	}
	got, err := s.List(ctx, "payments", "reviewer", 2)
	if err != nil || len(got) != 2 || got[0].Subject != "three" || got[1].Subject != "two" {
		t.Fatalf("List = %+v (%v)", got, err)
	}
}

func TestMessagesAreAddressedPerAgent(t *testing.T) {
	ctx := context.Background()
	s := openTemp(t)
	_, _ = s.Send(ctx, msg("reviewer", "for reviewer"))
	_, _ = s.Send(ctx, msg("scribe", "for scribe"))
	unread, _ := s.Unread(ctx, "payments", "scribe")
	if len(unread) != 1 || unread[0].Subject != "for scribe" {
		t.Fatalf("scribe sees %+v", unread)
	}
}

func TestUnreadCountsKeyedBySpaceAndAgent(t *testing.T) {
	ctx := context.Background()
	s := openTemp(t)
	_, _ = s.Send(ctx, msg("reviewer", "a"))
	_, _ = s.Send(ctx, msg("reviewer", "b"))
	_, _ = s.Send(ctx, Message{FromAgent: "x", ToSpace: "root", ToAgent: "groot", Subject: "c", Body: "c"})
	counts, err := s.UnreadCounts(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if counts["payments/reviewer"] != 2 || counts["root/groot"] != 1 || len(counts) != 2 {
		t.Fatalf("counts = %v", counts)
	}
}

func TestBodyAndSubjectLimits(t *testing.T) {
	ctx := context.Background()
	s := openTemp(t)
	long := Message{ToSpace: "p", ToAgent: "a", Subject: strings.Repeat("s", 201), Body: "b"}
	if _, err := s.Send(ctx, long); err == nil {
		t.Fatal("subjects over 200 characters must be refused")
	}
	big := Message{ToSpace: "p", ToAgent: "a", Subject: "ok", Body: strings.Repeat("x", 16*1024+1)}
	if _, err := s.Send(ctx, big); err == nil {
		t.Fatal("bodies over 16 KB must be refused")
	}
	empty := Message{ToSpace: "p", ToAgent: "a", Subject: "", Body: ""}
	if _, err := s.Send(ctx, empty); err == nil {
		t.Fatal("a message needs a subject or a body")
	}
	exact := Message{ToSpace: "p", ToAgent: "a", Subject: strings.Repeat("s", 200), Body: strings.Repeat("x", 16*1024)}
	if _, err := s.Send(ctx, exact); err != nil {
		t.Fatalf("limits are inclusive: %v", err)
	}
}

func TestCountUnreadIsOneQueryPerAgent(t *testing.T) {
	ctx := context.Background()
	s := openTemp(t)
	id, _ := s.Send(ctx, msg("reviewer", "a"))
	_, _ = s.Send(ctx, msg("reviewer", "b"))
	_, _ = s.Send(ctx, msg("scribe", "c"))
	n, err := s.CountUnread(ctx, "payments", "reviewer")
	if err != nil || n != 2 {
		t.Fatalf("CountUnread = %d, %v", n, err)
	}
	_ = s.MarkRead(ctx, []int64{id})
	n, _ = s.CountUnread(ctx, "payments", "reviewer")
	if n != 1 {
		t.Fatalf("after MarkRead, CountUnread = %d", n)
	}
}

func TestMessagesSurviveReopen(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "inbox.db")
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	_, _ = s.Send(ctx, msg("reviewer", "persisted"))
	_ = s.Close()
	again, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer again.Close()
	unread, _ := again.Unread(ctx, "payments", "reviewer")
	if len(unread) != 1 || unread[0].Subject != "persisted" {
		t.Fatalf("after reopen: %+v", unread)
	}
}
