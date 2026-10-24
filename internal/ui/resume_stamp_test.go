package ui

import (
	"strings"
	"testing"
	"time"

	"github.com/tinchoram/groot-cli/internal/api"
	"github.com/tinchoram/groot-cli/internal/workspace"
)

func TestResumedDividersCarryEachMessageTime(t *testing.T) {
	first := time.Date(2026, 10, 24, 9, 5, 0, 0, time.UTC)
	second := time.Date(2026, 10, 24, 17, 40, 0, 0, time.UTC)
	chat := &fakeSession{model: "m", resumed: &workspace.Resumed{
		ID: "s-9", Count: 2, LastActivity: second,
		Messages: []api.Message{
			{Role: api.RoleUser, Content: []api.Block{{Type: api.BlockText, Text: "one"}}},
			{Role: api.RoleUser, Content: []api.Block{{Type: api.BlockText, Text: "two"}}},
		},
		Times: []time.Time{first, second},
	}}
	m, _ := newTestModel(t, chat)
	out := stripANSI(m.transcriptText())
	if !strings.Contains(out, first.Local().Format("15:04:05")) || !strings.Contains(out, second.Local().Format("15:04:05")) {
		t.Fatalf("each divider should show its own time:\n%s", out)
	}
}

func TestInjectedBlocksAreSummarizedNotEchoed(t *testing.T) {
	text := "[inbox] from default/writer · 2026-10-24 12:00\nSubject: spec\nsecret body text\n\n" +
		"[task #3 · medium · implement] goal: x\n\n" +
		"revisa la bandeja"
	chat := &fakeSession{model: "m", resumed: &workspace.Resumed{
		ID: "s-10", Count: 1, LastActivity: time.Now(),
		Messages: []api.Message{{Role: api.RoleUser, Content: []api.Block{{Type: api.BlockText, Text: text}}}},
	}}
	m, _ := newTestModel(t, chat)
	out := stripANSI(m.transcriptText())
	if !strings.Contains(out, "[inbox] message delivered from default/writer") {
		t.Fatalf("the delivery is summarized:\n%s", out)
	}
	if !strings.Contains(out, "❯ revisa la bandeja") {
		t.Fatalf("only what the user typed is echoed:\n%s", out)
	}
	if strings.Contains(out, "secret body text") || strings.Contains(out, "❯ [inbox]") {
		t.Fatalf("no body and no injected echo:\n%s", out)
	}
}
