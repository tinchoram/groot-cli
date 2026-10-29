package workspace

import (
	"context"
	"strings"
	"testing"

	"github.com/tinchoram/groot-cli/internal/flow"
	"github.com/tinchoram/groot-cli/internal/provider"
)

// assignFixture is teamFixture with the medium route kept by its opener, so
// a task opened by reader stays open until it is assigned. The space manager is
// space-manager, and tester is a third agent of the space.
func assignFixture(t *testing.T, steps ...provider.Step) *fixture {
	t.Helper()
	f := teamFixture(t, steps...)
	f.ws.opts.Config.Flow.Routes.Medium.Agent = ""
	f.ws.opts.Config.Work.MaxAutoTurns = 0
	if err := f.ws.SetSpaceDelegation("default", "", "space-manager"); err != nil {
		t.Fatal(err)
	}
	saveAgent(t, f, "tester", "tests things", []string{"read_file", "task_status"})
	if err := f.spaces.Assign("default", "tester"); err != nil {
		t.Fatal(err)
	}
	if err := f.ws.reload(); err != nil {
		t.Fatal(err)
	}
	return f
}

// openMediumSteps opens one medium task of reader with the given goal.
func openMediumSteps(id, goal string) []provider.Step {
	return []provider.Step{
		classifyStep("tu_"+id+"a", "medium", true, "several files"),
		toolStep("tu_"+id+"b", "task_open", map[string]string{"goal": goal, "scope": "payments", "done_when": "tests pass"}),
		text("opened " + goal),
	}
}

// taskOf reads a task of the default space from its store.
func taskOf(t *testing.T, f *fixture, id int64) flow.Task {
	t.Helper()
	sp, _ := f.spaces.Get("default")
	f.ws.mu.Lock()
	st, err := f.ws.storesFor(sp.Path)
	f.ws.mu.Unlock()
	if err != nil {
		t.Fatal(err)
	}
	tk, err := st.mem.Tasks().Get(context.Background(), id)
	if err != nil {
		t.Fatal(err)
	}
	return tk
}

// openStored saves an open medium task of agent in the default space, without a
// conversation: the agent then holds an open task of its own.
func openStored(t *testing.T, f *fixture, agent, goal string) {
	t.Helper()
	sp, _ := f.spaces.Get("default")
	f.ws.mu.Lock()
	st, err := f.ws.storesFor(sp.Path)
	f.ws.mu.Unlock()
	if err != nil {
		t.Fatal(err)
	}
	tk, err := flow.NewTask(flow.SizeMedium, goal, "payments", "tests pass", t0)
	if err != nil {
		t.Fatal(err)
	}
	tk.Space, tk.Agent = "default", agent
	if _, err := st.mem.Tasks().Open(context.Background(), tk); err != nil {
		t.Fatal(err)
	}
}

func TestManagerAssignsAnOpenTaskAndTheHandoverComesFromTheOwner(t *testing.T) {
	ctx := context.Background()
	f := assignFixture(t, openMediumSteps("1", "split billing")...)
	if _, err := f.ws.Chat().Send(ctx, "build it"); err != nil {
		t.Fatal(err)
	}

	out, err := f.ws.assignTask(ctx, "default", "space-manager", 1, "developer", "split billing first")
	if err != nil {
		t.Fatalf("assignTask: %v", err)
	}
	if !strings.Contains(out, "task #1") || !strings.Contains(out, "developer") {
		t.Fatalf("result = %q", out)
	}
	handover := onlyMessage(t, f, "developer", "task:#1 handover")
	if handover.FromAgent != "reader" {
		t.Fatalf("the handover must come from the owner, from %s/%s", handover.FromSpace, handover.FromAgent)
	}
	if !strings.Contains(handover.Body, "split billing first") || !strings.Contains(handover.Body, "handed over to you from default/reader") {
		t.Fatalf("handover body:\n%s", handover.Body)
	}
	onlyMessage(t, f, "space-manager", "task:#1 tracking")

	tk := taskOf(t, f, 1)
	if tk.Agent != "reader" || tk.Delegation.Delegate != "developer" || !tk.Delegation.Assigned || tk.Delegation.Status != flow.DelegationHandedOver {
		t.Fatalf("stored task = agent %s, delegation %+v", tk.Agent, tk.Delegation)
	}
}

