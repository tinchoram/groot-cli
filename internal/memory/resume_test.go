package memory

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"
	"time"

	"github.com/tinchoram/groot-cli/internal/api"
)

var base = time.Date(2026, 10, 24, 9, 0, 0, 0, time.UTC)

func openTempSQLite(t *testing.T) *SQLite {
	t.Helper()
	s, err := OpenSQLite(filepath.Join(t.TempDir(), "groot.db"))
	if err != nil {
		t.Fatalf("OpenSQLite: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s
}

func userText(text string) api.Message {
	return api.Message{Role: api.RoleUser, Content: []api.Block{{Type: api.BlockText, Text: text}}}
}

func assistantText(text string) api.Message {
	return api.Message{Role: api.RoleAssistant, Content: []api.Block{{Type: api.BlockText, Text: text}}}
}

// seedSession writes a session and its messages, one turn per message pair.
func seedSession(t *testing.T, s *SQLite, id, agent string, started time.Time, msgs ...api.Message) {
	t.Helper()
	ctx := context.Background()
	if err := s.StartSession(ctx, Session{ID: id, Agent: agent, Model: "m", StartedAt: started}); err != nil {
		t.Fatal(err)
	}
	for i, m := range msgs {
		if err := s.AppendMessage(ctx, id, i/2+1, m, api.Usage{}); err != nil {
			t.Fatal(err)
		}
	}
}

func TestLatestSessionIsTheNewestOfTheAgent(t *testing.T) {
	ctx := context.Background()
	s := openTempSQLite(t)
	seedSession(t, s, "s-old", "reader", base, userText("a"), assistantText("b"))
	seedSession(t, s, "s-new", "reader", base.Add(time.Hour),
		userText("c"), assistantText("d"), userText("e"), assistantText("f"))
	seedSession(t, s, "s-other", "writer", base.Add(2*time.Hour), userText("g"))

	got, ok, err := s.LatestSession(ctx, "reader")
	if err != nil || !ok {
		t.Fatalf("LatestSession = %v, %v", ok, err)
	}
	if got.ID != "s-new" || got.Messages != 4 || got.Turns != 2 {
		t.Fatalf("latest = %+v", got)
	}
	if got.Cleared {
		t.Fatal("a session that was never cleared must not report cleared")
	}
}

func TestLatestSessionReportsNothingForUnknownAgent(t *testing.T) {
	s := openTempSQLite(t)
	_, ok, err := s.LatestSession(context.Background(), "nobody")
	if err != nil || ok {
		t.Fatalf("ok=%v err=%v, want no session and no error", ok, err)
	}
}

func TestSessionMessagesKeepOrderAndBlocks(t *testing.T) {
	ctx := context.Background()
	s := openTempSQLite(t)
	toolUse := api.Message{Role: api.RoleAssistant, Content: []api.Block{
		{Type: api.BlockText, Text: "let me look"},
		{Type: api.BlockToolUse, ID: "tu_1", Name: "read_file", Input: []byte(`{"path":"a"}`)},
	}}
	toolResult := api.Message{Role: api.RoleUser, Content: []api.Block{
		{Type: api.BlockToolResult, ToolUseID: "tu_1", Content: "file body"},
	}}
	seedSession(t, s, "s-1", "reader", base, userText("read a"), toolUse, toolResult, assistantText("done"))

	got, err := s.SessionMessages(ctx, "s-1")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 4 {
		t.Fatalf("messages = %d, want 4", len(got))
	}
	if got[1].Content[1].Type != api.BlockToolUse || got[1].Content[1].Name != "read_file" {
		t.Fatalf("tool_use block lost: %+v", got[1].Content)
	}
	if got[2].Content[0].Type != api.BlockToolResult || got[2].Content[0].Content != "file body" {
		t.Fatalf("tool_result lost: %+v", got[2].Content)
	}
	if got[3].Role != api.RoleAssistant || got[3].Content[0].Text != "done" {
		t.Fatalf("last message = %+v", got[3])
	}
}

func TestMarkClearedIsReportedAndEndSessionRecordsEnd(t *testing.T) {
	ctx := context.Background()
	s := openTempSQLite(t)
	seedSession(t, s, "s-1", "reader", base, userText("hi"))
	if err := s.EndSession(ctx, "s-1", 0.01); err != nil {
		t.Fatal(err)
	}
	got, _, _ := s.LatestSession(ctx, "reader")
	if got.EndedAt.IsZero() {
		t.Fatal("ended session should carry its end time")
	}
	if err := s.MarkCleared(ctx, "s-1"); err != nil {
		t.Fatal(err)
	}
	got, _, _ = s.LatestSession(ctx, "reader")
	if !got.Cleared {
		t.Fatal("marked session should report cleared")
	}
}

func TestOpenAddsClearedColumnToOlderDatabases(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "groot.db")
	s, err := OpenSQLite(path)
	if err != nil {
		t.Fatal(err)
	}
	seedSession(t, s, "s-1", "reader", base, userText("kept"))
	_ = s.Close()

	// Simulate a database written before cleared_at existed.
	raw, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := raw.Exec(`ALTER TABLE sessions DROP COLUMN cleared_at`); err != nil {
		t.Fatal(err)
	}
	_ = raw.Close()

	again, err := OpenSQLite(path)
	if err != nil {
		t.Fatalf("reopen after dropping the column: %v", err)
	}
	defer again.Close()
	got, ok, err := again.LatestSession(ctx, "reader")
	if err != nil || !ok || got.ID != "s-1" || got.Cleared {
		t.Fatalf("after migration: %+v ok=%v err=%v", got, ok, err)
	}
	// Opening a third time must be a no-op.
	third, err := OpenSQLite(path)
	if err != nil {
		t.Fatalf("second migration pass: %v", err)
	}
	_ = third.Close()
}

func TestNoMemoryHasNothingToResume(t *testing.T) {
	var st Store = NoMemory{}
	if _, ok, err := st.LatestSession(context.Background(), "x"); err != nil || ok {
		t.Fatalf("NoMemory LatestSession ok=%v err=%v", ok, err)
	}
	if msgs, err := st.SessionMessages(context.Background(), "x"); err != nil || msgs != nil {
		t.Fatalf("NoMemory SessionMessages = %v, %v", msgs, err)
	}
	if err := st.MarkCleared(context.Background(), "x"); err != nil {
		t.Fatal(err)
	}
}
