package memory

import (
	"context"
	"testing"
	"time"

	"github.com/tinchoram/groot-cli/internal/flow"
)

// openAt stores a task of agent in phase, last updated at at, with closedAt set
// when the phase is done or failed.
func openAt(t *testing.T, st flow.Store, agent string, phase flow.Phase, at time.Time) flow.Task {
	t.Helper()
	ctx := context.Background()
	saved, err := st.Open(ctx, newTask("payments", agent, at))
	if err != nil {
		t.Fatal(err)
	}
	saved.Phase, saved.PhaseAt, saved.UpdatedAt = phase, at, at
	if phase == flow.PhaseDone || phase == flow.PhaseFailed {
		saved.ClosedAt = at
	}
	if err := st.Update(ctx, saved); err != nil {
		t.Fatal(err)
	}
	return saved
}

func TestTaskSummariesListsEveryOpenPhase(t *testing.T) {
	ctx := context.Background()
	s := openTempSQLite(t)
	st := s.Tasks()
	for i, phase := range []flow.Phase{flow.PhaseClassified, flow.PhasePlanned, flow.PhaseImplement, flow.PhaseVerify, flow.PhaseDeliver} {
		openAt(t, st, "developer", phase, base.Add(time.Duration(i)*time.Minute))
	}
	openAt(t, st, "reviewer", flow.PhaseDone, base)
	openAt(t, st, "reviewer", flow.PhaseFailed, base)

	rows, err := s.TaskSummaries(ctx, false, time.Time{})
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 5 {
		t.Fatalf("open rows = %d, want the five open phases only: %+v", len(rows), rows)
	}
	for _, r := range rows {
		if r.Phase == flow.PhaseDone || r.Phase == flow.PhaseFailed {
			t.Fatalf("closed task listed without includeClosed: %+v", r)
		}
	}
}

func TestTaskSummariesCountsEvidenceAndKeepsTheGoal(t *testing.T) {
	ctx := context.Background()
	s := openTempSQLite(t)
	st := s.Tasks()
	task := openAt(t, st, "developer", flow.PhaseImplement, base)
	for i := range 3 {
		if err := st.AddEvidence(ctx, task.ID, flow.Evidence{
			Phase: flow.PhaseImplement, Kind: flow.KindNote, Text: "n", At: base.Add(time.Duration(i) * time.Second),
		}); err != nil {
			t.Fatal(err)
		}
	}

	rows, err := s.TaskSummaries(ctx, false, time.Time{})
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 {
		t.Fatalf("rows = %+v", rows)
	}
	r := rows[0]
	if r.ID != task.ID || r.Space != "payments" || r.Agent != "developer" || r.Evidence != 3 ||
		r.Goal != "ship the change" || r.Size != flow.SizeMedium || !r.OpenedAt.Equal(base) {
		t.Fatalf("summary = %+v", r)
	}
}

func TestTaskSummariesIncludesClosedTasksOnlyInsideTheWindow(t *testing.T) {
	ctx := context.Background()
	s := openTempSQLite(t)
	st := s.Tasks()
	now := base.Add(30 * 24 * time.Hour)
	since := now.Add(-7 * 24 * time.Hour)

	recent := openAt(t, st, "developer", flow.PhaseDone, now.Add(-2*24*time.Hour))
	old := openAt(t, st, "developer", flow.PhaseFailed, now.Add(-10*24*time.Hour))
	ancientOpen := openAt(t, st, "reviewer", flow.PhaseVerify, now.Add(-90*24*time.Hour))

	rows, err := s.TaskSummaries(ctx, true, since)
	if err != nil {
		t.Fatal(err)
	}
	got := map[int64]flow.Phase{}
	for _, r := range rows {
		got[r.ID] = r.Phase
	}
	if got[recent.ID] != flow.PhaseDone {
		t.Fatalf("a task closed two days ago must be listed with its final phase: %+v", rows)
	}
	if _, ok := got[old.ID]; ok {
		t.Fatalf("a task closed ten days ago must fall outside the seven-day window: %+v", rows)
	}
	if got[ancientOpen.ID] != flow.PhaseVerify {
		t.Fatalf("an open task is listed whatever its age: %+v", rows)
	}
}

func TestTaskSummariesIsOrderedBySpaceThenUpdatedNewestFirst(t *testing.T) {
	ctx := context.Background()
	s := openTempSQLite(t)
	st := s.Tasks()
	older := openAt(t, st, "developer", flow.PhaseImplement, base)
	newer := openAt(t, st, "reviewer", flow.PhaseImplement, base.Add(time.Hour))

	rows, err := s.TaskSummaries(ctx, false, time.Time{})
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 2 || rows[0].ID != newer.ID || rows[1].ID != older.ID {
		t.Fatalf("order = %+v, want the newer task first", rows)
	}
}
