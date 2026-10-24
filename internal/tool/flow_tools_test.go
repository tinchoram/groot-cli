package tool

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/tinchoram/groot-cli/internal/flow"
	"github.com/tinchoram/groot-cli/internal/memory"
)

func flowController(t *testing.T) *flow.Controller {
	t.Helper()
	store, err := memory.OpenSQLite(filepath.Join(t.TempDir(), "groot.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	ctl, err := flow.NewController(context.Background(), flow.Options{
		Space: "payments", Agent: "developer", Store: store.Tasks(),
		Routes: flow.Routes{Medium: flow.Route{Model: "sonnet"}},
		Now:    func() time.Time { return time.Date(2026, 10, 24, 9, 0, 0, 0, time.UTC) },
	})
	if err != nil {
		t.Fatal(err)
	}
	return ctl
}

func TestTaskToolsRunTheFlowRoundTrip(t *testing.T) {
	ctx := context.Background()
	ctl := flowController(t)

	out, isErr := NewTaskClassify(ctl).Execute(ctx, `{"size":"medium","reason":"two files"}`)
	if isErr || !strings.Contains(out, "sonnet") {
		t.Fatalf("classify = %q (err %v)", out, isErr)
	}
	out, isErr = NewTaskOpen(ctl).Execute(ctx, `{"goal":"ship","scope":"api","done_when":"tests pass"}`)
	if isErr || !strings.Contains(out, "task #1 opened (medium): phase implement") {
		t.Fatalf("open = %q (err %v)", out, isErr)
	}
	if out, isErr = NewTaskAdvance(ctl).Execute(ctx, `{"phase":"verify"}`); !isErr || !strings.Contains(out, "command") {
		t.Fatalf("verify without a command must be refused, got %q", out)
	}
	if out, _ = NewTaskStatus(ctl).Execute(ctx, `{}`); !strings.Contains(out, "task #1 · medium · implement") {
		t.Fatalf("status = %q", out)
	}
}

func TestTaskOpenNeedsAClassificationAndPreviewShowsTheThree(t *testing.T) {
	ctx := context.Background()
	ctl := flowController(t)
	open := NewTaskOpen(ctl)
	if out, isErr := open.Execute(ctx, `{"goal":"g","done_when":"d"}`); !isErr || !strings.Contains(out, "classify first") {
		t.Fatalf("open without classify = %q (err %v)", out, isErr)
	}
	preview := open.Preview(`{"goal":"ship","scope":"api","done_when":"tests pass"}`)
	for _, want := range []string{"ship", "api", "tests pass"} {
		if !strings.Contains(preview, want) {
			t.Fatalf("preview misses %q: %s", want, preview)
		}
	}
}

func TestTaskToolRisks(t *testing.T) {
	ctl := flowController(t)
	if NewTaskOpen(ctl).Risk() != RiskWrite {
		t.Fatal("task_open switches the model: it is a write")
	}
	for _, tl := range []Tool{NewTaskClassify(ctl), NewTaskAdvance(ctl), NewTaskStatus(ctl)} {
		if tl.Risk() != RiskRead {
			t.Fatalf("%s should be RiskRead", tl.Definition().Name)
		}
	}
}
