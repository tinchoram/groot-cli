package ui

import (
	"strings"
	"testing"

	"github.com/tinchoram/groot-cli/internal/flow"
)

func TestHeaderNamesTheOpenTask(t *testing.T) {
	chat := &fakeSession{model: "m", task: &flow.Task{ID: 12, Size: flow.SizeMedium, Phase: flow.PhaseImplement}}
	m, _ := newTestModel(t, chat)
	if out := stripANSI(m.View()); !strings.Contains(out, "task #12 medium/implement") {
		t.Fatalf("header misses the task:\n%s", out)
	}
}

func TestHeaderHasNoTaskWithoutOne(t *testing.T) {
	m, _ := newTestModel(t, &fakeSession{model: "m"})
	if strings.Contains(stripANSI(m.View()), "task #") {
		t.Fatal("no open task, no task label")
	}
}

func TestTaskCommandPrintsStatus(t *testing.T) {
	chat := &fakeSession{model: "m", task: &flow.Task{ID: 12, Size: flow.SizeMedium, Phase: flow.PhaseImplement, Goal: "ship it"}}
	m, _ := newTestModel(t, chat)
	m = sendLine(t, m, "/task")
	out := stripANSI(m.transcriptText())
	if !strings.Contains(out, "task #12 · medium · implement") || !strings.Contains(out, "goal: ship it") {
		t.Fatalf("/task output:\n%s", out)
	}
}

func TestTaskFailClosesWithReason(t *testing.T) {
	chat := &fakeSession{model: "m", task: &flow.Task{ID: 12, Size: flow.SizeMedium, Phase: flow.PhaseVerify}}
	m, _ := newTestModel(t, chat)
	m = sendLine(t, m, "/task fail the scope changed")
	if len(chat.failed) != 1 || chat.failed[0] != "the scope changed" {
		t.Fatalf("failed = %v", chat.failed)
	}
	if !strings.Contains(stripANSI(m.transcriptText()), "task #12 failed: the scope changed") {
		t.Fatalf("confirmation missing:\n%s", m.transcriptText())
	}
}

func TestTaskFailNeedsAReason(t *testing.T) {
	chat := &fakeSession{model: "m", task: &flow.Task{ID: 12}}
	m, _ := newTestModel(t, chat)
	m = sendLine(t, m, "/task fail")
	if len(chat.failed) != 0 || !strings.Contains(m.transcriptText(), "usage: /task fail <reason>") {
		t.Fatalf("a failure without a reason must be refused: %v", chat.failed)
	}
}