func TestAssignmentRefusals(t *testing.T) {
	ctx := context.Background()
	f := assignFixture(t, openMediumSteps("1", "split billing")...)
	if _, err := f.ws.Chat().Send(ctx, "build it"); err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		name   string
		caller string
		task   int64
		to     string
		want   string
	}{
		{"a stranger cannot assign", "tester", 1, "developer", "only the owner"},
		{"an agent outside the space", "space-manager", 1, "nobody", "not assigned to space"},
		{"the recipient is the caller", "reader", 1, "reader", "to itself"},
		{"a task that does not exist", "space-manager", 99, "developer", "task #99"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, err := f.ws.assignTask(ctx, "default", c.caller, c.task, c.to, "")
			if err == nil || !strings.Contains(err.Error(), c.want) {
				t.Fatalf("error = %v, want a refusal containing %q", err, c.want)
			}
		})
	}

	openStored(t, f, "tester", "tester's own work")
	if _, err := f.ws.assignTask(ctx, "default", "space-manager", 1, "tester", ""); err == nil || !strings.Contains(err.Error(), "holds open task") {
		t.Fatalf("a recipient with an open task must be refused, got %v", err)
	}
	if _, err := f.ws.assignTask(ctx, "default", "space-manager", 1, "developer", ""); err != nil {
		t.Fatal(err)
	}
	if _, err := f.ws.assignTask(ctx, "default", "space-manager", 1, "tester", ""); err == nil || !strings.Contains(err.Error(), "is with developer") {
		t.Fatalf("a task in flight must not be reassigned, got %v", err)
	}
}

func TestAnAssignedTaskReleasesTheOwnersSlotForItsNextTask(t *testing.T) {
	ctx := context.Background()
	steps := append(openMediumSteps("1", "split billing"), openMediumSteps("2", "reports")...)
	f := assignFixture(t, steps...)
	conv := f.ws.Chat()
	if _, err := conv.Send(ctx, "build it"); err != nil {
		t.Fatal(err)
	}
	if _, err := f.ws.assignTask(ctx, "default", "space-manager", 1, "developer", ""); err != nil {
		t.Fatal(err)
	}
	if _, ok := conv.Task(); ok {
		t.Fatal("the owner's open slot must be released while the assignment runs")
	}

	if _, err := conv.Send(ctx, "next"); err != nil {
		t.Fatal(err)
	}
	next, ok := conv.Task()
	if !ok || next.ID != 2 {
		t.Fatalf("the owner's next task = %+v, %v", next, ok)
	}
	if assigned := taskOf(t, f, 1); assigned.Delegation.Delegate != "developer" {
		t.Fatalf("the assignment must still be recorded, got %+v", assigned.Delegation)
	}
}

func TestSeveralAssignedTasksRunAtOnce(t *testing.T) {
	ctx := context.Background()
	steps := append(openMediumSteps("1", "split billing"), openMediumSteps("2", "reports")...)
	f := assignFixture(t, steps...)
	conv := f.ws.Chat()
	if _, err := conv.Send(ctx, "build it"); err != nil {
		t.Fatal(err)
	}
	if _, err := f.ws.assignTask(ctx, "default", "space-manager", 1, "developer", ""); err != nil {
		t.Fatal(err)
	}
	if _, err := conv.Send(ctx, "next"); err != nil {
		t.Fatal(err)
	}
	if _, err := f.ws.assignTask(ctx, "default", "space-manager", 2, "tester", ""); err != nil {
		t.Fatalf("the second task goes to another agent while the first is in flight: %v", err)
	}
	first, second := taskOf(t, f, 1), taskOf(t, f, 2)
	if first.Delegation.Delegate != "developer" || second.Delegation.Delegate != "tester" ||
		!first.Delegation.Active() || !second.Delegation.Active() {
		t.Fatalf("both tasks must be in flight: %+v and %+v", first.Delegation, second.Delegation)
	}
}

func TestDeliveredAssignmentReportsToTheOwnersTaskByID(t *testing.T) {
	ctx := context.Background()
	f := assignFixture(t, openMediumSteps("1", "split billing")...)
	if _, err := f.ws.Chat().Send(ctx, "build it"); err != nil {
		t.Fatal(err)
	}
	if _, err := f.ws.assignTask(ctx, "default", "space-manager", 1, "developer", ""); err != nil {
		t.Fatal(err)
	}
	if _, _, err := f.ws.messengerFor("default", "developer").Send(ctx, "reader", "task:#1 delivered", "tests green"); err != nil {
		t.Fatal(err)
	}
	tk := taskOf(t, f, 1)
	if tk.Phase != flow.PhaseVerify || tk.Delegation.Status != flow.DelegationDelivered {
		t.Fatalf("the report must reach the owner's task: phase %s, status %s", tk.Phase, tk.Delegation.Status)
	}
	onlyMessage(t, f, "space-manager", "task:#1 delivered")
}
