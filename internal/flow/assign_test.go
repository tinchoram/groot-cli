package flow

import (
	"context"
	"strings"
	"testing"
)

// assignable is an open task of reviewer with the given phase and delegation.
func assignable(phase Phase, d Delegation) Task {
	return Task{ID: 7, Space: "payments", Agent: "reviewer", Size: SizeMedium, Phase: phase, Delegation: d}
}

// withNote records an assignment-style note on the task: no work has run.
func withNote(t Task) Task {
	t.Evidence = []Evidence{{Phase: t.Phase, Kind: KindNote, Text: "assigned to tester by space-manager"}}
	return t
}

// withEvidence records one command run on the task.
func withEvidence(t Task) Task {
	t.Evidence = []Evidence{{Phase: t.Phase, Kind: KindCommand, Text: "$ go test"}}
	return t
}

func TestAssignRefusals(t *testing.T) {
	inFlight := Delegation{Delegate: "tester", Status: DelegationHandedOver, Assigned: true}
	accepted := Delegation{Delegate: "tester", Status: DelegationAccepted, Assigned: true}
	delivered := Delegation{Delegate: "tester", Status: DelegationDelivered, Assigned: true}
	returned := Delegation{Delegate: "tester", Status: DelegationReturned, Assigned: true}
	cases := []struct {
		name    string
		task    Task
		caller  string
		manager string
		to      string
		want    string // a substring of the refusal; "" when the assignment is allowed
	}{
		{"a stranger cannot assign", assignable(PhaseClassified, Delegation{}), "developer", "space-manager", "tester", "only the owner"},
		{"no manager and a stranger", assignable(PhaseClassified, Delegation{}), "developer", "", "tester", "only the owner"},
		{"the recipient is the caller", assignable(PhaseClassified, Delegation{}), "reviewer", "space-manager", "reviewer", "to itself"},
		{"the owner is the recipient", assignable(PhaseClassified, Delegation{}), "space-manager", "space-manager", "reviewer", "already belongs"},
		{"a done task is closed", assignable(PhaseDone, Delegation{}), "reviewer", "", "tester", "is closed"},
		{"a failed task is closed", assignable(PhaseFailed, Delegation{}), "reviewer", "", "tester", "is closed"},
		{"implement with work recorded is refused", withEvidence(assignable(PhaseImplement, Delegation{})), "reviewer", "", "tester", "work recorded"},
		{"implement before any evidence is assignable", assignable(PhaseImplement, Delegation{}), "reviewer", "", "tester", ""},
		{"a note is not work", withNote(assignable(PhaseImplement, Delegation{})), "reviewer", "", "tester", ""},
		{"verify is not assignable", assignable(PhaseVerify, Delegation{}), "reviewer", "", "tester", "is in verify"},
		{"a handover in flight blocks", assignable(PhaseClassified, inFlight), "reviewer", "", "developer", "is with tester"},
		{"an accepted task blocks", assignable(PhaseClassified, accepted), "reviewer", "", "developer", "is with tester"},
		{"a delivered task waits for the owner", assignable(PhaseVerify, delivered), "reviewer", "", "developer", "is in verify"},
		{"a returned task can be reassigned", withEvidence(assignable(PhaseImplement, returned)), "reviewer", "", "developer", ""},
		{"the owner assigns a classified task", assignable(PhaseClassified, Delegation{}), "reviewer", "", "tester", ""},
		{"the manager assigns a planned task", assignable(PhasePlanned, Delegation{}), "space-manager", "space-manager", "tester", ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, err := Assign(c.task, c.caller, c.manager, c.to, t0)
			if c.want == "" {
				if err != nil {
					t.Fatalf("Assign refused: %v", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), c.want) {
				t.Fatalf("Assign error = %v, want a refusal containing %q", err, c.want)
			}
		})
	}
}

func TestAssignRecordsTheHandoverAndKeepsOwnership(t *testing.T) {
	next, err := Assign(assignable(PhaseClassified, Delegation{}), "space-manager", "space-manager", "developer", t0)
	if err != nil {
		t.Fatal(err)
	}
	if next.Agent != "reviewer" {
		t.Fatalf("ownership moved to %s", next.Agent)
	}
	d := next.Delegation
	if d.Delegate != "developer" || d.Status != DelegationHandedOver || !d.Assigned || d.HandedAt.IsZero() {
		t.Fatalf("delegation = %+v", d)
	}
	if d.Manager != "space-manager" {
		t.Fatalf("the tracking manager = %q, want space-manager", d.Manager)
	}
	if next.Phase != PhaseClassified {
		t.Fatalf("assigning moves no phase, got %s", next.Phase)
	}
}

func TestAssignNamesNoTrackingManagerForTheOriginOrTheDelegate(t *testing.T) {
	next, err := Assign(assignable(PhaseClassified, Delegation{}), "reviewer", "developer", "developer", t0)
	if err != nil {
		t.Fatal(err)
	}
	if next.Delegation.Manager != "" {
		t.Fatalf("the origin tracks nothing, manager = %q", next.Delegation.Manager)
	}
	next, err = Assign(assignable(PhaseClassified, Delegation{}), "space-manager", "space-manager", "developer", t0)
	if err != nil {
		t.Fatal(err)
	}
	if next.Delegation.Manager != "space-manager" {
		t.Fatalf("the manager tracks a handover it did not make, manager = %q", next.Delegation.Manager)
	}
}

