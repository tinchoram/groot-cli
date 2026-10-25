package memory

import (
	"context"
	"database/sql"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/tinchoram/groot-cli/internal/api"
)

func openTemp(t *testing.T) *SQLite {
	t.Helper()
	path := filepath.Join(t.TempDir(), "nested", "groot.db")
	s, err := OpenSQLite(path)
	if err != nil {
		t.Fatalf("OpenSQLite: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s
}

func TestRememberRecallRoundTrip(t *testing.T) {
	ctx := context.Background()
	s := openTemp(t)

	err := s.Remember(ctx, Entry{Kind: KindRunbook, Scope: ScopeProject, Agent: "sre", Text: "Restart the ingress controller with kubectl rollout restart", Source: "test"})
	if err != nil {
		t.Fatalf("Remember: %v", err)
	}

	got, err := s.Recall(ctx, "ingress", "sre", 5)
	if err != nil {
		t.Fatalf("Recall: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("want 1 match, got %d", len(got))
	}
	if got[0].Kind != KindRunbook || got[0].Scope != ScopeProject || got[0].Agent != "sre" {
		t.Fatalf("fields not persisted: %+v", got[0])
	}
	if got[0].ID == 0 || got[0].CreatedAt.IsZero() {
		t.Fatalf("id/created_at not set: %+v", got[0])
	}
}

func TestRecallMatchesWordsAndSanitizesQuery(t *testing.T) {
	ctx := context.Background()
	s := openTemp(t)
	_ = s.Remember(ctx, Entry{Kind: KindFact, Scope: ScopeGlobal, Text: "The payments cluster runs in eu-west-1"})
	_ = s.Remember(ctx, Entry{Kind: KindFact, Scope: ScopeGlobal, Text: "Grafana dashboards live in the monitoring namespace"})

	got, err := s.Recall(ctx, "payments", "", 10)
	if err != nil || len(got) != 1 {
		t.Fatalf("want 1 payments match, got %d (err %v)", len(got), err)
	}
	// FTS syntax characters must not break the query.
	if _, err := s.Recall(ctx, `"unbalanced (quote AND OR * :`, "", 10); err != nil {
		t.Fatalf("sanitized query failed: %v", err)
	}
	empty, err := s.Recall(ctx, "   ", "", 10)
	if err != nil || len(empty) != 0 {
		t.Fatalf("blank query should return nothing, got %d (err %v)", len(empty), err)
	}
}

func TestRememberRejectsInvalidKind(t *testing.T) {
	s := openTemp(t)
	if err := s.Remember(context.Background(), Entry{Kind: "gossip", Text: "x"}); err == nil {
		t.Fatal("expected invalid kind to fail")
	}
}

func TestPreambleEntriesAndOverviewCountVisibleEntries(t *testing.T) {
	ctx := context.Background()
	s := openTemp(t)
	_ = s.Remember(ctx, Entry{Kind: KindFact, Scope: ScopeProject, Agent: "a", Text: "older fact for a"})
	_ = s.Remember(ctx, Entry{Kind: KindSessionSummary, Scope: ScopeProject, Agent: "a", Text: "summary for a"})
	_ = s.Remember(ctx, Entry{Kind: KindFact, Scope: ScopeAgent, Agent: "b", Text: "private to b"})
	_ = s.Remember(ctx, Entry{Kind: KindDecision, Scope: ScopeGlobal, Text: "global decision"})

	entries, err := s.PreambleEntries(ctx, "a", 2)
	if err != nil {
		t.Fatalf("PreambleEntries: %v", err)
	}
	if len(entries) != 2 || entries[0].Text != "global decision" || entries[0].Kind != KindDecision || entries[1].Text != "summary for a" {
		t.Fatalf("PreambleEntries = %+v, want the two newest visible entries, newest first", entries)
	}
	pre, err := s.Preamble(ctx, "a", 2)
	if err != nil {
		t.Fatalf("Preamble: %v", err)
	}
	if want := "- [decision] global decision\n- [session_summary] summary for a\n"; pre != want {
		t.Fatalf("Preamble = %q, want %q", pre, want)
	}
	ov, err := s.Overview(ctx, "a")
	if err != nil {
		t.Fatalf("Overview: %v", err)
	}
	if ov.Visible != 3 || ov.Summaries != 1 {
		t.Fatalf("Overview = %+v, want 3 visible entries with 1 summary", ov)
	}
}

func TestPreambleOrderingAndScope(t *testing.T) {
	ctx := context.Background()
	s := openTemp(t)
	_ = s.Remember(ctx, Entry{Kind: KindFact, Scope: ScopeProject, Text: "first project fact"})
	_ = s.Remember(ctx, Entry{Kind: KindDecision, Scope: ScopeProject, Text: "second project decision"})
	_ = s.Remember(ctx, Entry{Kind: KindFact, Scope: ScopeGlobal, Text: "global note"})
	_ = s.Remember(ctx, Entry{Kind: KindFact, Scope: ScopeAgent, Text: "agent only note"})

	pre, err := s.Preamble(ctx, "", 2)
	if err != nil {
		t.Fatalf("Preamble: %v", err)
	}
	if strings.Contains(pre, "agent only note") {
		t.Fatalf("agent-scoped entry leaked into project preamble:\n%s", pre)
	}
	if !strings.Contains(pre, "global note") {
		t.Fatalf("global entries should be included:\n%s", pre)
	}
	// Newest first: the global note was inserted last among eligible entries.
	if strings.Index(pre, "global note") > strings.Index(pre, "second project decision") {
		t.Fatalf("expected newest first:\n%s", pre)
	}
	if strings.Contains(pre, "first project fact") {
		t.Fatalf("limit of 2 not applied:\n%s", pre)
	}
}

func TestSessionAndMessagesPersisted(t *testing.T) {
	ctx := context.Background()
	s := openTemp(t)
	if err := s.StartSession(ctx, Session{ID: "s1", Agent: "sre", Model: "claude-sonnet-5-5", StartedAt: time.Now()}); err != nil {
		t.Fatalf("StartSession: %v", err)
	}
	msg := api.Message{Role: api.RoleUser, Content: []api.Block{{Type: api.BlockText, Text: "hello"}}}
	usage := api.Usage{InputTokens: 12, OutputTokens: 4}
	if err := s.AppendMessage(ctx, "s1", 1, msg, usage); err != nil {
		t.Fatalf("AppendMessage: %v", err)
	}
	if err := s.EndSession(ctx, "s1", 0.0042); err != nil {
		t.Fatalf("EndSession: %v", err)
	}

	var role, content string
	var in, out int64
	if err := s.db.QueryRow(`SELECT role, content_json, in_tokens, out_tokens FROM messages WHERE session_id = ?`, "s1").Scan(&role, &content, &in, &out); err != nil {
		t.Fatalf("query message: %v", err)
	}
	if role != "user" || in != 12 || out != 4 {
		t.Fatalf("bad row: role=%s in=%d out=%d", role, in, out)
	}
	var decoded api.Message
	if err := json.Unmarshal([]byte(content), &decoded); err != nil || decoded.Content[0].Text != "hello" {
		t.Fatalf("content_json not decodable: %v %s", err, content)
	}

	var cost float64
	var ended sql.NullTime
	if err := s.db.QueryRow(`SELECT cost_usd, ended_at FROM sessions WHERE id = ?`, "s1").Scan(&cost, &ended); err != nil {
		t.Fatalf("query session: %v", err)
	}
	if cost != 0.0042 || !ended.Valid {
		t.Fatalf("session not ended: cost=%v ended=%v", cost, ended)
	}
}

func TestNoMemoryIsInert(t *testing.T) {
	ctx := context.Background()
	var m Store = NoMemory{}
	if err := m.Remember(ctx, Entry{Kind: KindFact, Text: "x"}); err != nil {
		t.Fatal(err)
	}
	got, err := m.Recall(ctx, "x", "", 5)
	if err != nil || len(got) != 0 {
		t.Fatalf("NoMemory recall should be empty: %v %v", got, err)
	}
	pre, err := m.Preamble(ctx, "", 5)
	if err != nil || pre != "" {
		t.Fatalf("NoMemory preamble should be empty: %q %v", pre, err)
	}
}

func TestAgentsOnlySeeTheirOwnProjectFacts(t *testing.T) {
	ctx := context.Background()
	s := openTemp(t)
	_ = s.Remember(ctx, Entry{Kind: KindFact, Scope: ScopeProject, Agent: "reviewer", Text: "reviewer fact about payments"})
	_ = s.Remember(ctx, Entry{Kind: KindFact, Scope: ScopeProject, Agent: "devjunior", Text: "devjunior fact about payments"})
	_ = s.Remember(ctx, Entry{Kind: KindFact, Scope: ScopeGlobal, Agent: "devjunior", Text: "global fact about payments"})
	_ = s.Remember(ctx, Entry{Kind: KindFact, Scope: ScopeProject, Agent: "", Text: "legacy fact about payments"})

	for _, agent := range []string{"reviewer", "devjunior"} {
		got, err := s.Recall(ctx, "payments", agent, 10)
		if err != nil {
			t.Fatal(err)
		}
		texts := ""
		for _, e := range got {
			texts += e.Text + "|"
		}
		own := "reviewer fact"
		if agent == "devjunior" {
			own = "devjunior fact"
		}
		other := "devjunior fact"
		if agent == "devjunior" {
			other = "reviewer fact"
		}
		if !strings.Contains(texts, own) || strings.Contains(texts, other) {
			t.Errorf("%s recall = %q: must see its own facts and not the other agent's", agent, texts)
		}
		if !strings.Contains(texts, "global fact") || !strings.Contains(texts, "legacy fact") {
			t.Errorf("%s recall = %q: must see global and legacy rows", agent, texts)
		}
	}

	pre, err := s.Preamble(ctx, "reviewer", 10)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(pre, "devjunior fact") || !strings.Contains(pre, "reviewer fact") || !strings.Contains(pre, "global fact") {
		t.Fatalf("preamble for reviewer wrong:\n%s", pre)
	}
}

func TestAgentScopedEntriesAreVisibleOnlyToTheirAgent(t *testing.T) {
	ctx := context.Background()
	s := openTemp(t)
	if err := s.Remember(ctx, Entry{Kind: KindRunbook, Scope: ScopeAgent, Agent: "a", Text: "agent a runbook about quota"}); err != nil {
		t.Fatalf("Remember: %v", err)
	}

	got, err := s.Recall(ctx, "quota", "a", 10)
	if err != nil || len(got) != 1 {
		t.Fatalf("agent a must recall its own agent-scoped entry, got %d (err %v)", len(got), err)
	}
	if got[0].Scope != ScopeAgent || got[0].Agent != "a" {
		t.Fatalf("scope/agent not persisted: %+v", got[0])
	}

	pre, err := s.Preamble(ctx, "a", 10)
	if err != nil || !strings.Contains(pre, "agent a runbook about quota") {
		t.Fatalf("agent a preamble must include its agent-scoped entry (err %v):\n%s", err, pre)
	}

	other, err := s.Recall(ctx, "quota", "b", 10)
	if err != nil || len(other) != 0 {
		t.Fatalf("agent b must not recall agent a's entry, got %d (err %v)", len(other), err)
	}
	pre, err = s.Preamble(ctx, "b", 10)
	if err != nil || strings.Contains(pre, "quota") {
		t.Fatalf("agent b preamble must not include agent a's entry (err %v):\n%s", err, pre)
	}
	anon, err := s.Preamble(ctx, "", 10)
	if err != nil || strings.Contains(pre, "quota") || strings.Contains(anon, "quota") {
		t.Fatalf("an empty agent must not see agent-scoped entries (err %v):\n%s", err, anon)
	}
}
