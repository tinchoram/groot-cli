package workspace

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/tinchoram/groot-cli/internal/api"
	"github.com/tinchoram/groot-cli/internal/config"
	"github.com/tinchoram/groot-cli/internal/memory"
)

// seedStored writes a finished-looking session for "reader" into the space's
// memory, as an earlier run would have.
func seedStored(t *testing.T, f *fixture, id string, msgs ...api.Message) {
	t.Helper()
	ctx := context.Background()
	st, err := f.ws.storesFor(f.dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := st.mem.StartSession(ctx, memory.Session{ID: id, Agent: "reader", Model: "m", StartedAt: t0}); err != nil {
		t.Fatal(err)
	}
	for i, m := range msgs {
		if err := st.mem.AppendMessage(ctx, id, i/2+1, m, api.Usage{}); err != nil {
			t.Fatal(err)
		}
	}
}

func plain(role api.Role, text string) api.Message {
	return api.Message{Role: role, Content: []api.Block{{Type: api.BlockText, Text: text}}}
}

func TestChatResumesTheLatestStoredSession(t *testing.T) {
	f := newFixture(t, text("ok"))
	seedStored(t, f, "s-old", plain(api.RoleUser, "ancient"))
	seedStored(t, f, "s-1", plain(api.RoleUser, "earlier"), plain(api.RoleAssistant, "answer"))
	if err := f.ws.SelectAgent("reader"); err != nil {
		t.Fatal(err)
	}
	conv := f.ws.Chat()
	info, ok := conv.Resumed()
	if !ok || info.ID != "s-1" || info.Count != 2 || len(info.Messages) != 2 {
		t.Fatalf("Resumed = %+v, %v", info, ok)
	}

	if _, err := conv.Send(context.Background(), "next"); err != nil {
		t.Fatal(err)
	}
	if got := len(f.mock.Calls[0].Messages); got != 3 {
		t.Fatalf("model saw %d messages, want the restored 2 plus the prompt", got)
	}
	if conv.SessionID() != "s-1" {
		t.Fatalf("the conversation must keep its session id, got %s", conv.SessionID())
	}
}

func TestClearedSessionIsNotResumed(t *testing.T) {
	f := newFixture(t, text("ok"))
	seedStored(t, f, "s-1", plain(api.RoleUser, "old"), plain(api.RoleAssistant, "old answer"))
	st, _ := f.ws.storesFor(f.dir)
	if err := st.mem.MarkCleared(context.Background(), "s-1"); err != nil {
		t.Fatal(err)
	}
	_ = f.ws.SelectAgent("reader")
	conv := f.ws.Chat()
	if _, ok := conv.Resumed(); ok {
		t.Fatal("a cleared session must not be resumed")
	}
	if _, err := conv.Send(context.Background(), "fresh"); err != nil {
		t.Fatal(err)
	}
	if got := len(f.mock.Calls[0].Messages); got != 1 {
		t.Fatalf("a fresh conversation sent %d messages, want only the prompt", got)
	}
}

func TestClearEndsTheOldSessionWithSummaryAndStartsFresh(t *testing.T) {
	ctx := context.Background()
	// Steps: the first answer, the summary written at clear, the answer after clear.
	f := newFixture(t, text("one"), text("summary of the session"), text("two"))
	_ = f.ws.SelectAgent("reader")
	conv := f.ws.Chat()
	if _, err := conv.Send(ctx, "first"); err != nil {
		t.Fatal(err)
	}
	oldID := conv.SessionID()

	if err := conv.Clear()(ctx); err != nil {
		t.Fatalf("clear: %v", err)
	}
	st, _ := f.ws.storesFor(f.dir)
	latest, ok, err := st.mem.LatestSession(ctx, "reader")
	if err != nil || !ok || latest.ID != oldID || !latest.Cleared {
		t.Fatalf("old session after clear = %+v (%v, %v)", latest, ok, err)
	}
	if latest.Messages != 2 {
		t.Fatalf("old messages must stay in the database, got %d", latest.Messages)
	}
	if _, err := conv.Send(ctx, "second"); err != nil {
		t.Fatal(err)
	}
	if conv.SessionID() == oldID {
		t.Fatal("after clear the next send must open a fresh session")
	}
	if got := len(f.mock.Calls[2].Messages); got != 1 {
		t.Fatalf("fresh session sent %d messages, want only the prompt", got)
	}
}

func TestClearRefreshesTheMemoryPreamble(t *testing.T) {
	ctx := context.Background()
	// Steps: the first answer, the summary written at clear, the answer after clear.
	f := newFixture(t, text("one"), text("Asked about the cluster. Nothing open."), text("two"))
	_ = f.ws.SelectAgent("reader")
	conv := f.ws.Chat()
	if _, err := conv.Send(ctx, "first"); err != nil {
		t.Fatal(err)
	}
	st, _ := f.ws.storesFor(f.dir)
	if err := st.mem.Remember(ctx, memory.Entry{Kind: memory.KindFact, Scope: memory.ScopeProject, Agent: "reader", Text: "pager rota lives in ops-calendar"}); err != nil {
		t.Fatal(err)
	}
	if err := conv.Clear()(ctx); err != nil {
		t.Fatalf("clear: %v", err)
	}
	if _, err := conv.Send(ctx, "second"); err != nil {
		t.Fatal(err)
	}
	sys := f.mock.Calls[2].System
	for _, want := range []string{"pager rota lives in ops-calendar", "Asked about the cluster. Nothing open."} {
		if !strings.Contains(sys, want) {
			t.Fatalf("prompt after /clear lacks %q:\n%s", want, sys)
		}
	}
}

func TestChatMemoryListsThePreambleAndRecalls(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t, text("ok"))
	_ = f.ws.SelectAgent("reader")
	conv := f.ws.Chat()
	st, _ := f.ws.storesFor(f.dir)
	for _, text := range []string{"first note about backups", "second note about backups", "third note about dns"} {
		if err := st.mem.Remember(ctx, memory.Entry{Kind: memory.KindFact, Scope: memory.ScopeProject, Agent: "reader", Text: text}); err != nil {
			t.Fatal(err)
		}
	}
	entries, ov, err := conv.PromptMemory(ctx)
	if err != nil {
		t.Fatalf("Memory: %v", err)
	}
	if len(entries) != 3 || entries[0].Text != "third note about dns" {
		t.Fatalf("Memory entries = %+v, want the three notes newest first", entries)
	}
	if ov.Visible != 3 || ov.Summaries != 0 {
		t.Fatalf("Memory overview = %+v", ov)
	}
	found, err := conv.RecallMemory(ctx, "backups")
	if err != nil || len(found) != 2 {
		t.Fatalf("RecallMemory = %+v, %v; want the two backup notes", found, err)
	}
	if none, _ := conv.RecallMemory(ctx, "kubernetes"); len(none) != 0 {
		t.Fatalf("RecallMemory without matches = %+v", none)
	}
}

