package gate

import (
	"context"
	"testing"

	"github.com/tinchoram/groot-cli/internal/tool"
)

func TestAutoApproveAllowsEverything(t *testing.T) {
	g := AutoApprove{}
	for _, r := range []tool.Risk{tool.RiskRead, tool.RiskWrite, tool.RiskExec} {
		if d := g.Decide(context.Background(), Request{Tool: "x", Risk: r}); d != Allow {
			t.Fatalf("risk %v should be allowed", r)
		}
	}
}

func TestReadOnlyAllowsOnlyReads(t *testing.T) {
	g := ReadOnly{}
	cases := map[tool.Risk]Decision{
		tool.RiskRead:  Allow,
		tool.RiskWrite: Deny,
		tool.RiskExec:  Deny,
	}
	for r, want := range cases {
		if got := g.Decide(context.Background(), Request{Tool: "x", Risk: r}); got != want {
			t.Errorf("risk %v: got %v want %v", r, got, want)
		}
	}
}

func TestInteractiveAutoAllowsReadsWithoutAsking(t *testing.T) {
	asked := 0
	g := Interactive{Confirm: func(Request) bool { asked++; return false }}
	if d := g.Decide(context.Background(), Request{Tool: "kubectl", Risk: tool.RiskRead}); d != Allow {
		t.Fatal("reads must be auto-allowed")
	}
	if asked != 0 {
		t.Fatalf("reads must not prompt, prompted %d times", asked)
	}
}

func TestInteractiveAsksForWritesAndExec(t *testing.T) {
	var seen []Request
	g := Interactive{Confirm: func(r Request) bool { seen = append(seen, r); return r.Risk == tool.RiskExec }}

	w := Request{Tool: "write_file", Risk: tool.RiskWrite, Detail: "--- a\n+++ b"}
	if d := g.Decide(context.Background(), w); d != Deny {
		t.Fatal("confirm returned false, write must be denied")
	}
	e := Request{Tool: "bash", Risk: tool.RiskExec, Detail: "ls"}
	if d := g.Decide(context.Background(), e); d != Allow {
		t.Fatal("confirm returned true, exec must be allowed")
	}
	if len(seen) != 2 || seen[0].Detail != w.Detail {
		t.Fatalf("confirm must receive the preview detail: %+v", seen)
	}
}

func TestInteractiveWithoutConfirmDenies(t *testing.T) {
	g := Interactive{}
	if d := g.Decide(context.Background(), Request{Tool: "bash", Risk: tool.RiskExec}); d != Deny {
		t.Fatal("no confirmer means deny, never silent allow")
	}
}

func TestDecisionString(t *testing.T) {
	if Allow.String() != "allow" || Deny.String() != "deny" {
		t.Fatal("decision names drifted")
	}
}
