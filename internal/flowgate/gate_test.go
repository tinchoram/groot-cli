package flowgate

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/tinchoram/groot-cli/internal/flow"
	"github.com/tinchoram/groot-cli/internal/gate"
	"github.com/tinchoram/groot-cli/internal/memory"
	"github.com/tinchoram/groot-cli/internal/tool"
)

func controller(t *testing.T, mode flow.Mode, warn func(string)) *flow.Controller {
	t.Helper()
	store, err := memory.OpenSQLite(filepath.Join(t.TempDir(), "groot.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	ctl, err := flow.NewController(context.Background(), flow.Options{
		Space: "payments", Agent: "developer", Store: store.Tasks(), Mode: mode,
		Now:  func() time.Time { return time.Date(2026, 10, 24, 9, 0, 0, 0, time.UTC) },
		Warn: warn,
	})
	if err != nil {
		t.Fatal(err)
	}
	return ctl
}

// allowAll is the user's policy in these tests: every call the flow lets
// through is approved.
type allowAll struct{ calls int }

func (a *allowAll) Decide(context.Context, gate.Request) gate.Decision {
	a.calls++
	return gate.Allow
}

func TestDeniedWriteExplainsWhatToDo(t *testing.T) {
	ctl := controller(t, flow.ModeStrict, nil)
	if _, err := ctl.Classify(flow.SizeMedium, "several files"); err != nil {
		t.Fatal(err)
	}
	inner := &allowAll{}
	g := &Gate{Inner: inner, Ctl: ctl}
	req := gate.Request{Tool: "write_file", Input: `{"path":"a.go"}`, Risk: tool.RiskWrite}

	if got := g.Decide(context.Background(), req); got != gate.Deny {
		t.Fatalf("decision = %v, want Deny", got)
	}
	if inner.calls != 0 {
		t.Fatal("a flow denial must not reach the user's gate")
	}
	if reason := g.Explain(req); !strings.Contains(reason, "task_open") {
		t.Fatalf("explanation = %q", reason)
	}
}

func TestReadsAndAllowedWritesGoToTheUserGate(t *testing.T) {
	ctl := controller(t, flow.ModeStrict, nil)
	_, _ = ctl.Classify(flow.SizeSmall, "one line")
	inner := &allowAll{}
	g := &Gate{Inner: inner, Ctl: ctl}

	if got := g.Decide(context.Background(), gate.Request{Tool: "read_file", Risk: tool.RiskRead}); got != gate.Allow {
		t.Fatal("reads pass")
	}
	if got := g.Decide(context.Background(), gate.Request{Tool: "write_file", Input: `{}`, Risk: tool.RiskWrite}); got != gate.Allow {
		t.Fatal("small writes pass the flow")
	}
	if inner.calls != 2 {
		t.Fatalf("user gate consulted %d times, want 2", inner.calls)
	}
}

func TestUserDenialHasNoFlowExplanation(t *testing.T) {
	ctl := controller(t, flow.ModeStrict, nil)
	_, _ = ctl.Classify(flow.SizeSmall, "one line")
	g := &Gate{Inner: gate.ReadOnly{}, Ctl: ctl}
	req := gate.Request{Tool: "bash", Input: `{"command":"rm x"}`, Risk: tool.RiskExec}
	if got := g.Decide(context.Background(), req); got != gate.Deny {
		t.Fatal("the user's policy still denies")
	}
	if reason := g.Explain(req); reason != "" {
		t.Fatalf("a user denial must not be explained by the flow: %q", reason)
	}
}

func TestAdvisoryModeWarnsAndLetsTheCallThrough(t *testing.T) {
	var warnings []string
	ctl := controller(t, flow.ModeAdvisory, func(msg string) { warnings = append(warnings, msg) })
	_, _ = ctl.Classify(flow.SizeMedium, "x")
	inner := &allowAll{}
	g := &Gate{Inner: inner, Ctl: ctl, Warn: func(msg string) { warnings = append(warnings, msg) }}

	if got := g.Decide(context.Background(), gate.Request{Tool: "write_file", Input: `{}`, Risk: tool.RiskWrite}); got != gate.Allow {
		t.Fatal("advisory mode never denies")
	}
	if len(warnings) != 1 || !strings.Contains(warnings[0], "task_open") {
		t.Fatalf("warnings = %v", warnings)
	}
}
