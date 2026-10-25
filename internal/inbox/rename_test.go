package inbox

import (
	"context"
	"testing"
)

func TestRenameSpaceKeepsUnreadMessagesReachable(t *testing.T) {
	ctx := context.Background()
	s := openTemp(t)
	if _, err := s.Send(ctx, msg("reviewer", "unread one")); err != nil {
		t.Fatal(err)
	}
	reply := Message{FromSpace: "payments", FromAgent: "reviewer", ToSpace: "root", ToAgent: "groot", Subject: "reply", Body: "x"}
	if _, err := s.Send(ctx, reply); err != nil {
		t.Fatal(err)
	}
	n, err := s.RenameSpace(ctx, "payments", "billing")
	if err != nil {
		t.Fatalf("RenameSpace: %v", err)
	}
	if n != 1 {
		t.Fatalf("rows moved to the new name = %d, want 1", n)
	}
	unread, err := s.Unread(ctx, "billing", "reviewer")
	if err != nil || len(unread) != 1 || unread[0].Subject != "unread one" {
		t.Fatalf("unread after rename = %+v, %v", unread, err)
	}
	old, _ := s.Unread(ctx, "payments", "reviewer")
	if len(old) != 0 {
		t.Fatalf("messages still addressed to the old name: %+v", old)
	}
	sent, _ := s.List(ctx, "root", "groot", 10)
	if len(sent) != 1 || sent[0].FromSpace != "billing" {
		t.Fatalf("sender of the reply = %+v, want billing", sent)
	}
}

func TestRenameAgentRewritesRecipientsAndSenders(t *testing.T) {
	ctx := context.Background()
	s := openTemp(t)
	if _, err := s.Send(ctx, msg("reviewer", "to the reviewer")); err != nil {
		t.Fatal(err)
	}
	out := Message{FromSpace: "payments", FromAgent: "reviewer", ToSpace: "root", ToAgent: "groot", Subject: "from the reviewer", Body: "x"}
	if _, err := s.Send(ctx, out); err != nil {
		t.Fatal(err)
	}
	if _, err := s.RenameAgent(ctx, "reviewer", "critic"); err != nil {
		t.Fatalf("RenameAgent: %v", err)
	}
	inbox, err := s.Unread(ctx, "payments", "critic")
	if err != nil || len(inbox) != 1 {
		t.Fatalf("inbox of the new name = %+v, %v", inbox, err)
	}
	sent, _ := s.List(ctx, "root", "groot", 10)
	if len(sent) != 1 || sent[0].FromAgent != "critic" {
		t.Fatalf("sender after rename = %+v", sent)
	}
	if left, _ := s.Unread(ctx, "payments", "reviewer"); len(left) != 0 {
		t.Fatal("messages still addressed to the old agent name")
	}
}
