package memory

import (
	"context"
	"testing"

	"github.com/tinchoram/groot-cli/internal/flow"
)

func TestRenameAgentMovesTasksMemoriesAndSessions(t *testing.T) {
	ctx := context.Background()
	st := openTempSQLite(t)

	if _, err := st.Tasks().Open(ctx, newTask("payments", "developer", base)); err != nil {
		t.Fatal(err)
	}
	delegated := newTask("payments", "reviewer", base)
	delegated.Delegation = flow.Delegation{Delegate: "developer"}
	if _, err := st.Tasks().Open(ctx, delegated); err != nil {
		t.Fatal(err)
	}
	if err := st.Remember(ctx, Entry{Kind: KindFact, Scope: ScopeAgent, Agent: "developer", Text: "the developer keeps a changelog", Source: "test"}); err != nil {
		t.Fatal(err)
	}
	if err := st.StartSession(ctx, Session{ID: "s1", Agent: "developer", Model: "m", StartedAt: base}); err != nil {
		t.Fatal(err)
	}

	if err := st.RenameAgent(ctx, "developer", "coder"); err != nil {
		t.Fatalf("RenameAgent: %v", err)
	}

	if old, err := st.Tasks().List(ctx, "payments", "developer", 10); err != nil || len(old) != 0 {
		t.Fatalf("tasks still under the old name: %+v, %v", old, err)
	}
	moved, err := st.Tasks().List(ctx, "payments", "coder", 10)
	if err != nil || len(moved) != 1 {
		t.Fatalf("tasks under the new name = %+v, %v", moved, err)
	}
	if moved[0].Delegation.Delegate != "" {
		t.Fatal("the delegate of another agent must not change")
	}
	reviewer, _ := st.Tasks().List(ctx, "payments", "reviewer", 10)
	if len(reviewer) != 1 || reviewer[0].Delegation.Delegate != "coder" {
		t.Fatalf("delegation to the renamed agent = %+v", reviewer)
	}

	entries, err := st.PreambleEntries(ctx, "coder", 10)
	if err != nil || len(entries) != 1 || entries[0].Text != "the developer keeps a changelog" {
		t.Fatalf("memory of the renamed agent = %+v, %v", entries, err)
	}
	sess, ok, err := st.LatestSession(ctx, "coder")
	if err != nil || !ok || sess.ID != "s1" {
		t.Fatalf("session of the renamed agent = %+v ok=%v err=%v", sess, ok, err)
	}
}
