package tool

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/tinchoram/groot-cli/internal/api"
	"github.com/tinchoram/groot-cli/internal/flow"
)

// The task tools drive the ODD flow of one conversation. They only record
// state and report it: the flow rules themselves are enforced by the gate
// around every write and exec call.

// TaskClassify records the size of the work at hand.
type TaskClassify struct{ ctl *flow.Controller }

// NewTaskClassify returns the task_classify tool.
func NewTaskClassify(ctl *flow.Controller) *TaskClassify { return &TaskClassify{ctl: ctl} }

type taskClassifyInput struct {
	Size   string `json:"size"`
	Reason string `json:"reason"`
}

func (*TaskClassify) Definition() api.ToolDef {
	return api.ToolDef{
		Name:        "task_classify",
		Description: "Classify the work before the first write: small (1-2 files, no design, reversible), medium (several files or one component, tests affected, no open decisions), or large (design, several components, risk, or questions the user must answer). Returns the route and what the flow will require.",
		InputSchema: json.RawMessage(`{"type":"object","properties":{"size":{"type":"string","enum":["small","medium","large"]},"reason":{"type":"string"}},"required":["size","reason"]}`),
	}
}

func (*TaskClassify) Risk() Risk            { return RiskRead }
func (t *TaskClassify) RiskFor(string) Risk { return t.Risk() }
func (*TaskClassify) Preview(input string) string {
	var in taskClassifyInput
	if err := json.Unmarshal([]byte(input), &in); err != nil {
		return "task_classify: invalid input"
	}
	return "classify as " + in.Size + ": " + in.Reason
}

func (t *TaskClassify) Execute(ctx context.Context, input string) (string, bool) {
	var in taskClassifyInput
	if err := json.Unmarshal([]byte(input), &in); err != nil {
		return "invalid input: " + err.Error(), true
	}
	size := flow.Size(strings.TrimSpace(in.Size))
	out, err := t.ctl.Classify(size, in.Reason)
	if err != nil {
		return err.Error(), true
	}
	return out, false
}

// TaskOpen opens the task for the last classification.
type TaskOpen struct {
	ctl *flow.Controller
	msg Messenger
}

// NewTaskOpen returns the task_open tool. msg sends the handover when the
// route hands the task over.
func NewTaskOpen(ctl *flow.Controller, msg Messenger) *TaskOpen { return &TaskOpen{ctl: ctl, msg: msg} }

type taskOpenInput struct {
	Goal     string `json:"goal"`
	Scope    string `json:"scope"`
	DoneWhen string `json:"done_when"`
}

func (*TaskOpen) Definition() api.ToolDef {
	return api.ToolDef{
		Name:        "task_open",
		Description: "Open the task for a medium or large classification. Say the goal, the scope and how you will know it is done. Opening applies the route's model for this conversation.",
		InputSchema: json.RawMessage(`{"type":"object","properties":{"goal":{"type":"string"},"scope":{"type":"string"},"done_when":{"type":"string"}},"required":["goal","done_when"]}`),
	}
}

// Risk is RiskWrite: opening a task switches the model and changes what the
// flow requires next.
func (*TaskOpen) Risk() Risk            { return RiskWrite }
func (t *TaskOpen) RiskFor(string) Risk { return t.Risk() }

func (*TaskOpen) Preview(input string) string {
	var in taskOpenInput
	if err := json.Unmarshal([]byte(input), &in); err != nil {
		return "task_open: invalid input"
	}
	return fmt.Sprintf("open task: %s\nscope: %s\ndone when: %s", in.Goal, in.Scope, in.DoneWhen)
}

func (t *TaskOpen) Execute(ctx context.Context, input string) (string, bool) {
	var in taskOpenInput
	if err := json.Unmarshal([]byte(input), &in); err != nil {
		return "invalid input: " + err.Error(), true
	}
	task, op, err := t.ctl.Open(ctx, in.Goal, in.Scope, in.DoneWhen)
	if err != nil {
		return err.Error(), true
	}
	out := fmt.Sprintf("task #%d opened (%s): phase %s", task.ID, task.Size, task.Phase)
	if h := op.Handover; h != nil {
		return t.handOver(ctx, out, task, *h)
	}
	if op.Hint != "" {
		out += "\n" + op.Hint
	}
	return out, false
}

