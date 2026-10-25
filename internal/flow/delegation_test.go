package flow

import (
	"context"
	"strings"
	"testing"
	"time"
)

// delegationRig builds a controller whose medium and large routes name the
// developer agent. canDelegate says whether the developer is assigned.
func delegationRig(t *testing.T, mode DelegationMode, canDelegate bool, manager string) (*Controller, *fakeStore, *[]Route) {
	t.Helper()
	store := &fakeStore{}
	clk := &clock{now: t0}
	var applied []Route
	ctl, err := NewController(context.Background(), Options{
		Space: "payments", Agent: "reviewer", Store: store, Mode: ModeStrict,
		Routes: Routes{
			Small:  Route{Model: "small-model"},
			Medium: Route{Model: "medium-model", Agent: "developer"},
			Large:  Route{Model: "large-model", Agent: "developer"},
		},
		Now:         clk.tick,
		Delegation:  mode,
		Manager:     manager,
		CanDelegate: func(string) bool { return canDelegate },
		ApplyRoute: func(r Route) func() {
			applied = append(applied, r)
			return func() {}
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	return ctl, store, &applied
}

func TestAutoDelegationHandsTheTaskOver(t *testing.T) {
	ctx := context.Background()
	ctl, store, applied := delegationRig(t, DelegationAuto, true, "project-manager")
	_, _ = ctl.Classify(SizeMedium, true, "several files")

	tk, op, err := ctl.Open(ctx, "build it", "payments/api", "tests pass")
	if err != nil {
		t.Fatal(err)
	}
	if op.Handover == nil || op.Handover.Delegate != "developer" || op.Handover.Manager != "project-manager" {
		t.Fatalf("auto mode must hand the task over, got %+v", op)
	}
	if op.Hint != "" {
		t.Fatalf("a handover is not a hint: %q", op.Hint)
	}
	if tk.Delegation.Status != DelegationHandedOver || tk.Delegation.Delegate != "developer" || tk.Delegation.HandedAt.IsZero() {
		t.Fatalf("delegation record = %+v", tk.Delegation)
	}
	if got := store.tasks[0].Delegation.Status; got != DelegationHandedOver {
		t.Fatalf("the record must be saved, store has %q", got)
	}
	if len(*applied) != 0 {
		t.Fatalf("the origin does not run the route model when it hands over, applied %v", *applied)
	}
	if tk.Phase != PhaseImplement {
		t.Fatalf("the origin keeps its phase, got %s", tk.Phase)
	}
}

func TestHintDelegationOnlyNamesTheAgent(t *testing.T) {
	ctx := context.Background()
	ctl, _, _ := delegationRig(t, DelegationHint, true, "project-manager")
	_, _ = ctl.Classify(SizeMedium, true, "several files")

	tk, op, err := ctl.Open(ctx, "build it", "payments/api", "tests pass")
	if err != nil {
		t.Fatal(err)
	}
	if op.Handover != nil {
		t.Fatalf("hint mode must not hand over: %+v", op.Handover)
	}
	if !strings.Contains(op.Hint, "developer") || !strings.Contains(op.Hint, "send_message") {
		t.Fatalf("hint = %q", op.Hint)
	}
	if tk.Delegation.Status != "" {
		t.Fatalf("hint mode keeps no delegation record: %+v", tk.Delegation)
	}
}

func TestOffDelegationDoesNothing(t *testing.T) {
	ctx := context.Background()
	ctl, _, _ := delegationRig(t, DelegationOff, true, "project-manager")
	_, _ = ctl.Classify(SizeMedium, true, "several files")

	tk, op, err := ctl.Open(ctx, "build it", "payments/api", "tests pass")
	if err != nil {
		t.Fatal(err)
	}
	if op.Handover != nil || op.Hint != "" {
		t.Fatalf("off must neither hand over nor hint: %+v", op)
	}
	if tk.Delegation.Status != "" || len(tk.Evidence) != 0 {
		t.Fatalf("off leaves no trace on the task: %+v", tk)
	}
}

func TestAutoFallsBackToTheHintWhenTheAgentIsNotAssigned(t *testing.T) {
	ctx := context.Background()
	ctl, _, _ := delegationRig(t, DelegationAuto, false, "")
	_, _ = ctl.Classify(SizeMedium, true, "several files")

	_, op, err := ctl.Open(ctx, "build it", "payments/api", "tests pass")
	if err != nil {
		t.Fatal(err)
	}
	if op.Handover != nil {
		t.Fatalf("an unassigned agent cannot take a handover: %+v", op.Handover)
	}
	if !strings.Contains(op.Hint, "developer") {
		t.Fatalf("hint = %q", op.Hint)
	}
}

func TestDelegatedOriginCannotWriteWhileHandedOver(t *testing.T) {
	ctx := context.Background()

	// Without a handover a large task is still classified and writes are refused.
	hinted, _, _ := delegationRig(t, DelegationHint, true, "")
	_, _ = hinted.Classify(SizeLarge, false, "design")
	_, _, _ = hinted.Open(ctx, "build it", "s", "d")
	if deny, _, _ := hinted.Check(ctx, "write_file", `{"path":"x.go"}`, true); !deny {
		t.Fatal("a hinted large task must still refuse writes before its plan")
	}

	ctl, _, _ := delegationRig(t, DelegationAuto, true, "")
	_, _ = ctl.Classify(SizeLarge, false, "design")
	_, op, err := ctl.Open(ctx, "build it", "s", "d")
	if err != nil || op.Handover == nil {
		t.Fatalf("Open = %+v, %v", op, err)
	}
	deny, reason, _ := ctl.Check(ctx, "write_file", `{"path":"x.go"}`, true)
	if !deny {
		t.Fatal("a delegated origin must not implement the task itself")
	}
	if !strings.Contains(reason, "handed over to developer") || !strings.Contains(reason, "task:#1 delivered") {
		t.Fatalf("the reason names the delegate and the report to wait for, got %q", reason)
	}
	// Reads and messaging still pass, so the origin can answer and wait.
	if deny, _, _ := ctl.Check(ctx, "read_file", `{"path":"x.go"}`, false); deny {
		t.Fatal("reads pass while handed over")
	}
	if deny, _, _ := ctl.Check(ctx, "send_message", `{}`, true); deny {
		t.Fatal("send_message passes while handed over")
	}
}

func TestDelegatedOriginCannotAdvanceItself(t *testing.T) {
	ctx := context.Background()
	ctl, _, _ := delegationRig(t, DelegationAuto, true, "")
	_, _ = ctl.Classify(SizeMedium, true, "several files")
	if _, _, err := ctl.Open(ctx, "build it", "s", "d"); err != nil {
		t.Fatal(err)
	}
	if _, err := ctl.Advance(ctx, PhaseVerify, ""); err == nil {
		t.Fatal("a delegated task must not move to verify by itself")
	}
	if _, err := ctl.Fail(ctx, "no longer needed"); err != nil {
		t.Fatalf("failing a delegated task stays possible: %v", err)
	}
}

func TestExpectedParentIsAttachedToTheDelegateTask(t *testing.T) {
	ctx := context.Background()
	ctl, store, _ := delegationRig(t, DelegationAuto, true, "")
	ctl.Expect(Parent{Space: "payments", Agent: "reviewer", Task: 12})
	_, _ = ctl.Classify(SizeMedium, true, "handed over")

	tk, _, err := ctl.Open(ctx, "build it", "s", "d")
	if err != nil {
		t.Fatal(err)
	}
	if tk.Parent != (Parent{Space: "payments", Agent: "reviewer", Task: 12}) {
		t.Fatalf("parent = %+v", tk.Parent)
	}
	if store.tasks[0].Parent.Task != 12 {
		t.Fatal("the parent must be saved with the task")
	}
	if !strings.Contains(ctl.Status(ctx), "from payments/reviewer task #12") {
		t.Fatalf("status lacks the parent:\n%s", ctl.Status(ctx))
	}
}

func TestDelegateDoesNotHandBackToItsOrigin(t *testing.T) {
	ctx := context.Background()
	ctl, _, _ := delegationRig(t, DelegationAuto, true, "")
	// The rig routes medium work to developer; the task came from developer.
	ctl.Expect(Parent{Space: "payments", Agent: "developer", Task: 3})
	_, _ = ctl.Classify(SizeMedium, true, "handed over")

	_, op, err := ctl.Open(ctx, "build it", "s", "d")
	if err != nil {
		t.Fatal(err)
	}
	if op.Handover != nil {
		t.Fatalf("a task must not bounce back to the agent it came from: %+v", op)
	}
}

func TestStatusShowsTheDelegation(t *testing.T) {
	ctx := context.Background()
	ctl, _, _ := delegationRig(t, DelegationAuto, true, "project-manager")
	_, _ = ctl.Classify(SizeMedium, true, "several files")
	if _, _, err := ctl.Open(ctx, "build it", "s", "d"); err != nil {
		t.Fatal(err)
	}
	status := ctl.Status(ctx)
	if !strings.Contains(status, "delegation: → developer (handed over") || !strings.Contains(status, "manager project-manager") {
		t.Fatalf("status lacks the delegation line:\n%s", status)
	}
	if block := ctl.ContextBlock(ctx); !strings.Contains(block, "delegated to developer") {
		t.Fatalf("context block lacks the delegation: %q", block)
	}
}

func TestHandoverBodyCarriesTheContract(t *testing.T) {
	tk := Task{ID: 7, Size: SizeLarge, Goal: "split the billing module", Scope: "payments/billing",
		DoneWhen: "tests pass", PlanPath: "docs/tasks/billing.md", Agent: "reviewer", Space: "payments"}
	body := HandoverBody(tk, "payments/reviewer")
	for _, want := range []string{
		"split the billing module", "tests pass", "docs/tasks/billing.md",
		"payments/reviewer", "task:#7 delivered", "task:#7 returned",
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("handover body lacks %q:\n%s", want, body)
		}
	}
}

func TestSubjectParsingIsTolerant(t *testing.T) {
	cases := []struct {
		in   string
		ok   bool
		want Subject
	}{
		{"task:#12 handover", true, Subject{Signal: SignalHandover, Task: 12}},
		{"  Task:#7 delivered ", true, Subject{Signal: SignalDelivered, Task: 7}},
		{"Re: task:#3 delivered", true, Subject{Signal: SignalDelivered, Task: 3}},
		{"task:#4 returned no time left", true, Subject{Signal: SignalReturned, Task: 4, Rest: "no time left"}},
		{"task:#9 tracking", true, Subject{Signal: SignalTracking, Task: 9}},
		{"task:#x handover", false, Subject{}},
		{"fix login, see task:#3 delivered", false, Subject{}},
		{"task:#3 unknown", false, Subject{}},
		{"", false, Subject{}},
	}
	for _, c := range cases {
		got, ok := ParseSubject(c.in)
		if ok != c.ok || (ok && got != c.want) {
			t.Errorf("ParseSubject(%q) = %+v, %v; want %+v, %v", c.in, got, ok, c.want, c.ok)
		}
	}
}

// handedOver opens a medium task that the origin has handed over.
func handedOver(t *testing.T) (*Controller, *fakeStore) {
	t.Helper()
	ctl, store, _ := delegationRig(t, DelegationAuto, true, "")
	_, _ = ctl.Classify(SizeMedium, true, "several files")
	if _, op, err := ctl.Open(context.Background(), "build it", "s", "d"); err != nil || op.Handover == nil {
		t.Fatalf("Open = %+v, %v", op, err)
	}
	return ctl, store
}

func TestDeliveredReportMovesADelegatedTaskToVerify(t *testing.T) {
	ctl, store := handedOver(t)
	tk, _ := ctl.Active()
	next, ev, err := ApplyReport(tk, Subject{Signal: SignalDelivered, Task: tk.ID}, "tests green, two files", time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if next.Phase != PhaseVerify || next.Delegation.Status != DelegationDelivered {
		t.Fatalf("after delivery: phase %s, status %s", next.Phase, next.Delegation.Status)
	}
	if ev.Kind != KindNote || ev.Text != "tests green, two files" {
		t.Fatalf("the report is evidence: %+v", ev)
	}
	if err := store.Update(context.Background(), next); err != nil {
		t.Fatal(err)
	}
	if err := ctl.Refresh(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got, _ := ctl.Active(); got.Phase != PhaseVerify {
		t.Fatalf("the live controller must see the report, phase %s", got.Phase)
	}
}

func TestReturnedReportSendsTheTaskBackToImplement(t *testing.T) {
	ctl, _ := handedOver(t)
	tk, _ := ctl.Active()
	next, ev, err := ApplyReport(tk, Subject{Signal: SignalReturned, Task: tk.ID, Rest: "no time left"}, "", time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if next.Phase != PhaseImplement || next.Delegation.Status != DelegationReturned {
		t.Fatalf("after return: phase %s, status %s", next.Phase, next.Delegation.Status)
	}
	if ev.Phase != PhaseImplement || !strings.Contains(ev.Text, "no time left") {
		t.Fatalf("the reason is evidence: %+v", ev)
	}
	if next.Delegation.Active() {
		t.Fatal("a returned task is no longer exempt from the gate")
	}
}

func TestASecondReportDoesNotMoveTheTaskAgain(t *testing.T) {
	ctl, _ := handedOver(t)
	tk, _ := ctl.Active()
	delivered, _, err := ApplyReport(tk, Subject{Signal: SignalDelivered, Task: tk.ID}, "done", time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := ApplyReport(delivered, Subject{Signal: SignalDelivered, Task: tk.ID}, "again", time.Now()); err == nil {
		t.Fatal("a delivered task is no longer waiting on the delegate")
	}
	if _, _, err := ApplyReport(delivered, Subject{Signal: SignalReturned, Task: tk.ID}, "late", time.Now()); err == nil {
		t.Fatal("a late return must not move a delivered task back")
	}
}

func TestReportNeedsADelegatedOpenTask(t *testing.T) {
	ctl, _, _ := delegationRig(t, DelegationOff, true, "")
	_, _ = ctl.Classify(SizeMedium, true, "several files")
	tk, _, _ := ctl.Open(context.Background(), "build it", "s", "d")
	if _, _, err := ApplyReport(tk, Subject{Signal: SignalDelivered, Task: tk.ID}, "done", time.Now()); err == nil {
		t.Fatal("a task that was not handed over takes no delivery report")
	}
	closed := tk
	closed.Phase = PhaseDone
	if _, _, err := ApplyReport(closed, Subject{Signal: SignalReturned, Task: tk.ID}, "", time.Now()); err == nil {
		t.Fatal("a closed task takes no report")
	}
}
