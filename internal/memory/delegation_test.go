package memory

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"
	"time"

	"github.com/tinchoram/groot-cli/internal/flow"
)

var delegatedColumns = []string{
	"delegate", "manager", "delegation_status", "delegated_at", "delegation_at",
	"parent_space", "parent_agent", "parent_task",
}

func TestDelegationAndParentPersistAcrossReopen(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "groot.db")
	s, err := OpenSQLite(path)
	if err != nil {
		t.Fatal(err)
	}
	st := s.Tasks()
	tk := newTask("payments", "developer", base)
	tk.Parent = flow.Parent{Space: "payments", Agent: "reviewer", Task: 12}
	saved, err := st.Open(ctx, tk)
	if err != nil {
		t.Fatal(err)
	}
	origin := newTask("payments", "reviewer", base)
	origin, err = st.Open(ctx, origin)
	if err != nil {
		t.Fatal(err)
	}
	origin.Delegation = flow.Delegation{
		Delegate: "developer", Manager: "project-manager", Status: flow.DelegationHandedOver,
		HandedAt: base.Add(time.Minute), StatusAt: base.Add(time.Minute),
	}
	origin.UpdatedAt = base.Add(time.Minute)
	if err := st.Update(ctx, origin); err != nil {
		t.Fatal(err)
	}
	_ = s.Close()

	again, err := OpenSQLite(path)
	if err != nil {
		t.Fatal(err)
	}
	defer again.Close()
	got, err := again.Tasks().Get(ctx, origin.ID)
	if err != nil {
		t.Fatal(err)
	}
	d := got.Delegation
	if d.Delegate != "developer" || d.Manager != "project-manager" || d.Status != flow.DelegationHandedOver ||
		!d.HandedAt.Equal(base.Add(time.Minute)) {
		t.Fatalf("delegation record after reopen = %+v", d)
	}
	child, err := again.Tasks().Get(ctx, saved.ID)
	if err != nil {
		t.Fatal(err)
	}
	if child.Parent != (flow.Parent{Space: "payments", Agent: "reviewer", Task: 12}) {
		t.Fatalf("parent after reopen = %+v", child.Parent)
	}
	if child.Delegation.Status != "" {
		t.Fatalf("a task without a handover has no delegation: %+v", child.Delegation)
	}

	sums, err := again.TaskSummaries(ctx, false, base)
	if err != nil {
		t.Fatal(err)
	}
	var sawOrigin, sawChild bool
	for _, sum := range sums {
		if sum.ID == origin.ID && sum.Delegation.Delegate == "developer" {
			sawOrigin = true
		}
		if sum.ID == saved.ID && sum.Parent.Task == 12 {
			sawChild = true
		}
	}
	if !sawOrigin || !sawChild {
		t.Fatalf("summaries lack the delegation or the parent: %+v", sums)
	}
}

func TestDelegationColumnsMigrateOnOlderDatabases(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "groot.db")
	s, err := OpenSQLite(path)
	if err != nil {
		t.Fatal(err)
	}
	saved, err := s.Tasks().Open(ctx, newTask("payments", "reviewer", base))
	if err != nil {
		t.Fatal(err)
	}
	_ = s.Close()

	// Simulate a database written before the delegation columns existed.
	raw, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	for _, col := range delegatedColumns {
		if _, err := raw.Exec(`ALTER TABLE tasks DROP COLUMN ` + col); err != nil {
			t.Fatalf("drop %s: %v", col, err)
		}
	}
	_ = raw.Close()

	again, err := OpenSQLite(path)
	if err != nil {
		t.Fatalf("reopen an older database: %v", err)
	}
	defer again.Close()
	got, err := again.Tasks().Get(ctx, saved.ID)
	if err != nil || got.Goal != "ship the change" {
		t.Fatalf("old task after migration: %+v, %v", got, err)
	}
	got.Delegation = flow.Delegation{Delegate: "developer", Status: flow.DelegationHandedOver, HandedAt: base}
	if err := again.Tasks().Update(ctx, got); err != nil {
		t.Fatalf("write a delegation after migration: %v", err)
	}
	back, err := again.Tasks().Get(ctx, saved.ID)
	if err != nil || back.Delegation.Delegate != "developer" {
		t.Fatalf("delegation after migration = %+v, %v", back.Delegation, err)
	}
}
