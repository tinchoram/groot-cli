package gate

import (
	"context"
	"testing"

	"github.com/tinchoram/groot-cli/internal/tool"
)

func destructiveBash() Request {
	return Request{
		Tool: "bash", Input: `{"command":"git push --force"}`, Risk: tool.RiskExec,
		Destructive: true, DestructiveReason: "git: git push --force",
	}
}

func TestWorkModeNeverAutoApprovesDestructiveInput(t *testing.T) {
	inner := &promptCounter{allow: true}
	w := Work{Inner: inner, State: taintWork(t, true)}

	if w.Decide(context.Background(), destructiveBash()) != Allow || inner.prompts != 1 {
		t.Fatalf("work mode must hand destructive input to the user, prompts = %d", inner.prompts)
	}

	clean := Request{Tool: "bash", Input: `{"command":"go test ./..."}`, Risk: tool.RiskExec}
	w.Decide(context.Background(), clean)
	if inner.prompts != 1 {
		t.Fatalf("a clean bash command must stay approved by work mode, prompts = %d", inner.prompts)
	}
}

func TestWorkModeNeverAutoApprovesDestructiveInputWithoutAlwaysAsk(t *testing.T) {
	work, err := NewWorkState(WorkConfig{Space: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	work.Set(true)
	if work.Would(destructiveBash()) {
		t.Fatal("work mode must not label destructive input as auto-approved, even with an empty always_ask list")
	}
}

func TestDestructiveInputIsNeverCoveredByASessionGrant(t *testing.T) {
	grants := NewGrants()
	grants.Grant("bash")
	inner := &promptCounter{allow: true}
	s := Session{Inner: inner, Grants: grants, Work: taintWork(t, true)}

	if s.Covers(destructiveBash()) {
		t.Fatal("a session grant must not cover destructive input")
	}
	if s.Decide(context.Background(), destructiveBash()) != Allow || inner.prompts != 1 {
		t.Fatalf("destructive input under a grant must reach the user, prompts = %d", inner.prompts)
	}
}

func TestDestructiveRequestCannotBeGrantedForTheSession(t *testing.T) {
	if !(Request{Tool: "bash"}).CanGrant() {
		t.Fatal("an ordinary grantable call must be grantable")
	}
	if destructiveBash().CanGrant() {
		t.Fatal("a destructive call must not be grantable for the session")
	}
}

func TestInteractiveAlwaysAsksForDestructiveInput(t *testing.T) {
	asks := 0
	g := Interactive{Confirm: func(Request) bool { asks++; return true }}

	g.Decide(context.Background(), destructiveBash())
	if asks != 1 {
		t.Fatalf("a destructive exec call must ask, asks = %d", asks)
	}
	g.Decide(context.Background(), Request{Tool: "bash", Risk: tool.RiskRead, Destructive: true})
	if asks != 2 {
		t.Fatalf("a destructive call must ask even when read-risk, asks = %d", asks)
	}
}

// Destructive mirrors Tainted under every policy: the same verdict for the same risk.
func TestDestructiveMirrorsTaintedUnderEveryPolicy(t *testing.T) {
	policies := map[string]Gate{
		"auto-approve": AutoApprove{},
		"read-only":    ReadOnly{},
		"interactive":  Interactive{Confirm: func(Request) bool { return false }},
	}
	for name, g := range policies {
		for _, risk := range []tool.Risk{tool.RiskRead, tool.RiskExec} {
			tainted := g.Decide(context.Background(), Request{Tool: "bash", Risk: risk, Tainted: true})
			destructive := g.Decide(context.Background(), Request{Tool: "bash", Risk: risk, Destructive: true})
			if tainted != destructive {
				t.Errorf("%s with risk %v: tainted = %v, destructive = %v; destructive must mirror tainted",
					name, risk, tainted, destructive)
			}
		}
	}
}