func TestEmptyStoredSessionStartsFresh(t *testing.T) {
	f := newFixture(t, text("ok"))
	seedStored(t, f, "s-empty")
	_ = f.ws.SelectAgent("reader")
	if _, ok := f.ws.Chat().Resumed(); ok {
		t.Fatal("a session without messages has nothing to resume")
	}
}

func TestRestoreHistoryPolicy(t *testing.T) {
	alt := []api.Message{
		plain(api.RoleUser, "u0"), plain(api.RoleAssistant, "a0"),
		plain(api.RoleUser, "u1"), plain(api.RoleAssistant, "a1"),
	}
	toolRound := []api.Message{
		plain(api.RoleUser, "u0"),
		{Role: api.RoleAssistant, Content: []api.Block{{Type: api.BlockToolUse, ID: "t", Name: "read_file"}}},
		{Role: api.RoleUser, Content: []api.Block{{Type: api.BlockToolResult, ToolUseID: "t", Content: "body"}}},
		plain(api.RoleAssistant, "a1"),
		plain(api.RoleUser, "u2"),
		plain(api.RoleAssistant, "a2"),
		plain(api.RoleUser, "u3"),
	}
	tests := []struct {
		name     string
		msgs     []api.Message
		strategy string
		keep     int
		wantLen  int
		wantTop  string // text of the first restored message
	}{
		{"none restores everything", alt, "none", 0, 4, "u0"},
		{"window keeps the last messages from a user turn", alt, "window", 2, 2, "u1"},
		{"window cuts at the last plain user turn", toolRound, "window", 2, 3, "u2"},
		{"window keeps all when no safe cut exists", toolRound, "window", 5, 7, "u0"},
		{"summarize restores the transcript cap", alt, "summarize", 0, 4, "u0"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := restoreHistory(tt.msgs, config.CompactionConfig{Strategy: tt.strategy, Keep: tt.keep})
			if len(got) != tt.wantLen {
				t.Fatalf("restored %d messages, want %d", len(got), tt.wantLen)
			}
			if first := got[0]; first.Role != api.RoleUser || first.Content[0].Text != tt.wantTop {
				t.Fatalf("first restored message = %+v, want user %q", first, tt.wantTop)
			}
		})
	}
}

func TestTranscriptIsCappedAtTheLastTwoHundredMessages(t *testing.T) {
	var msgs []api.Message
	for i := 0; i < 250; i++ {
		if i%2 == 0 {
			msgs = append(msgs, plain(api.RoleUser, fmt.Sprintf("u%d", i)))
		} else {
			msgs = append(msgs, plain(api.RoleAssistant, fmt.Sprintf("a%d", i)))
		}
	}
	got := transcriptTail(msgs)
	if len(got) != transcriptCap {
		t.Fatalf("transcript = %d messages, want %d", len(got), transcriptCap)
	}
	if got[0].Role != api.RoleUser {
		t.Fatal("the transcript must start at a user message")
	}
	if transcriptTail(msgs[:10])[0].Content[0].Text != "u0" {
		t.Fatal("short sessions are shown whole")
	}
}
