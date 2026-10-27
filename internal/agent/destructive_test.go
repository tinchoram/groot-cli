package agent

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/tinchoram/groot-cli/internal/api"
	"github.com/tinchoram/groot-cli/internal/provider"
	"github.com/tinchoram/groot-cli/internal/tool"
)

// The gate denies every call, so no destructive command runs during the test.
func TestBashDestructiveCommandReachesTheGateFlagged(t *testing.T) {
	rec := &recordGate{allow: false}
	a := newAgent(provider.NewMock(), registry(t, tool.NewBash(t.TempDir())), rec)

	destructive := api.Block{Type: api.BlockToolUse, ID: "1", Name: "bash", Input: json.RawMessage(`{"command":"git push --force"}`)}
	plain := api.Block{Type: api.BlockToolUse, ID: "2", Name: "bash", Input: json.RawMessage(`{"command":"ls"}`)}
	a.runTool(context.Background(), destructive)
	a.runTool(context.Background(), plain)

	if len(rec.reqs) != 2 {
		t.Fatalf("gate calls = %d, want 2", len(rec.reqs))
	}
	got := rec.reqs[0]
	if !got.Destructive || !strings.Contains(got.DestructiveReason, "git") || !strings.Contains(got.DestructiveReason, "git push --force") {
		t.Fatalf("destructive call: Destructive=%v reason=%q", got.Destructive, got.DestructiveReason)
	}
	if rec.reqs[1].Destructive || rec.reqs[1].DestructiveReason != "" {
		t.Fatalf("ls must not be flagged destructive: %+v", rec.reqs[1])
	}
}
