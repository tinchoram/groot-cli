package workspace

import (
	"context"
	"strings"
	"testing"

	"github.com/tinchoram/groot-cli/internal/catalog"
	"github.com/tinchoram/groot-cli/internal/config"
	"github.com/tinchoram/groot-cli/internal/flow"
	"github.com/tinchoram/groot-cli/internal/inbox"
	"github.com/tinchoram/groot-cli/internal/provider"
	"github.com/tinchoram/groot-cli/internal/space"
)

// saveAgent writes an agent of the catalog with the given tools.
func saveAgent(t *testing.T, f *fixture, name, description string, tools []string) {
	t.Helper()
	if err := catalog.Save(f.home, catalog.AgentDef{Name: name, Description: description, Tools: tools}, "You are "+name+"."); err != nil {
		t.Fatal(err)
	}
}

// teamFixture is a space with the origin "reader" (a task planner), the
// delegate "developer" and the "space-manager". A medium route names the
// developer, so a medium task opened by reader is handed over.
func teamFixture(t *testing.T, steps ...provider.Step) *fixture {
	t.Helper()
	f := newFixture(t, steps...)
	saveAgent(t, f, "reader", "plans work", []string{"read_file", "task_classify", "task_open", "task_advance", "task_status"})
	saveAgent(t, f, "developer", "builds things", []string{"read_file", "write_file", "bash", "task_classify", "task_open", "task_advance", "task_status"})
	saveAgent(t, f, "space-manager", "tracks work", []string{"read_inbox", "send_message", "list_tasks"})
	for _, name := range []string{"developer", "space-manager"} {
		if err := f.spaces.Assign("default", name); err != nil {
			t.Fatal(err)
		}
	}
	if err := f.ws.reload(); err != nil {
		t.Fatal(err)
	}
	f.ws.opts.Config.Flow.Routes.Medium.Agent = "developer"
	if err := f.ws.SelectAgent("reader"); err != nil {
		t.Fatal(err)
	}
	return f
}

// onlyMessage returns the first message of agent with the given subject.
func onlyMessage(t *testing.T, f *fixture, agent, subject string) inbox.Message {
	t.Helper()
	msgs, err := f.ws.inbox.List(context.Background(), "default", agent, 50)
	if err != nil {
		t.Fatal(err)
	}
	for _, m := range msgs {
		if m.Subject == subject {
			return m
		}
	}
	t.Fatalf("%s has no message %q; inbox: %+v", agent, subject, msgs)
	return inbox.Message{}
}

func classifyAndOpen(goal string) []provider.Step {
	return []provider.Step{
		classifyStep("tu_1", "medium", true, "several files"),
		toolStep("tu_2", "task_open", map[string]string{"goal": goal, "scope": "payments", "done_when": "tests pass"}),
		text("handed over"),
	}
}

func TestTaskOpenHandsOverAndSendsTheHandoverAndTracking(t *testing.T) {
	ctx := context.Background()
	f := teamFixture(t, classifyAndOpen("split billing")...)
	conv := f.ws.Chat()
	if _, err := conv.Send(ctx, "build it"); err != nil {
		t.Fatal(err)
	}

	if got := lastResult(t, f.mock.Calls[2].Messages).Content; !strings.Contains(got, "handed over to developer") ||
		!strings.Contains(got, "do not implement it yourself") {
		t.Fatalf("tool result = %q", got)
	}
	handover := onlyMessage(t, f, "developer", "task:#1 handover")
	for _, want := range []string{"split billing", "tests pass", "default/reader", "task:#1 delivered"} {
		if !strings.Contains(handover.Body, want) {
			t.Fatalf("handover body lacks %q:\n%s", want, handover.Body)
		}
	}
	if handover.FromAgent != "reader" || handover.FromSpace != "default" {
		t.Fatalf("handover sender = %s/%s", handover.FromSpace, handover.FromAgent)
	}
	onlyMessage(t, f, "space-manager", "task:#1 tracking")

	tk, ok := conv.Task()
	if !ok || tk.Phase != flow.PhaseImplement || tk.Delegation.Status != flow.DelegationHandedOver || tk.Delegation.Delegate != "developer" {
		t.Fatalf("origin task = %+v, %v", tk, ok)
	}
}

func TestHintAndOffKeepTheOldBehaviourWithoutMessages(t *testing.T) {
	for _, mode := range []string{"hint", "off"} {
		t.Run(mode, func(t *testing.T) {
			ctx := context.Background()
			f := teamFixture(t, classifyAndOpen("split billing")...)
			f.ws.opts.Config.Flow.Delegation = mode
			if _, err := f.ws.Chat().Send(ctx, "build it"); err != nil {
				t.Fatal(err)
			}
			got := lastResult(t, f.mock.Calls[2].Messages).Content
			if mode == "hint" && !strings.Contains(got, "route for medium is agent developer") {
				t.Fatalf("hint mode must name the agent: %q", got)
			}
			if mode == "off" && strings.Contains(got, "developer") {
				t.Fatalf("off mode must not mention the route agent: %q", got)
			}
			if msgs, _ := f.ws.inbox.List(ctx, "default", "developer", 10); len(msgs) != 0 {
				t.Fatalf("%s mode must not send a handover, developer has %d messages", mode, len(msgs))
			}
		})
	}
}

