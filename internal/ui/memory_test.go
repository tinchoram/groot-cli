package ui

import (
	"strings"
	"testing"
	"time"

	"github.com/tinchoram/groot-cli/internal/memory"
)

func TestMemoryCommandListsThePreambleWithFlagsAndFooter(t *testing.T) {
	now := time.Now()
	chat := &fakeSession{
		model: "m",
		promptMemory: []memory.Entry{
			{ID: 7, Kind: memory.KindFact, Scope: memory.ScopeSpace, Text: "pager rota lives in ops-calendar", CreatedAt: now.Add(-2 * time.Hour)},
			{ID: 6, Kind: memory.KindSessionSummary, Scope: memory.ScopeAgent, Title: "Session 2026-10-22 09:00 payments", Text: "Goal: cluster", CreatedAt: now.Add(-72 * time.Hour)},
		},
		memoryOverview: memory.Overview{Visible: 5, Summaries: 2, Pinned: 1, NeedsReview: 0},
	}
	m, _ := newTestModel(t, chat)
	m = sendLine(t, m, "/memory")
	out := stripANSI(m.transcriptText())
	for _, want := range []string{
		"#7 ",
		" fact ",
		" space ",
		"2h  pager rota lives in ops-calendar",
		"#6 ",
		" session_summary ",
		"3d  Session 2026-10-22 09:00 payments",
		"5 visible · 2 summaries · 1 pinned · 0 need review",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("/memory lacks %q:\n%s", want, out)
		}
	}
	if len(chat.recallQueries) != 0 {
		t.Fatalf("/memory without a query must not search: %v", chat.recallQueries)
	}
}

func TestMemoryFlagsMarkPinnedReviewTaintedAndGlobal(t *testing.T) {
	chat := &fakeSession{
		model: "m",
		promptMemory: []memory.Entry{
			{ID: 4, Kind: memory.KindFact, Scope: memory.ScopeGlobal, Text: "root dns", Pinned: true, Tainted: true, State: memory.StateNeedsReview, CreatedAt: time.Now()},
		},
	}
	m, _ := newTestModel(t, chat)
	m = sendLine(t, m, "/memory")
	line := lineContaining(stripANSI(m.transcriptText()), "#4 ")
	for _, flag := range []string{"★", "?", "!", "G"} {
		if !strings.Contains(line, flag) {
			t.Fatalf("line %q lacks flag %s", line, flag)
		}
	}
}

func TestMemoryCommandWithQueryPrintsMatchesWithIDs(t *testing.T) {
	chat := &fakeSession{model: "m", recall: []memory.Entry{
		{ID: 3, Kind: memory.KindRunbook, Scope: memory.ScopeGlobal, Text: "restart foo with the runbook", CreatedAt: time.Now()},
	}}
	m, _ := newTestModel(t, chat)
	m = sendLine(t, m, "/memory foo")
	if got := chat.recallQueries; len(got) != 1 || got[0] != "foo" {
		t.Fatalf("recall queries = %v, want [foo]", got)
	}
	if out := stripANSI(m.transcriptText()); !strings.Contains(out, "#3 ") || !strings.Contains(out, "restart foo with the runbook") {
		t.Fatalf("/memory foo lacks the match:\n%s", out)
	}

	chat.recall = nil
	m = sendLine(t, m, "/memory nothing-here")
	if out := stripANSI(m.transcriptText()); !strings.Contains(out, "no memory matches nothing-here") {
		t.Fatalf("/memory without matches must say so:\n%s", out)
	}
}

func TestMemoryShowPrintsTheFullEntry(t *testing.T) {
	long := strings.Repeat("ops ", 60)
	chat := &fakeSession{model: "m", memoryByID: map[int64]memory.Entry{
		9: {ID: 9, Kind: memory.KindRunbook, Scope: memory.ScopeSpace, Title: "Drain", Text: long, CreatedAt: time.Now()},
	}}
	m, _ := newTestModel(t, chat)
	m = sendLine(t, m, "/memory show 9")
	out := stripANSI(m.transcriptText())
	if !strings.Contains(out, "#9 ") || !strings.Contains(out, "Drain") || !strings.Contains(out, strings.TrimSpace(long)) {
		t.Fatalf("/memory show lacks the full entry:\n%s", out)
	}
}

func TestMemoryPinUnpinForgetAndReviewRunThroughTheSession(t *testing.T) {
	chat := &fakeSession{model: "m", reviewList: []memory.Entry{
		{ID: 11, Kind: memory.KindFact, Scope: memory.ScopeSpace, Text: "old cluster name", State: memory.StateNeedsReview, CreatedAt: time.Now()},
	}}
	m, _ := newTestModel(t, chat)
	steps := []struct {
		line, want string
	}{
		{"/memory pin 7", "pinned #7"},
		{"/memory unpin 7", "unpinned #7"},
		{"/memory rm 7", "forgot #7"},
		{"/memory review", "#11 "},
		{"/memory reviewed 11", "reviewed #11"},
	}
	for _, s := range steps {
		m = sendLine(t, m, s.line)
		if out := stripANSI(m.transcriptText()); !strings.Contains(out, s.want) {
			t.Fatalf("%s: transcript lacks %q:\n%s", s.line, s.want, out)
		}
	}
	if got := strings.Join(chat.pinCalls, ","); got != "7=true,7=false" {
		t.Fatalf("pin calls = %q", got)
	}
	if len(chat.forgotten) != 1 || chat.forgotten[0] != 7 {
		t.Fatalf("forgotten = %v", chat.forgotten)
	}
	if len(chat.reviewed) != 1 || chat.reviewed[0] != 11 {
		t.Fatalf("reviewed = %v", chat.reviewed)
	}
}

func TestMemoryRejectsUnknownSubcommandsAndBadNumbers(t *testing.T) {
	tests := []struct {
		line, want string
	}{
		{"/memory pin", "usage: /memory"},
		{"/memory reviewed", "usage: /memory"},
		{"/memory pin abc", "id must be a positive number"},
		{"/memory pin 0", "id must be a positive number"},
		{"/memory rm -3", "id must be a positive number"},
		{"/memory show", "usage: /memory"},
	}
	for _, tt := range tests {
		t.Run(tt.line, func(t *testing.T) {
			chat := &fakeSession{model: "m"}
			m, _ := newTestModel(t, chat)
			m = sendLine(t, m, tt.line)
			if out := stripANSI(m.transcriptText()); !strings.Contains(out, tt.want) {
				t.Fatalf("transcript lacks %q:\n%s", tt.want, out)
			}
			if len(chat.pinCalls) != 0 || len(chat.forgotten) != 0 {
				t.Fatal("a bad command must not change memory")
			}
		})
	}
}

func TestMemoryErrorsAreNotesNotPanics(t *testing.T) {
	chat := &fakeSession{model: "m", memoryErr: memory.ErrNotFound}
	m, _ := newTestModel(t, chat)
	m = sendLine(t, m, "/memory rm 5")
	if out := stripANSI(m.transcriptText()); !strings.Contains(out, "error: memory entry not found") {
		t.Fatalf("a store error must be shown as a note:\n%s", out)
	}
}

// lineContaining returns the first line of s that contains sub.
func lineContaining(s, sub string) string {
	for _, line := range strings.Split(s, "\n") {
		if strings.Contains(line, sub) {
			return line
		}
	}
	return ""
}
