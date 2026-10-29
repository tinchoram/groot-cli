package workspace

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/tinchoram/groot-cli/internal/api"
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
	// The handover opens the delegate; these tests read the origin only.
	f.ws.opts.Config.Work.MaxAutoTurns = 0
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
	// The handover opens the delegate; these tests read the origin only.
	f.ws.opts.Config.Work.MaxAutoTurns = 0
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
	// The handover opens the delegate; these tests read the origin only.
	f.ws.opts.Config.Work.MaxAutoTurns = 0
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

// assignStep is a task_assign call; toolStep cannot carry the task number, which
// is a JSON integer.
func assignStep(id string, task int64, agent, note string) provider.Step {
	in, _ := json.Marshal(map[string]any{"task": task, "agent": agent, "note": note})
	return provider.Step{Response: api.Response{
		Content:    []api.Block{{Type: api.BlockToolUse, ID: id, Name: "task_assign", Input: in}},
		StopReason: api.StopToolUse,
	}}
}

// TestOperatorAskAssignsTwoTasksAndTheAnswerReturnsToTheOrigin runs the whole
// loop: the operator asks the manager from the reader's screen, the manager opens
// two tasks and assigns one to each agent, both deliver, and the manager's answer
// comes back to the reader's screen. The turns run one after another: the mock
// provider is shared, so the script is consumed in order.
func TestOperatorAskAssignsTwoTasksAndTheAnswerReturnsToTheOrigin(t *testing.T) {
	ctx := context.Background()
	scope := map[string]string{"scope": "payments", "done_when": "tests pass"}
	open := func(id, goal string) provider.Step {
		in := map[string]string{"goal": goal, "scope": scope["scope"], "done_when": scope["done_when"]}
		return toolStep(id, "task_open", in)
	}
	f := assignFixture(t,
		// The manager answers the ask: open #1, assign it, open #2, assign it.
		classifyStep("m_1", "medium", true, "two tasks"),
		open("m_2", "split billing"),
		assignStep("m_3", 1, "developer", "split billing first"),
		open("m_4", "billing reports"),
		assignStep("m_5", 2, "tester", "test the reports"),
		text("both tasks are with their agents"),
		// The developer takes #1: open its own task, check, deliver.
		classifyStep("d_1", "medium", true, "handed over"),
		open("d_2", "split billing"),
		toolStep("d_3", "bash", map[string]string{"command": "echo ok"}),
		toolStep("d_4", "task_advance", map[string]string{"phase": "verify"}),
		toolStep("d_5", "task_advance", map[string]string{"phase": "deliver", "evidence": "billing split, tests pass"}),
		text("developer delivered"),
		// The tester takes #2 the same way.
		classifyStep("t_1", "medium", true, "handed over"),
		open("t_2", "billing reports"),
		toolStep("t_3", "bash", map[string]string{"command": "echo ok"}),
		toolStep("t_4", "task_advance", map[string]string{"phase": "verify"}),
		toolStep("t_5", "task_advance", map[string]string{"phase": "deliver", "evidence": "reports tested"}),
		text("tester delivered"),
	)
	// Each turn below is explicit, so no automatic turn may start behind the script.
	f.ws.opts.Config.Work.MaxAutoTurns = 0
	f.ws.opts.Config.Flow.Routes.Medium.Agent = ""
	if err := f.ws.reload(); err != nil {
		t.Fatal(err)
	}
	saveAgent(t, f, "space-manager", "tracks work", []string{"read_inbox", "send_message", "list_tasks", "task_classify", "task_open", "task_advance", "task_status", "task_assign"})
	saveAgent(t, f, "tester", "tests things", []string{"read_file", "bash", "task_classify", "task_open", "task_advance", "task_status"})
	if err := f.ws.reload(); err != nil {
		t.Fatal(err)
	}

	answers := make(chan answered, 4)
	f.ws.opts.Hooks.OnAsk = func(origin, key, text string, err error) {
		answers <- answered{origin, key, text, err}
	}
	if err := f.ws.SelectAgent("reader"); err != nil {
		t.Fatal(err)
	}
	if err := f.ws.Ask("default/reader", "default/space-manager", "which tasks are open?"); err != nil {
		t.Fatal(err)
	}
	if err := f.ws.SelectAgent("space-manager"); err != nil {
		t.Fatal(err)
	}
	if _, err := f.ws.Chat().Send(ctx, "answer the operator"); err != nil {
		t.Fatal(err)
	}
	got := awaitAnswer(t, answers)
	if got.origin != "default/reader" || got.key != "default/space-manager" || got.text != "both tasks are with their agents" {
		t.Fatalf("the answer must return to the operator's origin, got %+v", got)
	}
	if last := f.mock.Calls[0].Messages[len(f.mock.Calls[0].Messages)-1].Content[0].Text; !strings.Contains(last, "which tasks are open?") {
		t.Fatalf("the manager must read the ask:\n%s", last)
	}

	for _, agent := range []string{"developer", "tester"} {
		if err := f.ws.SelectAgent(agent); err != nil {
			t.Fatal(err)
		}
		if _, err := f.ws.Chat().Send(ctx, "go"); err != nil {
			t.Fatal(err)
		}
	}

	first, second := taskOf(t, f, 1), taskOf(t, f, 2)
	if first.Delegation.Delegate != "developer" || first.Delegation.Status != flow.DelegationDelivered || first.Phase != flow.PhaseVerify {
		t.Fatalf("manager's task #1 after the loop: phase %s, delegation %+v", first.Phase, first.Delegation)
	}
	if second.Delegation.Delegate != "tester" || second.Delegation.Status != flow.DelegationDelivered || second.Phase != flow.PhaseVerify {
		t.Fatalf("manager's task #2 after the loop: phase %s, delegation %+v", second.Phase, second.Delegation)
	}
	if first.Agent != "space-manager" || second.Agent != "space-manager" {
		t.Fatalf("ownership must stay with the manager: %s and %s", first.Agent, second.Agent)
	}
	var accepted bool
	for _, ev := range first.Evidence {
		accepted = accepted || strings.Contains(ev.Text, "accepted by developer")
	}
	if !accepted {
		t.Fatalf("task #1 must record the acceptance: %+v", first.Evidence)
	}
	onlyMessage(t, f, "developer", "task:#1 handover")
	onlyMessage(t, f, "tester", "task:#2 handover")
	onlyMessage(t, f, "space-manager", "task:#2 delivered")
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
