package gate

import (
	"context"
	"testing"

	"github.com/tinchoram/groot-cli/internal/tool"
)

// promptCounter answers every prompt with allow and counts the prompts.
type promptCounter struct {
	prompts int
	allow   bool
}

func (p *promptCounter) Decide(context.Context, Request) Decision {
	p.prompts++
	if p.allow {
		return Allow
	}
	return Deny
}

func taintWork(t *testing.T, on bool) *WorkState {
	t.Helper()
	w, err := NewWorkState(WorkConfig{Space: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	w.Set(on)
	return w
}

func TestGrantedToolStillPromptsForTaintedInput(t *testing.T) {
	grants := NewGrants()
	grants.Grant("bash")
	inner := &promptCounter{allow: true}
	s := Session{Inner: inner, Grants: grants, Work: taintWork(t, false)}

	tainted := Request{Tool: "bash", Input: `{"command":"ls"}`, Risk: tool.RiskExec, Tainted: true}
	if s.Decide(context.Background(), tainted) != Allow || inner.prompts != 1 {
		t.Fatalf("a granted tool with tainted input must reach the user, prompts = %d", inner.prompts)
	}

	clean := Request{Tool: "bash", Input: `{"command":"ls"}`, Risk: tool.RiskExec}
	if s.Decide(context.Background(), clean) != Allow || inner.prompts != 1 {
		t.Fatalf("untainted input under a grant must not prompt, prompts = %d", inner.prompts)
	}
}

func TestWorkModeDoesNotAutoApproveTaintedInput(t *testing.T) {
	inner := &promptCounter{allow: true}
	w := Work{Inner: inner, State: taintWork(t, true)}

	tainted := Request{Tool: "write_file", Input: `{"path":"notes.md"}`, Risk: tool.RiskWrite, Tainted: true}
	if w.Decide(context.Background(), tainted) != Allow || inner.prompts != 1 {
		t.Fatalf("work mode must hand tainted input to the user, prompts = %d", inner.prompts)
	}

	clean := Request{Tool: "write_file", Input: `{"path":"notes.md"}`, Risk: tool.RiskWrite}
	if w.Decide(context.Background(), clean) != Allow || inner.prompts != 1 {
		t.Fatalf("untainted write inside the space must stay approved by work mode, prompts = %d", inner.prompts)
	}

	taintedBash := Request{Tool: "bash", Input: `{"command":"ls"}`, Risk: tool.RiskExec, Tainted: true}
	w.Decide(context.Background(), taintedBash)
	if inner.prompts != 2 {
		t.Fatalf("tainted bash must prompt even though work mode approves bash, prompts = %d", inner.prompts)
	}
}

func TestTaintedInputIsNeverCoveredOrLabelledAuto(t *testing.T) {
	grants := NewGrants()
	grants.Grant("bash")
	work := taintWork(t, true)
	s := Session{Inner: &promptCounter{}, Grants: grants, Work: work}
	tainted := Request{Tool: "bash", Input: `{"command":"ls"}`, Risk: tool.RiskExec, Tainted: true}

	if work.Would(tainted) {
		t.Fatal("work mode must not label tainted input as auto-approved")
	}
	if s.Covers(tainted) {
		t.Fatal("a session grant must not cover tainted input")
	}
}

func TestInteractiveAsksForTaintedReadsOnly(t *testing.T) {
	asks := 0
	g := Interactive{Confirm: func(Request) bool { asks++; return true }}

	g.Decide(context.Background(), Request{Tool: "read_file", Risk: tool.RiskRead})
	if asks != 0 {
		t.Fatalf("an untainted read must not ask, asks = %d", asks)
	}
	g.Decide(context.Background(), Request{Tool: "read_file", Risk: tool.RiskRead, Tainted: true})
	if asks != 1 {
		t.Fatalf("a tainted read must ask, asks = %d", asks)
	}
}

func TestTaintedRequestCannotBeGrantedForTheSession(t *testing.T) {
	if !(Request{Tool: "bash"}).CanGrant() {
		t.Fatal("an untainted grantable call must be grantable")
	}
	if (Request{Tool: "bash", Tainted: true}).CanGrant() {
		t.Fatal("a tainted call must not be grantable for the session")
	}
	if (Request{Tool: "create_space"}).CanGrant() {
		t.Fatal("admin tools must not be grantable")
	}
}
