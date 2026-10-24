package ui

import (
	"strings"
	"testing"
	"time"

	"github.com/tinchoram/groot-cli/internal/api"
	"github.com/tinchoram/groot-cli/internal/workspace"
)

var activity = time.Date(2026, 10, 24, 15, 30, 0, 0, time.UTC)

func storedSession() *workspace.Resumed {
	return &workspace.Resumed{
		ID:           "s-7",
		Count:        4,
		LastActivity: activity,
		Messages: []api.Message{
			{Role: api.RoleUser, Content: []api.Block{{Type: api.BlockText, Text: "hello there"}}},
			{Role: api.RoleAssistant, Content: []api.Block{
				{Type: api.BlockText, Text: "reading the file"},
				{Type: api.BlockToolUse, ID: "tu_1", Name: "read_file"},
			}},
			{Role: api.RoleUser, Content: []api.Block{
				{Type: api.BlockToolResult, ToolUseID: "tu_1", Content: "SECRET-BODY-NOT-SHOWN"},
			}},
			{Role: api.RoleAssistant, Content: []api.Block{{Type: api.BlockText, Text: "all done"}}},
		},
	}
}

func TestResumedTranscriptShowsTheStoredTurns(t *testing.T) {
	chat := &fakeSession{model: "m", resumed: storedSession()}
	m, _ := newTestModel(t, chat)
	out := stripANSI(m.transcriptText())

	for _, want := range []string{
		"resumed session s-7 · 4 messages · last activity " + activity.Local().Format("2006-01-02 15:04"),
		"❯ hello there",
		"reading the file",
		"[tool] read_file",
		"all done",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("transcript misses %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "SECRET-BODY-NOT-SHOWN") {
		t.Fatal("tool result bodies must not be drawn")
	}
	if strings.Index(out, "resumed session") > strings.Index(out, "❯ hello there") {
		t.Fatal("the resume line must come before the first turn")
	}
}

func TestResumedTranscriptIsDrawnOnlyOnce(t *testing.T) {
	chat := &fakeSession{model: "m", resumed: storedSession()}
	m, _ := newTestModel(t, chat)
	before := strings.Count(stripANSI(m.transcriptText()), "resumed session")
	m.syncConversation()
	m.syncConversation()
	if after := strings.Count(stripANSI(m.transcriptText()), "resumed session"); after != before || before != 1 {
		t.Fatalf("resume line drawn %d times after %d syncs, want once", after, 2)
	}
}

func TestClearEmptiesTheTranscriptAndFinishesTheOldSession(t *testing.T) {
	chat := &fakeSession{model: "m", resumed: storedSession()}
	m, _ := newTestModel(t, chat)
	m.input.SetValue("/clear")
	next, cmd := m.submit()
	m = next.(Model)

	if !chat.cleared {
		t.Fatal("/clear must detach the conversation")
	}
	if strings.Contains(stripANSI(m.transcriptText()), "hello there") {
		t.Fatal("the transcript must be empty after /clear")
	}
	if cmd == nil {
		t.Fatal("/clear must return the command that finishes the old session")
	}
	note, ok := cmd().(noteMsg)
	if !ok || note.err != nil || !strings.Contains(note.text, "conversation cleared") {
		t.Fatalf("finish message = %#v", cmd())
	}
}
