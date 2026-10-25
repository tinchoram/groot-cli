package memory

import (
	"context"
	"testing"

	"github.com/tinchoram/groot-cli/internal/api"
	"github.com/tinchoram/groot-cli/internal/flow"
)

func TestCopyAgentDataCopiesOneAgentAndRemapsIDs(t *testing.T) {
	ctx := context.Background()
	src := openTempSQLite(t)
	dst := openTempSQLite(t)

	if err := src.Remember(ctx, Entry{Kind: KindFact, Scope: ScopeAgent, Agent: "developer", Text: "the developer keeps a changelog", Source: "test"}); err != nil {
		t.Fatal(err)
	}
	if err := src.Remember(ctx, Entry{Kind: KindFact, Scope: ScopeAgent, Agent: "reviewer", Text: "the reviewer reads diffs", Source: "test"}); err != nil {
		t.Fatal(err)
	}
	if err := src.StartSession(ctx, Session{ID: "s1", Agent: "developer", Model: "m", StartedAt: base}); err != nil {
		t.Fatal(err)
	}
	for i, text := range []string{"hi", "bye"} {
		if err := src.AppendMessage(ctx, "s1", i+1, userText(text), api.Usage{InputTokens: 3}); err != nil {
			t.Fatal(err)
		}
	}
	if err := src.StartSession(ctx, Session{ID: "s2", Agent: "reviewer", Model: "m", StartedAt: base}); err != nil {
		t.Fatal(err)
	}
	if err := src.AppendMessage(ctx, "s2", 1, userText("other"), api.Usage{}); err != nil {
		t.Fatal(err)
	}

	first, err := src.Tasks().Open(ctx, newTask("payments", "developer", base))
	if err != nil {
		t.Fatal(err)
	}
	for _, text := range []string{"one", "two"} {
		if err := src.Tasks().AddEvidence(ctx, first.ID, flow.Evidence{Phase: flow.PhaseClassified, Kind: flow.KindNote, Text: text, At: base}); err != nil {
			t.Fatal(err)
		}
	}
	child := newTask("payments", "developer", base)
	child.Goal = "child goal"
	child.Parent = flow.Parent{Space: "payments", Agent: "developer", Task: first.ID}
	if _, err := src.Tasks().Open(ctx, child); err != nil {
		t.Fatal(err)
	}
	if _, err := src.Tasks().Open(ctx, newTask("payments", "reviewer", base)); err != nil {
		t.Fatal(err)
	}

	// The target already has a task, so the copies get ids that differ from
	// the source ids: the parent link must follow the remap.
	if _, err := dst.Tasks().Open(ctx, newTask("billing", "other", base)); err != nil {
		t.Fatal(err)
	}

	got, err := src.CopyAgentData(ctx, dst, "developer")
	if err != nil {
		t.Fatalf("CopyAgentData: %v", err)
	}
	want := CopyCounts{Memories: 1, Sessions: 1, Messages: 2, Tasks: 2, Evidence: 2}
	if got != want {
		t.Fatalf("counts = %+v, want %+v", got, want)
	}

	if sess, ok, err := dst.LatestSession(ctx, "developer"); err != nil || !ok || sess.ID != "s1" {
		t.Fatalf("copied session = %+v ok=%v err=%v", sess, ok, err)
	}
	if msgs, err := dst.SessionMessages(ctx, "s1"); err != nil || len(msgs) != 2 {
		t.Fatalf("copied messages = %d, %v", len(msgs), err)
	}
	if _, ok, _ := dst.LatestSession(ctx, "reviewer"); ok {
		t.Fatal("another agent's session was copied")
	}
	if rev, err := dst.Tasks().List(ctx, "payments", "reviewer", 10); err != nil || len(rev) != 0 {
		t.Fatalf("another agent's tasks were copied: %+v, %v", rev, err)
	}

	copied, err := dst.Tasks().List(ctx, "payments", "developer", 10)
	if err != nil || len(copied) != 2 {
		t.Fatalf("copied tasks = %+v, %v", copied, err)
	}
	var parent, kid flow.Task
	for _, tk := range copied {
		switch tk.Goal {
		case "ship the change":
			parent = tk
		case "child goal":
			kid = tk
		}
	}
	if parent.ID == 0 || kid.ID == 0 {
		t.Fatalf("copied tasks lack the expected goals: %+v", copied)
	}
	if parent.ID == first.ID {
		t.Fatalf("the copy kept the source task id %d", first.ID)
	}
	if kid.Parent.Task != parent.ID {
		t.Fatalf("child parent = %d, want the copied parent id %d", kid.Parent.Task, parent.ID)
	}
	withEvidence, err := dst.Tasks().Get(ctx, parent.ID)
	if err != nil || len(withEvidence.Evidence) != 2 || withEvidence.Evidence[0].Text != "one" || withEvidence.Evidence[1].Text != "two" {
		t.Fatalf("copied evidence = %+v, %v", withEvidence.Evidence, err)
	}

	// The source keeps its copies.
	if old, err := src.Tasks().List(ctx, "payments", "developer", 10); err != nil || len(old) != 2 {
		t.Fatalf("source tasks after the copy = %+v, %v", old, err)
	}
	if msgs, err := src.SessionMessages(ctx, "s1"); err != nil || len(msgs) != 2 {
		t.Fatalf("source messages after the copy = %d, %v", len(msgs), err)
	}
}

func TestCopyAgentDataSkipsSessionsThatTheTargetAlreadyHas(t *testing.T) {
	ctx := context.Background()
	src := openTempSQLite(t)
	dst := openTempSQLite(t)

	if err := src.StartSession(ctx, Session{ID: "s1", Agent: "developer", Model: "m", StartedAt: base}); err != nil {
		t.Fatal(err)
	}
	if err := src.AppendMessage(ctx, "s1", 1, userText("source"), api.Usage{}); err != nil {
		t.Fatal(err)
	}
	if err := dst.StartSession(ctx, Session{ID: "s1", Agent: "other", Model: "m", StartedAt: base}); err != nil {
		t.Fatal(err)
	}
	if err := dst.AppendMessage(ctx, "s1", 1, userText("target"), api.Usage{}); err != nil {
		t.Fatal(err)
	}

	got, err := src.CopyAgentData(ctx, dst, "developer")
	if err != nil {
		t.Fatalf("CopyAgentData: %v", err)
	}
	if got.Sessions != 0 || got.Messages != 0 || got.SkippedSessions != 1 {
		t.Fatalf("counts = %+v, want the colliding session skipped", got)
	}
	if sess, ok, _ := dst.LatestSession(ctx, "other"); !ok || sess.ID != "s1" {
		t.Fatalf("the target's own session changed: %+v ok=%v", sess, ok)
	}
	if msgs, err := dst.SessionMessages(ctx, "s1"); err != nil || len(msgs) != 1 {
		t.Fatalf("target messages = %d, %v; want its own single message", len(msgs), err)
	}
}

func TestCopyAgentDataRefusesTheSameDatabase(t *testing.T) {
	src := openTempSQLite(t)
	if _, err := src.CopyAgentData(context.Background(), src, "developer"); err == nil {
		t.Fatal("copying an agent into its own database must be refused")
	}
}