func TestDeliveredReportMovesTheOriginToVerify(t *testing.T) {
	ctx := context.Background()
	f := teamFixture(t, classifyAndOpen("split billing")...)
	conv := f.ws.Chat()
	if _, err := conv.Send(ctx, "build it"); err != nil {
		t.Fatal(err)
	}
	var notes []string
	f.ws.opts.Hooks.OnNotice = func(key, text string) { notes = append(notes, key+" "+text) }

	if _, _, err := f.ws.messengerFor("default", "developer").Send(ctx, "reader", "task:#1 delivered", "two files, tests pass"); err != nil {
		t.Fatal(err)
	}

	tk, _ := conv.Task()
	if tk.Phase != flow.PhaseVerify || tk.Delegation.Status != flow.DelegationDelivered {
		t.Fatalf("after the report: phase %s, status %s", tk.Phase, tk.Delegation.Status)
	}
	if last := tk.Evidence[len(tk.Evidence)-1]; last.Kind != flow.KindNote || last.Text != "two files, tests pass" {
		t.Fatalf("the report is evidence, last = %+v", last)
	}
	onlyMessage(t, f, "space-manager", "task:#1 delivered")
	if !contains(notes, "default/reader ⇠ task #1 delivered by developer") {
		t.Fatalf("the transcript must show the report, notes = %v", notes)
	}
}

func TestReturnedReportSendsTheTaskBackToImplement(t *testing.T) {
	ctx := context.Background()
	f := teamFixture(t, classifyAndOpen("split billing")...)
	conv := f.ws.Chat()
	if _, err := conv.Send(ctx, "build it"); err != nil {
		t.Fatal(err)
	}
	if _, _, err := f.ws.messengerFor("default", "developer").Send(ctx, "reader", "task:#1 returned no time left", ""); err != nil {
		t.Fatal(err)
	}
	tk, _ := conv.Task()
	if tk.Phase != flow.PhaseImplement || tk.Delegation.Status != flow.DelegationReturned {
		t.Fatalf("after return: phase %s, status %s", tk.Phase, tk.Delegation.Status)
	}
	if last := tk.Evidence[len(tk.Evidence)-1]; !strings.Contains(last.Text, "no time left") {
		t.Fatalf("the reason is evidence, last = %+v", last)
	}
	onlyMessage(t, f, "space-manager", "task:#1 returned no time left")
}

func TestDelegationLoopEndToEnd(t *testing.T) {
	ctx := context.Background()
	f := teamFixture(t,
		// reader hands the task over.
		classifyStep("tu_1", "medium", true, "several files"),
		toolStep("tu_2", "task_open", map[string]string{"goal": "split billing", "scope": "payments", "done_when": "tests pass"}),
		text("handed over"),
		// developer takes it: classify, open (linked to the origin), check, verify, deliver.
		classifyStep("tu_3", "medium", true, "handed over"),
		toolStep("tu_4", "task_open", map[string]string{"goal": "split billing", "scope": "payments", "done_when": "tests pass"}),
		toolStep("tu_5", "bash", map[string]string{"command": "echo ok"}),
		toolStep("tu_6", "task_advance", map[string]string{"phase": "verify"}),
		toolStep("tu_7", "task_advance", map[string]string{"phase": "deliver", "evidence": "two files, tests pass"}),
		text("delivered"),
	)
	// The acceptance and the report would each wake the origin for an automatic
	// turn. The script ends with the delivery, so those turns would run in the
	// background against an exhausted mock while the test reads its calls. The
	// loop under test is the tool exchange; automatic turns have their own tests.
	f.ws.opts.Config.Work.MaxAutoTurns = 0
	origin := f.ws.Chat()
	if _, err := origin.Send(ctx, "build it"); err != nil {
		t.Fatal(err)
	}

	if err := f.ws.SelectAgent("developer"); err != nil {
		t.Fatal(err)
	}
	delegate := f.ws.Chat()
	if _, err := delegate.Send(ctx, "go"); err != nil {
		t.Fatal(err)
	}

	dtk, ok := delegate.Task()
	if !ok || dtk.Phase != flow.PhaseDeliver || dtk.Parent.Task != 1 || dtk.Parent.Agent != "reader" {
		t.Fatalf("delegate task = %+v, %v", dtk, ok)
	}
	if got := lastResult(t, f.mock.Calls[5].Messages).Content; !strings.Contains(got, "task #2 opened (medium)") {
		t.Fatalf("the delegate's open was refused: %q", got)
	}

	tk, _ := origin.Task()
	if tk.Phase != flow.PhaseVerify || tk.Delegation.Status != flow.DelegationDelivered {
		t.Fatalf("origin after the loop: phase %s, status %s", tk.Phase, tk.Delegation.Status)
	}
	var accepted, delivered bool
	for _, ev := range tk.Evidence {
		accepted = accepted || strings.Contains(ev.Text, "accepted by developer")
		delivered = delivered || ev.Text == "two files, tests pass"
	}
	if !accepted || !delivered {
		t.Fatalf("origin evidence lacks the acceptance or the report: %+v", tk.Evidence)
	}
}

