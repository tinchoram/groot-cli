package workspace

import (
	"context"
	"testing"

	"github.com/tinchoram/groot-cli/internal/config"
)

// A medium task with the default routes leaves the agent's model alone; an
// explicit route model in the config still switches it while the task is open.
func TestTaskOpenKeepsAgentModelUnlessRouteNamesOne(t *testing.T) {
	ctx := context.Background()
	for _, tc := range []struct {
		name, route, want string
	}{
		{"empty route keeps the agent model", "", "mock-model"},
		{"explicit route applies", "claude-haiku-5-5", "claude-haiku-5-5"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newFixture(t,
				classifyStep("tu_1", "medium", true, "two files"),
				toolStep("tu_2", "task_open", map[string]string{"goal": "ship", "scope": "space", "done_when": "tests pass"}),
				text("done"),
			)
			flowAgent(t, f)
			f.ws.opts.Config.Flow.Routes.Medium = config.FlowRoute{Model: tc.route}
			if _, err := f.ws.Chat().Send(ctx, "build it"); err != nil {
				t.Fatal(err)
			}
			if got := f.mock.Model(); got != tc.want {
				t.Fatalf("model = %s, want %s", got, tc.want)
			}
		})
	}
}
