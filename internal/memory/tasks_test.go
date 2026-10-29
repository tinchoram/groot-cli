package memory

import (
	"context"
	"testing"
	"time"

	"github.com/tinchoram/groot-cli/internal/flow"
)

func newTask(space, agent string, at time.Time) flow.Task {
	return flow.Task{
		Space: space, Agent: agent, Size: flow.SizeMedium, Phase: flow.PhaseClassified,
		Goal: "ship the change", Scope: "payments/api", DoneWhen: "tests pass",
		OpenedAt: at, PhaseAt: at, UpdatedAt: at,
	}
}

func TestTaskRoundTripWithEvidence(t *testing.T) {
	ctx := context.Background()
	st := openTempSQLite(t).Tasks()
	saved, err := st.Open(ctx, newTask("payments", "developer", base))
	if err != nil || saved.ID == 0 {
		t.Fatalf("Open = %+v, %v", saved, err)
	}

	saved.Phase, saved.PhaseAt, saved.UpdatedAt = flow.PhaseImplement, base.Add(time.Minute), base.Add(time.Minute)
	if err := st.Update(ctx, saved); err != nil {
		t.Fatal(err)
	}
	if err := st.AddEvidence(ctx, saved.ID, flow.Evidence{
		Phase: flow.PhaseImplement, Kind: flow.KindCommand, Text: "$ go test\nok\nexit: 0", At: base.Add(2 * time.Minute),
	}); err != nil {
		t.Fatal(err)
	}
	if err := st.AddEvidence(ctx, saved.ID, flow.Evidence{
		Phase: flow.PhaseImplement, Kind: flow.KindNote, Text: "second", At: base.Add(3 * time.Minute),
	}); err != nil {
		t.Fatal(err)
	}

	got, err := st.Get(ctx, saved.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Phase != flow.PhaseImplement || got.Goal != "ship the change" || got.DoneWhen != "tests pass" {
		t.Fatalf("task = %+v", got)
	}
	if !got.PhaseAt.Equal(base.Add(time.Minute)) {
		t.Fatalf("phase start = %v", got.PhaseAt)
	}
	if len(got.Evidence) != 2 || got.Evidence[0].Kind != flow.KindCommand || got.Evidence[1].Text != "second" {
		t.Fatalf("evidence = %+v", got.Evidence)
	}
}

func TestActiveIsTheOpenTaskOfTheAgentOnly(t *testing.T) {
	ctx := context.Background()
	st := openTempSQLite(t).Tasks()
	mine, _ := st.Open(ctx, newTask("payments", "developer", base))
	_, _ = st.Open(ctx, newTask("payments", "reviewer", base))

	got, ok, err := st.Active(ctx, "payments", "developer")
	if err != nil || !ok || got.ID != mine.ID {
		t.Fatalf("Active = %+v, %v, %v", got, ok, err)
	}
	if _, ok, _ := st.Active(ctx, "other-space", "developer"); ok {
		t.Fatal("a task in one space is not active in another")
	}
}

func TestClosedTasksAreNotActive(t *testing.T) {
	ctx := context.Background()
	st := openTempSQLite(t).Tasks()
	saved, _ := st.Open(ctx, newTask("payments", "developer", base))
	saved.Phase = flow.PhaseFailed
	saved.ClosedAt = base.Add(time.Minute)
	if err := st.Update(ctx, saved); err != nil {
		t.Fatal(err)
	}
	if _, ok, _ := st.Active(ctx, "payments", "developer"); ok {
		t.Fatal("a failed task is closed")
	}
	closed, err := st.Get(ctx, saved.ID)
	if err != nil || closed.ClosedAt.IsZero() {
		t.Fatalf("closed task = %+v, %v", closed, err)
	}
}

func TestListNewestFirst(t *testing.T) {
	ctx := context.Background()
	st := openTempSQLite(t).Tasks()
	first, _ := st.Open(ctx, newTask("payments", "developer", base))
	second, _ := st.Open(ctx, newTask("payments", "developer", base.Add(time.Hour)))
	got, err := st.List(ctx, "payments", "developer", 10)
	if err != nil || len(got) != 2 || got[0].ID != second.ID || got[1].ID != first.ID {
		t.Fatalf("List = %+v, %v", got, err)
	}
}

func TestAnAssignmentInFlightIsNotTheOwnersOpenTask(t *testing.T) {
	ctx := context.Background()
	st := openTempSQLite(t).Tasks()
	routed, err := st.Open(ctx, newTask("payments", "developer", base))
	if err != nil {
		t.Fatal(err)
	}
	assigned, err := st.Open(ctx, newTask("payments", "developer", base.Add(time.Minute)))
	if err != nil {
		t.Fatal(err)
	}
	assigned.Delegation = flow.Delegation{Delegate: "tester", Status: flow.DelegationHandedOver, Assigned: true, HandedAt: base}
	if err := st.Update(ctx, assigned); err != nil {
		t.Fatal(err)
	}
	if got, ok, _ := st.Active(ctx, "payments", "developer"); !ok || got.ID != routed.ID {
		t.Fatalf("Active = #%d, %v; the assignment in flight must be skipped", got.ID, ok)
	}

	back, _ := st.Get(ctx, assigned.ID)
	if !back.Delegation.Assigned || back.Delegation.Status != flow.DelegationHandedOver {
		t.Fatalf("the assigned flag must round-trip, got %+v", back.Delegation)
	}
	back.Delegation.Status = flow.DelegationDelivered
	if err := st.Update(ctx, back); err != nil {
		t.Fatal(err)
	}
	if got, ok, _ := st.Active(ctx, "payments", "developer"); !ok || got.ID != assigned.ID {
		t.Fatalf("a delivered assignment is the owner's open task again, Active = #%d, %v", got.ID, ok)
	}

	routedHandover := routed
	routedHandover.Delegation = flow.Delegation{Delegate: "tester", Status: flow.DelegationHandedOver, HandedAt: base}
	if err := st.Update(ctx, routedHandover); err != nil {
		t.Fatal(err)
	}
	if got, ok, _ := st.Active(ctx, "payments", "developer"); !ok || got.ID != assigned.ID {
		t.Fatalf("a route handover is not an assignment and keeps the slot, Active = #%d, %v", got.ID, ok)
	}
}