func TestRulesReachTheDelegateAndTheManager(t *testing.T) {
	ctx := context.Background()
	f := teamFixture(t, text("ok"), text("ok"))
	if err := f.ws.SelectAgent("developer"); err != nil {
		t.Fatal(err)
	}
	if _, err := f.ws.Chat().Send(ctx, "hi"); err != nil {
		t.Fatal(err)
	}
	if sys := f.mock.Calls[0].System; !strings.Contains(sys, "task:#N handover") || !strings.Contains(sys, "task:#N delivered") {
		t.Fatalf("the delegate rule is missing:\n%s", sys)
	}
	if err := f.ws.SelectAgent("space-manager"); err != nil {
		t.Fatal(err)
	}
	if _, err := f.ws.Chat().Send(ctx, "hi"); err != nil {
		t.Fatal(err)
	}
	if sys := f.mock.Calls[1].System; !strings.Contains(sys, "tracking") || !strings.Contains(sys, "do not implement") {
		t.Fatalf("the manager rule is missing:\n%s", sys)
	}
}

func TestResolveFlowDelegationAndManagerWithOrigins(t *testing.T) {
	assigned := space.Space{Name: "payments", Agents: []string{"reader", "space-manager"}}
	global := config.Default().Flow

	v := ResolveFlow(assigned, global)
	if v.Delegation.Value != "auto" || v.Delegation.From != SourceDefault {
		t.Fatalf("delegation = %+v", v.Delegation)
	}
	if v.Manager.Value != "space-manager" || v.Manager.From != SourceDefault {
		t.Fatalf("manager = %+v", v.Manager)
	}

	unassigned := space.Space{Name: "payments", Agents: []string{"reader"}}
	if v := ResolveFlow(unassigned, global); v.Manager.Value != "" {
		t.Fatalf("an unassigned default manager is none, got %+v", v.Manager)
	}

	own := assigned
	own.Flow.Delegation, own.Flow.Manager = "hint", "lead"
	own.Agents = []string{"reader", "lead"}
	v = ResolveFlow(own, global)
	if v.Delegation != (Setting{"hint", SourceSpace}) || v.Manager != (Setting{"lead", SourceSpace}) {
		t.Fatalf("space overrides = %+v / %+v", v.Delegation, v.Manager)
	}

	g := global
	g.Delegation = "off"
	if v := ResolveFlow(assigned, g); v.Delegation != (Setting{"off", SourceGlobal}) {
		t.Fatalf("global delegation = %+v", v.Delegation)
	}

	none := assigned
	none.Flow.Manager = "none"
	if v := ResolveFlow(none, global); v.Manager.Value != "" || v.Manager.From != SourceSpace {
		t.Fatalf("an explicit none is no manager, got %+v", v.Manager)
	}
	if v := ResolveFlow(assigned, global); v.FlowConfig().Delegation != "auto" || v.FlowConfig().Manager != "space-manager" {
		t.Fatalf("the controller reads the merged values: %+v", v.FlowConfig())
	}
}

func TestSetSpaceDelegationValidatesModeAndManager(t *testing.T) {
	f := teamFixture(t)
	if err := f.ws.SetSpaceDelegation("default", "sometimes", ""); err == nil {
		t.Fatal("an unknown mode must be refused")
	}
	if err := f.ws.SetSpaceDelegation("default", "", "ghost"); err == nil {
		t.Fatal("a manager that is not assigned must be refused")
	}
	if err := f.ws.SetSpaceDelegation("default", "hint", "developer"); err != nil {
		t.Fatal(err)
	}
	v, err := f.ws.SpaceFlow("default")
	if err != nil {
		t.Fatal(err)
	}
	if v.Delegation != (Setting{"hint", SourceSpace}) || v.Manager != (Setting{"developer", SourceSpace}) {
		t.Fatalf("saved = %+v / %+v", v.Delegation, v.Manager)
	}
	if err := f.ws.SetSpaceDelegation("default", "", "none"); err != nil {
		t.Fatal(err)
	}
	if v, _ := f.ws.SpaceFlow("default"); v.Manager.Value != "" || v.Delegation.Value != "hint" {
		t.Fatalf("an empty mode keeps the mode, none clears the manager: %+v / %+v", v.Delegation, v.Manager)
	}
}

func TestManagerWithoutListTasksGetsANote(t *testing.T) {
	f := teamFixture(t)
	saveAgent(t, f, "space-manager", "tracks work", []string{"read_inbox", "send_message"})
	if err := f.ws.reload(); err != nil {
		t.Fatal(err)
	}
	v, err := f.ws.SpaceFlow("default")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(v.ManagerNote, "list_tasks") {
		t.Fatalf("the manager note = %q", v.ManagerNote)
	}
}