// handOver sends the handover to the delegate and the tracking copy to the
// manager. The origin keeps its task open and waits for the report.
func (t *TaskOpen) handOver(ctx context.Context, out string, task flow.Task, h flow.Handover) (string, bool) {
	if t.msg == nil {
		return out + "\nhanded over to " + h.Delegate + " but no messenger is wired: the handover was not sent", true
	}
	origin := t.ctl.Address()
	if _, _, err := t.msg.Send(ctx, h.Delegate, flow.SubjectFor(flow.SignalHandover, task.ID), flow.HandoverBody(task, origin)); err != nil {
		return out + "\nhanded over to " + h.Delegate + " but the handover could not be sent: " + err.Error() + "\nfail the task with task_advance failed and a reason", true
	}
	out += "\nhanded over to " + h.Delegate + "; wait for the delivery report in your inbox, do not implement it yourself"
	if h.Manager != "" {
		body := fmt.Sprintf("Task #%d (%s) of %s is handed over to %s. Goal: %s", task.ID, task.Size, origin, h.Delegate, task.Goal)
		if _, _, err := t.msg.Send(ctx, h.Manager, flow.SubjectFor(flow.SignalTracking, task.ID), body); err != nil {
			out += "\ntracking copy to " + h.Manager + " not sent: " + err.Error()
		}
	}
	return out, false
}

// TaskAdvance moves the open task to its next phase.
type TaskAdvance struct {
	ctl *flow.Controller
	msg Messenger
}

// NewTaskAdvance returns the task_advance tool. msg reports a delivery to the
// origin of a handed-over task.
func NewTaskAdvance(ctl *flow.Controller, msg Messenger) *TaskAdvance {
	return &TaskAdvance{ctl: ctl, msg: msg}
}

type taskAdvanceInput struct {
	Phase    string `json:"phase"`
	Evidence string `json:"evidence"`
}

func (*TaskAdvance) Definition() api.ToolDef {
	return api.ToolDef{
		Name:        "task_advance",
		Description: "Move the open task to the next phase. planned needs the plan file path as evidence (large tasks). implement after a planned or a failed verify needs a reason on failure. verify needs a command run during implement. deliver needs a note with the changes and the risks. done closes the task. failed closes it with a reason.",
		InputSchema: json.RawMessage(`{"type":"object","properties":{"phase":{"type":"string","enum":["planned","implement","verify","deliver","done","failed"]},"evidence":{"type":"string"}},"required":["phase"]}`),
	}
}

func (*TaskAdvance) Risk() Risk            { return RiskRead }
func (t *TaskAdvance) RiskFor(string) Risk { return t.Risk() }

func (*TaskAdvance) Preview(input string) string {
	var in taskAdvanceInput
	if err := json.Unmarshal([]byte(input), &in); err != nil {
		return "task_advance: invalid input"
	}
	return "task → " + in.Phase + ": " + in.Evidence
}

func (t *TaskAdvance) Execute(ctx context.Context, input string) (string, bool) {
	var in taskAdvanceInput
	if err := json.Unmarshal([]byte(input), &in); err != nil {
		return "invalid input: " + err.Error(), true
	}
	before, _ := t.ctl.Active()
	next, err := t.ctl.Advance(ctx, flow.Phase(in.Phase), in.Evidence)
	if err != nil {
		return err.Error(), true
	}
	if next.Closed() {
		return fmt.Sprintf("task #%d closed as %s", before.ID, next.Phase), false
	}
	out := fmt.Sprintf("task #%d: %s → %s", next.ID, before.Phase, next.Phase)
	if next.Phase == flow.PhaseDeliver && next.Parent.Set() {
		out += t.report(ctx, next, in.Evidence)
	}
	return out, false
}

// report sends the delivery of a task that another agent handed over to its
// origin. The deliver note is the body. A failed send is reported, not hidden:
// the delivery itself already happened.
func (t *TaskAdvance) report(ctx context.Context, next flow.Task, note string) string {
	origin := next.Parent.Space + "/" + next.Parent.Agent
	subject := flow.SubjectFor(flow.SignalDelivered, next.Parent.Task)
	if t.msg == nil {
		return "\nnot reported to " + origin + ": no messenger is wired"
	}
	if _, _, err := t.msg.Send(ctx, origin, subject, note); err != nil {
		return "\nnot reported to " + origin + ": " + err.Error()
	}
	return "\nreported to " + origin + " as " + subject
}

// TaskStatus reports the classification and the open task.
type TaskStatus struct{ ctl *flow.Controller }

// NewTaskStatus returns the task_status tool.
func NewTaskStatus(ctl *flow.Controller) *TaskStatus { return &TaskStatus{ctl: ctl} }

func (*TaskStatus) Definition() api.ToolDef {
	return api.ToolDef{
		Name:        "task_status",
		Description: "Show the current classification and the open task, with its phase and evidence count.",
		InputSchema: json.RawMessage(`{"type":"object","properties":{}}`),
	}
}

func (*TaskStatus) Risk() Risk            { return RiskRead }
func (t *TaskStatus) RiskFor(string) Risk { return t.Risk() }
func (*TaskStatus) Preview(string) string { return "task_status" }

func (t *TaskStatus) Execute(ctx context.Context, _ string) (string, bool) {
	return t.ctl.Status(ctx), false
}
