package gate

import (
	"context"
	"testing"

	"github.com/tinchoram/groot-cli/internal/tool"
)

// TestWorkApprovesEveryCallExceptTheAdminAndRiskyOnes covers the work mode rule:
// messages, feedback, task and memory tools run without a prompt, while the
// catalog and space changes keep asking.
func TestWorkApprovesEveryCallExceptTheAdminAndRiskyOnes(t *testing.T) {
	s, _ := newWork(t, true, nil, 0, 0)
	user := &asker{}
	g := Work{Inner: user, State: s}
	ctx := context.Background()

	auto := []string{"send_message", "feedback_report", "feedback_update", "task_advance", "remember", "read_inbox"}
	for _, name := range auto {
		if g.Decide(ctx, Request{Tool: name, Input: `{}`, Risk: tool.RiskWrite}) != Allow {
			t.Fatalf("%s should run without a prompt in work mode", name)
		}
	}
	if user.calls != 0 {
		t.Fatalf("the user was asked %d times for auto tools", user.calls)
	}

	for _, name := range []string{"create_space", "assign_agent", "unassign_agent", "create_agent",
		"import_agent", "install_agent", "upgrade_agent", "set_work_mode"} {
		g.Decide(ctx, Request{Tool: name, Input: `{}`, Risk: tool.RiskWrite})
	}
	if user.calls != 8 {
		t.Fatalf("admin changes asked the user %d times, want 8", user.calls)
	}
}
