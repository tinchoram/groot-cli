package workspace

import (
	"context"
	"strings"
	"sync"
	"testing"

	"github.com/tinchoram/groot-cli/internal/catalog"
	"github.com/tinchoram/groot-cli/internal/gate"
)

// callGate records the bash calls that reach the user's gate and denies them.
// Every other call is allowed, so the flow can open its task.
type callGate struct {
	mu    sync.Mutex
	calls []gate.Request
}

func (g *callGate) Decide(_ context.Context, req gate.Request) gate.Decision {
	if req.Tool != "bash" && req.Tool != "bash_background" {
		return gate.Allow
	}
	g.mu.Lock()
	g.calls = append(g.calls, req)
	g.mu.Unlock()
	return gate.Deny
}

func (g *callGate) seen() []gate.Request {
	g.mu.Lock()
	defer g.mu.Unlock()
	return append([]gate.Request(nil), g.calls...)
}

// runInWorkMode runs one bash call of the given tool, with work mode on, in an
// open conversation, and returns the calls that reached the user's gate.
func runInWorkMode(t *testing.T, toolName, command string) []gate.Request {
	t.Helper()
	steps := append(jobFlow(), toolStep("b_1", toolName, map[string]string{"command": command}), text("done"))
	f := newFixture(t, steps...)
	g := &callGate{}
	f.ws.opts.Gate = g
	tools := []string{"bash", "bash_background", "task_classify", "task_open", "task_advance", "task_verify", "task_status"}
	if err := catalog.Save(f.home, catalog.AgentDef{Name: "runner", Description: "runs commands", Tools: tools}, "You run commands."); err != nil {
		t.Fatal(err)
	}
	if err := f.ws.reload(); err != nil {
		t.Fatal(err)
	}
	if err := f.spaces.Assign("default", "runner"); err != nil {
		t.Fatal(err)
	}
	if err := f.ws.SelectAgent("runner"); err != nil {
		t.Fatal(err)
	}
	if err := f.ws.SetWorkMode("default", "runner", true); err != nil {
		t.Fatal(err)
	}
	if _, err := f.ws.Chat().Send(context.Background(), "go"); err != nil {
		t.Fatalf("Send: %v", err)
	}
	return g.seen()
}

// A destructive bash call in an open conversation is flagged, so work mode does
// not approve it and it reaches the user's gate with its reason.
func TestDestructiveBashInAConversationReachesTheGateFlagged(t *testing.T) {
	calls := runInWorkMode(t, "bash", "git push --force origin main")
	if len(calls) != 1 {
		t.Fatalf("calls that reached the gate = %d, want the destructive one only", len(calls))
	}
	got := calls[0]
	if !got.Destructive || !strings.Contains(got.DestructiveReason, "git push --force") {
		t.Fatalf("destructive = %v reason = %q; the wrapped bash must forward the classification", got.Destructive, got.DestructiveReason)
	}
	if got.Tool != "bash" {
		t.Fatalf("tool = %q", got.Tool)
	}
}

// The background tool is flagged through the same path.
func TestDestructiveBackgroundBashInAConversationReachesTheGateFlagged(t *testing.T) {
	calls := runInWorkMode(t, "bash_background", "git push --force origin main")
	if len(calls) != 1 || !calls[0].Destructive || !strings.Contains(calls[0].DestructiveReason, "git push --force") {
		t.Fatalf("bash_background calls at the gate = %+v, want one flagged destructive call", calls)
	}
}

// Work mode still approves an ordinary command: the control for the two tests above.
func TestOrdinaryBashInWorkModeIsNotAskedAgain(t *testing.T) {
	if calls := runInWorkMode(t, "bash", "echo hello"); len(calls) != 0 {
		t.Fatalf("work mode must approve a plain command, but %d reached the gate", len(calls))
	}
}
