package gate

import (
	"context"
	"reflect"
	"testing"

	"github.com/tinchoram/groot-cli/internal/tool"
)

// countGate records how often it is asked and answers with its verdict.
type countGate struct {
	calls   int
	verdict Decision
}

func (c *countGate) Decide(context.Context, Request) Decision {
	c.calls++
	return c.verdict
}

func TestSessionGrantCoversOnlyGrantedToolsInScope(t *testing.T) {
	space := t.TempDir()
	work, err := NewWorkState(WorkConfig{Space: space, Ask: []string{`^rm `}})
	if err != nil {
		t.Fatal(err)
	}
	grants := NewGrants()
	if !grants.Grant("write_file") || !grants.Grant("bash") {
		t.Fatal("write_file and bash must be grantable")
	}
	inner := &countGate{verdict: Deny}
	sess := Session{Inner: inner, Grants: grants, Work: work}
	ctx := context.Background()

	inside := Request{Tool: "write_file", Input: `{"path":"notes/a.md"}`, Risk: tool.RiskWrite}
	if got := sess.Decide(ctx, inside); got != Allow || inner.calls != 0 {
		t.Fatalf("granted write inside the space: got %v, inner asked %d times", got, inner.calls)
	}
	outside := Request{Tool: "write_file", Input: `{"path":"/etc/hosts"}`, Risk: tool.RiskWrite}
	if got := sess.Decide(ctx, outside); got != Deny || inner.calls != 1 {
		t.Fatalf("write outside the space must still ask: got %v, asked %d", got, inner.calls)
	}
	if got := sess.Decide(ctx, Request{Tool: "bash", Input: `{"command":"ls"}`, Risk: tool.RiskExec}); got != Allow {
		t.Fatalf("granted bash outside always_ask: got %v", got)
	}
	if got := sess.Decide(ctx, Request{Tool: "bash", Input: `{"command":"rm -rf x"}`, Risk: tool.RiskExec}); got != Deny {
		t.Fatalf("bash matching always_ask must still ask: got %v", got)
	}
	if got := sess.Decide(ctx, Request{Tool: "send_message", Risk: tool.RiskWrite}); got != Deny {
		t.Fatalf("an ungranted tool must still ask: got %v", got)
	}
}

func TestSessionGrantsNeverCoverAdminTools(t *testing.T) {
	grants := NewGrants()
	for _, name := range []string{"create_space", "assign_agent", "upgrade_agent", "set_work_mode"} {
		if grants.Grant(name) || grants.Has(name) || Grantable(name) {
			t.Fatalf("%s must never be grantable for the session", name)
		}
	}
	if !grants.Grant("kubectl") {
		t.Fatal("kubectl should be grantable")
	}
}

func TestGrantsListAndClear(t *testing.T) {
	grants := NewGrants()
	grants.Grant("write_file")
	grants.Grant("bash")
	if got := grants.List(); !reflect.DeepEqual(got, []string{"bash", "write_file"}) {
		t.Fatalf("list = %v", got)
	}
	grants.Clear()
	if got := grants.List(); len(got) != 0 || grants.Has("bash") {
		t.Fatalf("after clear: list = %v", got)
	}
}

func TestSessionWithoutGrantsDelegates(t *testing.T) {
	inner := &countGate{verdict: Allow}
	sess := Session{Inner: inner}
	if got := sess.Decide(context.Background(), Request{Tool: "bash", Risk: tool.RiskExec}); got != Allow || inner.calls != 1 {
		t.Fatalf("nil grants must delegate: got %v, asked %d", got, inner.calls)
	}
}
