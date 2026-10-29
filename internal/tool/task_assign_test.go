package tool

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/tinchoram/groot-cli/internal/flow"
)

// assigner records the calls of task_assign and answers with a fixed result.
type assigner struct {
	task  int64
	agent string
	note  string
	err   error
}

func (a *assigner) AssignTask(_ context.Context, task int64, agent, note string) (string, error) {
	a.task, a.agent, a.note = task, agent, note
	if a.err != nil {
		return "", a.err
	}
	return "task #" + strings.TrimPrefix(agent, "#") + " assigned", nil
}

func TestTaskAssignPassesTheArgumentsToTheWorkspace(t *testing.T) {
	a := &assigner{}
	out, isErr := NewTaskAssign(a).Execute(context.Background(), `{"task":4,"agent":"developer","note":"split billing first"}`)
	if isErr || out != "task #developer assigned" {
		t.Fatalf("Execute = %q, %v", out, isErr)
	}
	if a.task != 4 || a.agent != "developer" || a.note != "split billing first" {
		t.Fatalf("the assigner got task %d, agent %q, note %q", a.task, a.agent, a.note)
	}
}

func TestTaskAssignReturnsTheRefusalAsAnError(t *testing.T) {
	a := &assigner{err: errors.New("agent tester has open task #2")}
	out, isErr := NewTaskAssign(a).Execute(context.Background(), `{"task":4,"agent":"tester"}`)
	if !isErr || out != "agent tester has open task #2" {
		t.Fatalf("Execute = %q, %v; a refusal must be an error result", out, isErr)
	}
}

func TestTaskAssignRejectsAnInvalidTaskNumber(t *testing.T) {
	a := &assigner{}
	if out, isErr := NewTaskAssign(a).Execute(context.Background(), `{"task":0,"agent":"developer"}`); !isErr || !strings.Contains(out, "task") {
		t.Fatalf("Execute = %q, %v; a task number must be positive", out, isErr)
	}
	if out, isErr := NewTaskAssign(a).Execute(context.Background(), `{"task":"four","agent":"developer"}`); !isErr {
		t.Fatalf("a text task number must be refused, got %q", out)
	}
}

func TestTaskAssignSchemaRiskAndPreview(t *testing.T) {
	tl := NewTaskAssign(&assigner{})
	def := tl.Definition()
	if def.Name != "task_assign" {
		t.Fatalf("name = %q", def.Name)
	}
	var schema struct {
		Required []string `json:"required"`
	}
	if err := json.Unmarshal(def.InputSchema, &schema); err != nil {
		t.Fatal(err)
	}
	if len(schema.Required) != 2 || schema.Required[0] != "task" || schema.Required[1] != "agent" {
		t.Fatalf("required = %v, want task and agent", schema.Required)
	}
	if tl.Risk() != RiskWrite {
		t.Fatalf("risk = %v, an assignment sends a handover", tl.Risk())
	}
	if p := tl.Preview(`{"task":4,"agent":"developer","note":"first"}`); !strings.Contains(p, "#4") || !strings.Contains(p, "developer") {
		t.Fatalf("preview = %q", p)
	}
}

func TestSendHandoverSendsTheHandoverAndTheTrackingCopy(t *testing.T) {
	m := &fakeMessenger{resolved: "payments/developer"}
	task := flow.Task{ID: 3, Size: flow.SizeMedium, Goal: "split billing", DoneWhen: "tests pass"}
	note, err := SendHandover(context.Background(), m, "payments/reviewer", task, flow.Handover{Delegate: "developer", Manager: "space-manager"})
	if err != nil || note != "" {
		t.Fatalf("SendHandover = %q, %v", note, err)
	}
	if len(m.sent) != 2 {
		t.Fatalf("sent %d messages, want the handover and the tracking copy: %v", len(m.sent), m.sent)
	}
	if !strings.HasPrefix(m.sent[0], "developer|task:#3 handover|") || !strings.Contains(m.sent[0], "handed over to you from payments/reviewer") {
		t.Fatalf("handover = %q", m.sent[0])
	}
	if !strings.HasPrefix(m.sent[1], "space-manager|task:#3 tracking|") || !strings.Contains(m.sent[1], "handed over to developer") {
		t.Fatalf("tracking copy = %q", m.sent[1])
	}
}

func TestSendHandoverWithoutAManagerSendsOnlyTheHandover(t *testing.T) {
	m := &fakeMessenger{resolved: "payments/developer"}
	task := flow.Task{ID: 3, Size: flow.SizeMedium, Goal: "split billing", DoneWhen: "tests pass"}
	if _, err := SendHandover(context.Background(), m, "payments/reviewer", task, flow.Handover{Delegate: "developer"}); err != nil {
		t.Fatal(err)
	}
	if len(m.sent) != 1 {
		t.Fatalf("sent %v, want the handover only", m.sent)
	}
}

func TestSendHandoverFailsWhenTheHandoverIsNotSent(t *testing.T) {
	m := &fakeMessenger{sendErr: errors.New("inbox closed")}
	task := flow.Task{ID: 3, Size: flow.SizeMedium, Goal: "g", DoneWhen: "d"}
	if _, err := SendHandover(context.Background(), m, "payments/reviewer", task, flow.Handover{Delegate: "developer", Manager: "space-manager"}); err == nil {
		t.Fatal("a handover that is not sent is an error")
	}
	if len(m.sent) != 0 {
		t.Fatalf("no tracking copy may follow a failed handover: %v", m.sent)
	}
}
