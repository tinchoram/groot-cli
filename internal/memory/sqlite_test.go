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

	_, err := s.Remember(ctx, Entry{Kind: KindRunbook, Scope: ScopeAgent, Agent: "sre", Text: "Restart the ingress controller with kubectl rollout restart", Source: "test"})
	if err != nil {
		t.Fatalf("Remember: %v", err)
	}

	got, err := s.Recall(ctx, Reader{Agent: "sre"}, "ingress", 5)
	if err != nil {
		t.Fatalf("Recall: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("want 1 match, got %d", len(got))
	}
	if got[0].Kind != KindRunbook || got[0].Scope != ScopeAgent || got[0].Agent != "sre" {
		t.Fatalf("fields not persisted: %+v", got[0])
	}
	if got[0].ID == 0 || got[0].CreatedAt.IsZero() || got[0].State != StateActive {
		t.Fatalf("id/created_at/state not set: %+v", got[0])
	}
}

func TestRecallMatchesWordsAndSanitizesQuery(t *testing.T) {
	ctx := context.Background()
	s := openTemp(t)
	_, _ = s.Remember(ctx, Entry{Kind: KindFact, Scope: ScopeSpace, Text: "The payments cluster runs in eu-west-1"})
	_, _ = s.Remember(ctx, Entry{Kind: KindFact, Scope: ScopeSpace, Text: "Grafana dashboards live in the monitoring namespace"})

	got, err := s.Recall(ctx, Reader{}, "payments", 10)
	if err != nil || len(got) != 1 {
		t.Fatalf("want 1 payments match, got %d (err %v)", len(got), err)
	}
	// FTS syntax characters must not break the query.
	if _, err := s.Recall(ctx, Reader{}, `"unbalanced (quote AND OR * :`, 10); err != nil {
		t.Fatalf("sanitized query failed: %v", err)
	}
	empty, err := s.Recall(ctx, Reader{}, "   ", 10)
	if err != nil || len(empty) != 0 {
		t.Fatalf("blank query should return nothing, got %d (err %v)", len(empty), err)
	}
}

func TestRememberRejectsInvalidKindAndScope(t *testing.T) {
	ctx := context.Background()
	s := openTemp(t)
	if _, err := s.Remember(ctx, Entry{Kind: "gossip", Text: "x"}); err == nil {
		t.Fatal("expected invalid kind to fail")
	}
	if _, err := s.Remember(ctx, Entry{Kind: KindFact, Scope: "project", Text: "x"}); err == nil {
		t.Fatal("the retired project scope must be rejected")
	}
	if _, err := s.Remember(ctx, Entry{Kind: KindFact, Scope: ScopeAgent, Text: "x"}); err == nil {
		t.Fatal("agent scope without an agent must be rejected")
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

func TestStartSessionStoresTheParent(t *testing.T) {
	ctx := context.Background()
	s := openTemp(t)
	if err := s.StartSession(ctx, Session{ID: "root", Agent: "sre", StartedAt: base}); err != nil {
		t.Fatal(err)
	}
	if err := s.StartSession(ctx, Session{ID: "child", ParentID: "root", Agent: "sre", StartedAt: base.Add(time.Minute)}); err != nil {
		t.Fatal(err)
	}
	var parent sql.NullString
	if err := s.db.QueryRow(`SELECT parent_id FROM sessions WHERE id = ?`, "child").Scan(&parent); err != nil {
		t.Fatal(err)
	}
	if !parent.Valid || parent.String != "root" {
		t.Fatalf("parent_id = %+v, want root", parent)
	}
}

func TestNoMemoryIsInert(t *testing.T) {
	ctx := context.Background()
	var m Store = NoMemory{}
	if _, err := m.Remember(ctx, Entry{Kind: KindFact, Text: "x"}); err != nil {
		t.Fatal(err)
	}
	got, err := m.Recall(ctx, Reader{}, "x", 5)
	if err != nil || len(got) != 0 {
		t.Fatalf("NoMemory recall should be empty: %v %v", got, err)
	}
	pre, err := m.Preamble(ctx, Reader{}, Budget{})
	if err != nil || len(pre) != 0 {
		t.Fatalf("NoMemory preamble should be empty: %v %v", pre, err)
	}
	if err := m.Forget(ctx, 1, Reader{}); err != ErrNotFound {
		t.Fatalf("NoMemory forget = %v, want ErrNotFound: it persists nothing", err)
	}
}

func TestAgentsOnlySeeTheirOwnAgentFacts(t *testing.T) {
	ctx := context.Background()
	s := openTemp(t)
	_, _ = s.Remember(ctx, Entry{Kind: KindFact, Scope: ScopeAgent, Agent: "reviewer", Text: "reviewer fact about payments"})
	_, _ = s.Remember(ctx, Entry{Kind: KindFact, Scope: ScopeAgent, Agent: "devjunior", Text: "devjunior fact about payments"})
	_, _ = s.Remember(ctx, Entry{Kind: KindFact, Scope: ScopeSpace, Text: "space fact about payments"})

	for _, agent := range []string{"reviewer", "devjunior"} {
		got, err := s.Recall(ctx, Reader{Agent: agent}, "payments", 10)
		if err != nil {
			t.Fatal(err)
		}
		texts := ""
		for _, e := range got {
			texts += e.Text + "|"
		}
		own, other := "reviewer fact", "devjunior fact"
		if agent == "devjunior" {
			own, other = other, own
		}
		if !strings.Contains(texts, own) || strings.Contains(texts, other) {
			t.Errorf("%s recall = %q: must see its own facts and not the other agent's", agent, texts)
		}
		if !strings.Contains(texts, "space fact") {
			t.Errorf("%s recall = %q: must see space entries", agent, texts)
		}
	}
}

func TestAgentScopedEntriesAreVisibleOnlyToTheirAgent(t *testing.T) {
	ctx := context.Background()
	s := openTemp(t)
	if _, err := s.Remember(ctx, Entry{Kind: KindRunbook, Scope: ScopeAgent, Agent: "a", Text: "agent a runbook about quota"}); err != nil {
		t.Fatalf("Remember: %v", err)
	}

	got, err := s.Recall(ctx, Reader{Agent: "a"}, "quota", 10)
	if err != nil || len(got) != 1 {
		t.Fatalf("agent a must recall its own agent-scoped entry, got %d (err %v)", len(got), err)
	}
	if got[0].Scope != ScopeAgent || got[0].Agent != "a" {
		t.Fatalf("scope/agent not persisted: %+v", got[0])
	}
	if other, err := s.Recall(ctx, Reader{Agent: "b"}, "quota", 10); err != nil || len(other) != 0 {
		t.Fatalf("agent b must not recall agent a's entry, got %d (err %v)", len(other), err)
	}
	if anon, err := s.Recall(ctx, Reader{}, "quota", 10); err != nil || len(anon) != 0 {
		t.Fatalf("an empty agent must not see agent-scoped entries, got %d (err %v)", len(anon), err)
	}
}
