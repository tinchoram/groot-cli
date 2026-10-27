package ui

import (
	"strings"
	"testing"

	"github.com/tinchoram/groot-cli/internal/workspace"
)

// /agent show names the effort of the agent, or says it is the default.
func TestAgentDetailShowsEffort(t *testing.T) {
	set := agentDetailBlock(workspace.AgentInfo{Name: "reader", Model: "claude-sonnet-5-5", Effort: "high", Prompt: "You read."})
	if !strings.Contains(set, "effort") || !strings.Contains(set, "high") {
		t.Fatalf("agent detail must show its effort:\n%s", set)
	}
	unset := agentDetailBlock(workspace.AgentInfo{Name: "reader", Model: "claude-sonnet-5-5", Prompt: "You read."})
	if !strings.Contains(unset, "effort") || !strings.Contains(unset, "default") {
		t.Fatalf("agent detail must say the effort is the default:\n%s", unset)
	}
}
