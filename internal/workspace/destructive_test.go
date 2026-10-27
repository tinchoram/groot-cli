package workspace

import (
	"testing"

	"github.com/tinchoram/groot-cli/internal/gate"
	"github.com/tinchoram/groot-cli/internal/guard"
)

// The transcript must not label a destructive bash call as auto-approved,
// because work mode would send it to the user.
func TestAutoLabelNeverMarksDestructiveBashAsAuto(t *testing.T) {
	work, err := gate.NewWorkState(gate.WorkConfig{Space: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	work.Set(true)
	var auto, shown []string
	h := Hooks{
		OnAuto:     func(_, name, _ string) { auto = append(auto, name) },
		OnToolCall: func(_, name, _ string) { shown = append(shown, name) },
	}
	label := autoAwareTool(h, "k", work, gate.Session{Work: work}, guard.New(guard.Warn))

	label("bash", `{"command":"git push --force"}`)
	if len(auto) != 0 || len(shown) != 1 {
		t.Fatalf("destructive call labelled auto: auto=%v shown=%v", auto, shown)
	}

	label("bash", `{"command":"go test ./..."}`)
	if len(auto) != 1 {
		t.Fatalf("an ordinary bash call must stay auto in work mode, auto=%v", auto)
	}
}
