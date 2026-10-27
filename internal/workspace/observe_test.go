package workspace

import (
	"context"
	"testing"

	"github.com/tinchoram/groot-cli/internal/flow"
)

// TestObservedPhaseFollowsBashCommands drives real tool calls through the
// agent loop: a read-only command observes planned, a build observes implement.
func TestObservedPhaseFollowsBashCommands(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t,
		classifyStep("tu_1", "medium", true, "two files"),
		toolStep("tu_2", "task_open", map[string]string{"goal": "ship", "scope": "space", "done_when": "tests pass"}),
		toolStep("tu_3", "bash", map[string]string{"command": "ls"}),
		text("listed"),
		toolStep("tu_4", "bash", map[string]string{"command": "go build ./..."}),
		text("built"),
	)
	flowAgent(t, f)
	conv := f.ws.Chat()

	if _, err := conv.Send(ctx, "look around"); err != nil {
		t.Fatal(err)
	}
	if got, ok := conv.Observed(); !ok || got != flow.PhasePlanned {
		t.Fatalf("after ls: observed %q, %v; want planned", got, ok)
	}

	if _, err := conv.Send(ctx, "build it"); err != nil {
		t.Fatal(err)
	}
	if got, ok := conv.Observed(); !ok || got != flow.PhaseImplement {
		t.Fatalf("after go build: observed %q, %v; want implement", got, ok)
	}
	if task, ok := conv.Task(); !ok || task.Phase != flow.PhaseImplement {
		t.Fatalf("observing must not move the stored phase, task = %+v", task)
	}
}
