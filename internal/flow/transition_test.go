package flow

import (
	"strings"
	"testing"
	"time"
)

var t0 = time.Date(2026, 10, 24, 9, 0, 0, 0, time.UTC)

func taskAt(size Size, phase Phase) Task {
	return Task{ID: 1, Size: size, Phase: phase, Goal: "g", Scope: "s", DoneWhen: "d", PhaseAt: t0}
}

func withCommand(t Task, at time.Time) Task {
	t.Evidence = append(t.Evidence, Evidence{Phase: PhaseImplement, Kind: KindCommand, Text: "$ go test", At: at})
	return t
}

func TestNewTaskRefusesSmallAndMissingFields(t *testing.T) {
	tests := []struct {
		name     string
		size     Size
		goal     string
		doneWhen string
		wantErr  string
	}{
		{"small never opens a task", SizeSmall, "g", "d", "small"},
		{"unknown size", Size("huge"), "g", "d", "size"},
		{"goal required", SizeMedium, "  ", "d", "goal"},
		{"done when required", SizeLarge, "g", "", "done_when"},
		{"medium opens", SizeMedium, "g", "d", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := NewTask(tt.size, tt.goal, "scope", tt.doneWhen, t0)
			if tt.wantErr == "" {
				if err != nil || got.Phase != openPhase(tt.size) || got.Size != tt.size {
					t.Fatalf("NewTask = %+v, %v", got, err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("err = %v, want mention of %q", err, tt.wantErr)
			}
		})
	}
}

func TestTransitionTable(t *testing.T) {
	later := t0.Add(time.Minute)
	tests := []struct {
		name    string
		task    Task
		to      Phase
		text    string
		want    Phase
		wantErr string
	}{
		{"medium: classified to implement", taskAt(SizeMedium, PhaseClassified), PhaseImplement, "", PhaseImplement, ""},
		{"medium cannot plan", taskAt(SizeMedium, PhaseClassified), PhasePlanned, "docs/tasks/x.md", "", "cannot"},
		{"large: classified to planned with a plan path", taskAt(SizeLarge, PhaseClassified), PhasePlanned, "docs/tasks/1.md", PhasePlanned, ""},
		{"large: plan path is required", taskAt(SizeLarge, PhaseClassified), PhasePlanned, " ", "", "plan"},
		{"large: planned to implement", taskAt(SizeLarge, PhasePlanned), PhaseImplement, "", PhaseImplement, ""},
		{"large cannot skip the plan", taskAt(SizeLarge, PhaseClassified), PhaseImplement, "", "", "cannot"},
		{"implement to verify needs a command", taskAt(SizeMedium, PhaseImplement), PhaseVerify, "", "", "command"},
		{"implement to verify with a command in this cycle", withCommand(taskAt(SizeMedium, PhaseImplement), later), PhaseVerify, "", PhaseVerify, ""},
		{"command before the cycle started does not count", func() Task {
			tk := withCommand(taskAt(SizeMedium, PhaseImplement), t0.Add(-time.Minute))
			tk.PhaseAt = t0
			return tk
		}(), PhaseVerify, "", "", "command"},
		{"verify to implement needs a reason", taskAt(SizeMedium, PhaseVerify), PhaseImplement, "", "", "evidence"},
		{"verify to implement with the failure", taskAt(SizeMedium, PhaseVerify), PhaseImplement, "tests failed: TestX", PhaseImplement, ""},
		{"verify to deliver needs a note", taskAt(SizeMedium, PhaseVerify), PhaseDeliver, "", "", "note"},
		{"verify to deliver with a note", taskAt(SizeMedium, PhaseVerify), PhaseDeliver, "changed x; risk y", PhaseDeliver, ""},
		{"deliver to done", taskAt(SizeMedium, PhaseDeliver), PhaseDone, "", PhaseDone, ""},
		{"reject a delivery back to implement", taskAt(SizeMedium, PhaseDeliver), PhaseImplement, "missing a test", PhaseImplement, ""},
		{"reject needs the reason", taskAt(SizeMedium, PhaseDeliver), PhaseImplement, "", "", "reason"},
		{"implement cannot deliver directly", taskAt(SizeMedium, PhaseImplement), PhaseDeliver, "note", "", "cannot"},
		{"closed tasks do not move", taskAt(SizeMedium, PhaseDone), PhaseImplement, "", "", "closed"},
		{"fail from implement needs a reason", taskAt(SizeMedium, PhaseImplement), PhaseFailed, "", "", "reason"},
		{"fail from implement with a reason", taskAt(SizeMedium, PhaseImplement), PhaseFailed, "gave up", PhaseFailed, ""},
		{"fail from classified", taskAt(SizeLarge, PhaseClassified), PhaseFailed, "user changed scope", PhaseFailed, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ev, err := Transition(tt.task, tt.to, tt.text, later)
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("err = %v, want mention of %q", err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got.Phase != tt.want {
				t.Fatalf("phase = %s, want %s", got.Phase, tt.want)
			}
			if tt.to == PhaseImplement && !got.PhaseAt.Equal(later) {
				t.Fatal("entering implement starts a new cycle")
			}
			if (tt.want == PhaseDone || tt.want == PhaseFailed) && got.ClosedAt.IsZero() {
				t.Fatal("a closed task records when it closed")
			}
			if tt.to == PhasePlanned && got.PlanPath != tt.text {
				t.Fatalf("plan path = %q", got.PlanPath)
			}
			if tt.text != "" && ev.Kind == "" {
				t.Fatal("text given but no evidence returned")
			}
		})
	}
}