func TestAnUnassignedOpenTaskKeepsTheSingleSlot(t *testing.T) {
	ctx := context.Background()
	ctl, _, _ := delegationRig(t, DelegationOff, false, "")
	_, _ = ctl.Classify(SizeMedium, true, "several files")
	if _, _, err := ctl.Open(ctx, "first", "s", "d"); err != nil {
		t.Fatal(err)
	}
	_, _ = ctl.Classify(SizeMedium, true, "several files")
	if _, _, err := ctl.Open(ctx, "second", "s", "d"); err == nil || !strings.Contains(err.Error(), "still open") {
		t.Fatalf("a second open must be refused while the first is open, got %v", err)
	}
}

func TestOwnerOpensTheNextTaskWhileAnAssignmentRuns(t *testing.T) {
	ctx := context.Background()
	ctl, store, _ := delegationRig(t, DelegationOff, false, "space-manager")
	_, _ = ctl.Classify(SizeMedium, true, "several files")
	first, _, err := ctl.Open(ctx, "first", "s", "d")
	if err != nil {
		t.Fatal(err)
	}
	assigned, err := Assign(first, "reviewer", "space-manager", "developer", t0)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Update(ctx, assigned); err != nil {
		t.Fatal(err)
	}
	if err := ctl.Refresh(ctx); err != nil {
		t.Fatal(err)
	}
	if _, ok := ctl.Active(); ok {
		t.Fatal("an assigned task in flight must not be the owner's open task")
	}

	_, _ = ctl.Classify(SizeMedium, true, "second")
	second, _, err := ctl.Open(ctx, "second", "s", "d")
	if err != nil {
		t.Fatalf("the owner must open its next task while the assignment runs: %v", err)
	}
	if second.ID == first.ID {
		t.Fatal("the next task must be a new task")
	}
	if got, ok, _ := store.Active(ctx, "payments", "reviewer"); !ok || got.ID != second.ID {
		t.Fatalf("the store's open task of the owner = %+v, %v; want #%d", got, ok, second.ID)
	}
}

func TestDeliveredAssignmentResumesAfterTheOwnersCurrentTask(t *testing.T) {
	ctx := context.Background()
	ctl, store, _ := delegationRig(t, DelegationOff, false, "space-manager")
	_, _ = ctl.Classify(SizeMedium, true, "several files")
	first, _, _ := ctl.Open(ctx, "first", "s", "d")
	assigned, _ := Assign(first, "reviewer", "space-manager", "developer", t0)
	_ = store.Update(ctx, assigned)
	_ = ctl.Refresh(ctx)
	_, _ = ctl.Classify(SizeMedium, true, "second")
	second, _, err := ctl.Open(ctx, "second", "s", "d")
	if err != nil {
		t.Fatal(err)
	}

	delivered, ev, err := ApplyReport(assigned, Subject{Signal: SignalDelivered, Task: first.ID}, "done", t0)
	if err != nil {
		t.Fatal(err)
	}
	_ = store.Update(ctx, delivered)
	_ = store.AddEvidence(ctx, delivered.ID, ev)
	if err := ctl.Refresh(ctx); err != nil {
		t.Fatal(err)
	}
	if got, _ := ctl.Active(); got.ID != second.ID {
		t.Fatalf("the owner keeps its current task while the report waits, active = #%d", got.ID)
	}

	if _, err := ctl.Fail(ctx, "stop"); err != nil {
		t.Fatal(err)
	}
	got, ok := ctl.Active()
	if !ok || got.ID != first.ID || got.Phase != PhaseVerify {
		t.Fatalf("after the current task closes, the delivered assignment is the open task: %+v, %v", got, ok)
	}
	_, _ = ctl.Classify(SizeMedium, true, "third")
	if _, _, err := ctl.Open(ctx, "third", "s", "d"); err == nil || !strings.Contains(err.Error(), "still open") {
		t.Fatalf("the delivered assignment is still open, a new task must wait: %v", err)
	}
}

func TestOpenRefusesTheStoredOpenTaskWhenTheControllerHasNone(t *testing.T) {
	ctx := context.Background()
	ctl, store, _ := delegationRig(t, DelegationOff, false, "")
	_, _ = ctl.Classify(SizeMedium, true, "several files")
	stored, _, _ := ctl.Open(ctx, "stored", "s", "d")
	// The store holds an open task the controller does not know about.
	_ = store.Update(ctx, Task{ID: stored.ID, Space: "payments", Agent: "reviewer", Size: SizeMedium, Phase: PhaseVerify, Delegation: Delegation{Delegate: "developer", Status: DelegationDelivered, Assigned: true}})
	ctl.active = nil
	_, _ = ctl.Classify(SizeMedium, true, "again")
	if _, _, err := ctl.Open(ctx, "again", "s", "d"); err == nil || !strings.Contains(err.Error(), "still open") {
		t.Fatalf("an open task in the store must block a new one, got %v", err)
	}
}
