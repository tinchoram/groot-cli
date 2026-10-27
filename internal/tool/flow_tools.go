package tool

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
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
	Size              string `json:"size"`
	ResumableFromDiff bool   `json:"resumable_from_diff"`
	Reason            string `json:"reason"`
}

func (*TaskClassify) Definition() api.ToolDef {
	return api.ToolDef{
		Name:        "task_classify",
		Description: "Classify the work before the first write with one test: could another agent resume it from the request plus the git diff alone? small means one or two reversible edits you fully understand, with nothing to run, so write directly. medium means yes, resumable, and command evidence is enough. large means no, so the work needs a feature document with numbered specs. It returns the route and what the flow will require next.",
		InputSchema: json.RawMessage(`{"type":"object","properties":{` +
			`"size":{"type":"string","enum":["small","medium","large"],"description":"small, medium or large, by the test in the description"},` +
			`"resumable_from_diff":{"type":"boolean","description":"True when another agent could resume from the request and the git diff alone"},` +
			`"reason":{"type":"string","description":"One sentence on why this size was chosen"}},` +
			`"required":["size","resumable_from_diff","reason"]}`),
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
	out, err := t.ctl.Classify(size, in.ResumableFromDiff, in.Reason)
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
		Description: "Open the task after task_classify returned medium or large, with the goal, the scope and the condition that proves it is done. A small task is written directly and never opened. Opening applies the route's model to this conversation.",
		InputSchema: json.RawMessage(`{"type":"object","properties":{` +
			`"goal":{"type":"string","description":"What the task must achieve, in one or two sentences"},` +
			`"scope":{"type":"string","description":"Files, areas or systems the task may touch; optional"},` +
			`"done_when":{"type":"string","description":"The observable condition that proves the task is finished"}},` +
			`"required":["goal","done_when"]}`),
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
		Description: "Move the open task to the next phase: planned, implement, verify, deliver, done, or failed to close it with a reason. planned needs the plan file path as evidence for a large task, and verify needs a command you ran during implement. Deliver needs a note with the changes and the risks, and a large task delivers only when every spec passed in task_verify. Going back to implement after a failed verify needs the reason as evidence.",
		InputSchema: json.RawMessage(`{"type":"object","properties":{` +
			`"phase":{"type":"string","enum":["planned","implement","verify","deliver","done","failed"],"description":"The phase to move to; the flow rejects a move it does not allow"},` +
			`"evidence":{"type":"string","description":"The evidence the phase needs: the plan path, the command, the change note or the reason; optional otherwise"}},` +
			`"required":["phase"]}`),
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

// TaskVerify records the verdict of one spec of a large task in verify.
type TaskVerify struct{ ctl *flow.Controller }

// NewTaskVerify returns the task_verify tool.
func NewTaskVerify(ctl *flow.Controller) *TaskVerify { return &TaskVerify{ctl: ctl} }

type taskVerifyInput struct {
	Spec     string `json:"spec"`
	Result   string `json:"result"`
	Evidence string `json:"evidence"`
}

func (*TaskVerify) Definition() api.ToolDef {
	return api.ToolDef{
		Name:        "task_verify",
		Description: "Record the verdict of one spec of the open large task during verify, with the evidence: what you ran and what it showed. Call it once per spec, after running that spec's check; task_status shows which specs remain. A fail sends the task back to implement with your reason, and deliver proceeds only when every spec has passed.",
		InputSchema: json.RawMessage(`{"type":"object","properties":{` +
			`"spec":{"type":"string","description":"The spec being judged, as numbered in the feature document"},` +
			`"result":{"type":"string","enum":["pass","fail"],"description":"pass or fail"},` +
			`"evidence":{"type":"string","description":"What you ran and what it showed"}},` +
			`"required":["spec","result","evidence"]}`),
	}
}

func (*TaskVerify) Risk() Risk            { return RiskRead }
func (t *TaskVerify) RiskFor(string) Risk { return t.Risk() }

func (*TaskVerify) Preview(input string) string {
	var in taskVerifyInput
	if err := json.Unmarshal([]byte(input), &in); err != nil {
		return "task_verify: invalid input"
	}
	return "verify " + in.Spec + ": " + in.Result + " · " + in.Evidence
}

func (t *TaskVerify) Execute(ctx context.Context, input string) (string, bool) {
	var in taskVerifyInput
	if err := json.Unmarshal([]byte(input), &in); err != nil {
		return "invalid input: " + err.Error(), true
	}
	n, err := parseSpecNumber(in.Spec)
	if err != nil {
		return err.Error(), true
	}
	out, err := t.ctl.Verify(ctx, n, in.Result, in.Evidence)
	if err != nil {
		return err.Error(), true
	}
	return out, false
}

// parseSpecNumber reads a spec as "S2", "s2" or "2".
func parseSpecNumber(s string) (int, error) {
	s = strings.TrimSpace(s)
	n, err := strconv.Atoi(strings.TrimPrefix(strings.ToUpper(s), "S"))
	if err != nil || n < 1 {
		return 0, fmt.Errorf("spec must be S<n>, for example S2: got %q", s)
	}
	return n, nil
}

// TaskStatus reports the classification and the open task.
type TaskStatus struct{ ctl *flow.Controller }

// NewTaskStatus returns the task_status tool.
func NewTaskStatus(ctl *flow.Controller) *TaskStatus { return &TaskStatus{ctl: ctl} }

func (*TaskStatus) Definition() api.ToolDef {
	return api.ToolDef{
		Name:        "task_status",
		Description: "Show the current classification and the open task, with its phase and the number of evidence items recorded. Use it to check where the flow stands before a phase move or a verdict. It only reads state and changes nothing.",
		InputSchema: json.RawMessage(`{"type":"object","properties":{}}`),
	}
}

func (*TaskStatus) Risk() Risk            { return RiskRead }
func (t *TaskStatus) RiskFor(string) Risk { return t.Risk() }
func (*TaskStatus) Preview(string) string { return "task_status" }

func (t *TaskStatus) Execute(ctx context.Context, _ string) (string, bool) {
	return t.ctl.Status(ctx), false
}
