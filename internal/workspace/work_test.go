package workspace

import (
	"context"
	"strings"
	"testing"

	"github.com/tinchoram/groot-cli/internal/flow"
)

func TestDeliverInWorkModeTurnsItOffAndBuildsTheReview(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t,
		toolStep("tu_1", "task_classify", map[string]string{"size": "medium", "reason": "two files"}),
		toolStep("tu_2", "task_open", map[string]string{"goal": "ship", "scope": "space", "done_when": "tests pass"}),
		toolStep("tu_3", "write_file", map[string]string{"path": "a.txt", "content": "x"}),
		toolStep("tu_4", "bash", map[string]string{"command": "echo hi"}),
		toolStep("tu_5", "task_advance", map[string]string{"phase": "verify"}),
		toolStep("tu_6", "task_advance", map[string]string{"phase": "deliver", "evidence": "changed a.txt; risk: none known"}),
		text("delivered"),
	)
	flowAgent(t, f)
	conv := f.ws.Chat()
	conv.SetWorkMode(true)
	if _, err := conv.Send(ctx, "build it"); err != nil {
		t.Fatal(err)
	}
	if conv.WorkMode() {
		t.Fatal("delivering in work mode turns work mode off")
	}
	review := conv.TakeReview()
	for _, want := range []string{"task #1 · medium", "written: a.txt", "$ echo hi (exit: 0)", "changed a.txt; risk: none known", "work mode is off"} {
		if !strings.Contains(review, want) {
			t.Fatalf("review misses %q:\n%s", want, review)
		}
	}
	if again := conv.TakeReview(); again != "" {
		t.Fatal("the review is shown once")
	}
}

func TestAcceptClosesTheDeliveredTask(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t,
		toolStep("tu_1", "task_classify", map[string]string{"size": "medium", "reason": "x"}),
		toolStep("tu_2", "task_open", map[string]string{"goal": "g", "scope": "s", "done_when": "d"}),
		toolStep("tu_3", "bash", map[string]string{"command": "go test ./..."}),
		toolStep("tu_4", "task_advance", map[string]string{"phase": "verify"}),
		toolStep("tu_5", "task_advance", map[string]string{"phase": "deliver", "evidence": "done"}),
		text("delivered"),
	)
	flowAgent(t, f)
	conv := f.ws.Chat()
	if _, err := conv.Send(ctx, "go"); err != nil {
		t.Fatal(err)
	}
	if out, err := conv.TaskAccept(ctx); err != nil || !strings.Contains(out, "accepted") {
		t.Fatalf("accept = %q, %v", out, err)
	}
	if _, ok := conv.Task(); ok {
		t.Fatal("an accepted task is closed")
	}
}

func TestRejectSendsTheTaskBackToImplement(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t,
		toolStep("tu_1", "task_classify", map[string]string{"size": "medium", "reason": "x"}),
		toolStep("tu_2", "task_open", map[string]string{"goal": "g", "scope": "s", "done_when": "d"}),
		toolStep("tu_3", "bash", map[string]string{"command": "go test ./..."}),
		toolStep("tu_4", "task_advance", map[string]string{"phase": "verify"}),
		toolStep("tu_5", "task_advance", map[string]string{"phase": "deliver", "evidence": "done"}),
		text("delivered"),
	)
	flowAgent(t, f)
	conv := f.ws.Chat()
	if _, err := conv.Send(ctx, "go"); err != nil {
		t.Fatal(err)
	}
	out, err := conv.TaskReject(ctx, "the test for the edge case is missing")
	if err != nil || !strings.Contains(out, "sent back to implement") {
		t.Fatalf("reject = %q, %v", out, err)
	}
	task, ok := conv.Task()
	if !ok || task.Phase != flow.PhaseImplement {
		t.Fatalf("task after reject = %+v, %v", task, ok)
	}
	last := task.Evidence[len(task.Evidence)-1]
	if last.Kind != flow.KindNote || last.Text != "the test for the edge case is missing" {
		t.Fatalf("the reason is recorded as evidence: %+v", last)
	}
}

func TestSetWorkModeOpensTheConversationAndTogglesIt(t *testing.T) {
	f := newFixture(t, text("ok"))
	if err := f.ws.SetWorkMode("default", "reader", true); err != nil {
		t.Fatal(err)
	}
	if !f.ws.WorkModes()["default/reader"] {
		t.Fatal("work mode is listed for the agent")
	}
	if err := f.ws.SetWorkMode("default", "nobody", true); err == nil {
		t.Fatal("an agent that is not assigned cannot enter work mode")
	}
	if err := f.ws.SetWorkMode("default", "reader", false); err != nil {
		t.Fatal(err)
	}
	if f.ws.WorkModes()["default/reader"] {
		t.Fatal("work mode is off again")
	}
}
