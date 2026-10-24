package agent

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/tinchoram/groot-cli/internal/api"
	"github.com/tinchoram/groot-cli/internal/memory"
	"github.com/tinchoram/groot-cli/internal/provider"
)

func replyStep(text string) provider.Step {
	return provider.Step{Response: api.Response{
		Content:    []api.Block{{Type: api.BlockText, Text: text}},
		StopReason: api.StopEndTurn,
	}}
}

func openStore(t *testing.T) *memory.SQLite {
	t.Helper()
	s, err := memory.OpenSQLite(filepath.Join(t.TempDir(), "groot.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s
}

func plainText(role api.Role, text string) api.Message {
	return api.Message{Role: role, Content: []api.Block{{Type: api.BlockText, Text: text}}}
}

func TestResumeContinuesTheSameSession(t *testing.T) {
	ctx := context.Background()
	store := openStore(t)
	if err := store.StartSession(ctx, memory.Session{ID: "s-1", Agent: "reader", Model: "m",
		StartedAt: time.Now().UTC()}); err != nil {
		t.Fatal(err)
	}
	earlier := []api.Message{plainText(api.RoleUser, "earlier"), plainText(api.RoleAssistant, "answer")}
	for _, m := range earlier {
		if err := store.AppendMessage(ctx, "s-1", 1, m, api.Usage{}); err != nil {
			t.Fatal(err)
		}
	}

	mock := provider.NewMock(replyStep("ok"))
	ag := &Agent{Name: "reader", Provider: mock, Memory: store}
	ag.Resume("s-1", earlier, 1, 0)

	if _, err := ag.Send(ctx, "next"); err != nil {
		t.Fatal(err)
	}
	if got := len(mock.Calls[0].Messages); got != 3 {
		t.Fatalf("model saw %d messages, want the 2 restored plus the new prompt", got)
	}
	if ag.SessionID() != "s-1" {
		t.Fatalf("session id = %s, want the resumed s-1", ag.SessionID())
	}
	latest, ok, err := store.LatestSession(ctx, "reader")
	if err != nil || !ok {
		t.Fatalf("LatestSession: %v %v", ok, err)
	}
	if latest.ID != "s-1" || latest.Messages != 4 || latest.Turns != 2 {
		t.Fatalf("stored session = %+v; new turns must append to s-1 with the turn counter continued", latest)
	}
}

func TestDetachLeavesTheOldSessionAndOpensAFreshOne(t *testing.T) {
	ctx := context.Background()
	store := openStore(t)
	mock := provider.NewMock(replyStep("first answer"), replyStep("second answer"))
	ag := &Agent{Name: "reader", Provider: mock, Memory: store}

	if _, err := ag.Send(ctx, "first"); err != nil {
		t.Fatal(err)
	}
	oldID := ag.SessionID()
	id, transcript, n := ag.Detach()
	if id != oldID || n != 2 || transcript == "" {
		t.Fatalf("Detach = %q, %q, %d; want the old id, its transcript and 2 messages", id, transcript, n)
	}
	if ag.SessionID() != "" || ag.MessageCount() != 0 {
		t.Fatal("after Detach the agent must have no session and no history")
	}

	if _, err := ag.Send(ctx, "second"); err != nil {
		t.Fatal(err)
	}
	if ag.SessionID() == oldID || ag.SessionID() == "" {
		t.Fatalf("next send must open a new session, got %q", ag.SessionID())
	}
	if got := len(mock.Calls[1].Messages); got != 1 {
		t.Fatalf("the fresh session sent %d messages, want only the new prompt", got)
	}
	old, _, _ := store.LatestSession(ctx, "reader")
	if old.ID != ag.SessionID() {
		t.Fatalf("latest session should be the new one, got %s", old.ID)
	}
}
