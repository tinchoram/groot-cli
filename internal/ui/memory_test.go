package ui

import (
	"strings"
	"testing"
	"time"

	"github.com/tinchoram/groot-cli/internal/memory"
)

func TestMemoryCommandListsThePromptEntriesAndFooter(t *testing.T) {
	now := time.Now()
	chat := &fakeSession{
		model: "m",
		promptMemory: []memory.Entry{
			{ID: 7, Kind: memory.KindFact, Scope: memory.ScopeProject, Text: "pager rota lives in ops-calendar", CreatedAt: now.Add(-2 * time.Hour)},
			{ID: 6, Kind: memory.KindSessionSummary, Scope: memory.ScopeProject, Text: "Asked about the cluster", CreatedAt: now.Add(-72 * time.Hour)},
		},
		memoryOverview: memory.Overview{Visible: 5, Summaries: 2},
	}
	m, _ := newTestModel(t, chat)
	m = sendLine(t, m, "/memory")
	out := stripANSI(m.transcriptText())
	for _, want := range []string{
		"7 fact project 2h pager rota lives in ops-calendar",
		"6 session_summary project 3d Asked about the cluster",
		"5 entries visible to this agent · 2 session summaries",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("/memory lacks %q:\n%s", want, out)
		}
	}
	if len(chat.recallQueries) != 0 {
		t.Fatalf("/memory without a query must not search: %v", chat.recallQueries)
	}
}

func TestMemoryCommandWithQueryPrintsMatchesOrNone(t *testing.T) {
	chat := &fakeSession{model: "m", recall: []memory.Entry{
		{ID: 3, Kind: memory.KindRunbook, Scope: memory.ScopeGlobal, Text: "restart foo with the runbook", CreatedAt: time.Now()},
	}}
	m, _ := newTestModel(t, chat)
	m = sendLine(t, m, "/memory foo")
	if got := chat.recallQueries; len(got) != 1 || got[0] != "foo" {
		t.Fatalf("recall queries = %v, want [foo]", got)
	}
	if out := stripANSI(m.transcriptText()); !strings.Contains(out, "3 runbook global 0m restart foo with the runbook") {
		t.Fatalf("/memory foo lacks the match:\n%s", out)
	}

	chat.recall = nil
	m = sendLine(t, m, "/memory nothing-here")
	if out := stripANSI(m.transcriptText()); !strings.Contains(out, "no memory matches nothing-here") {
		t.Fatalf("/memory without matches must say so:\n%s", out)
	}
}
