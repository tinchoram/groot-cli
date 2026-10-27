package ui

import (
	"strings"
	"testing"

	"github.com/tinchoram/groot-cli/internal/flow"
)

func TestHeaderShowsObservedPhaseWhenItDiffers(t *testing.T) {
	chat := &fakeSession{model: "m", task: &flow.Task{ID: 12, Size: flow.SizeMedium, Phase: flow.PhaseImplement}, seen: flow.PhasePlanned}
	m, _ := newTestModel(t, chat)
	if out := stripANSI(m.View()); !strings.Contains(out, "task #12 medium/implement · seen: planned") {
		t.Fatalf("header misses the observed phase:\n%s", out)
	}
}

func TestHeaderOmitsObservedPhaseWhenItMatches(t *testing.T) {
	chat := &fakeSession{model: "m", task: &flow.Task{ID: 12, Size: flow.SizeMedium, Phase: flow.PhaseImplement}, seen: flow.PhaseImplement}
	m, _ := newTestModel(t, chat)
	out := stripANSI(m.View())
	if !strings.Contains(out, "task #12 medium/implement") || strings.Contains(out, "seen:") {
		t.Fatalf("a matching observed phase adds nothing:\n%s", out)
	}
}

func TestTaskCommandPrintsObservedPhaseWhenItDiffers(t *testing.T) {
	chat := &fakeSession{model: "m", task: &flow.Task{ID: 12, Size: flow.SizeMedium, Phase: flow.PhaseImplement}, seen: flow.PhasePlanned}
	m, _ := newTestModel(t, chat)
	m = sendLine(t, m, "/task")
	if out := stripANSI(m.transcriptText()); !strings.Contains(out, "observed: planned") {
		t.Fatalf("/task misses the observed phase:\n%s", out)
	}
}

func TestTaskCommandOmitsObservedPhaseWhenItMatches(t *testing.T) {
	chat := &fakeSession{model: "m", task: &flow.Task{ID: 12, Size: flow.SizeMedium, Phase: flow.PhaseImplement}, seen: flow.PhaseImplement}
	m, _ := newTestModel(t, chat)
	m = sendLine(t, m, "/task")
	if out := stripANSI(m.transcriptText()); strings.Contains(out, "observed:") {
		t.Fatalf("/task repeats a matching phase:\n%s", out)
	}
}
