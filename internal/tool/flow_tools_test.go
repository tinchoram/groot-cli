package tool

import (
	"context"
	"encoding/json"
	"path/filepath"
	"slices"
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

	out, isErr := NewTaskClassify(ctl).Execute(ctx, `{"size":"medium","resumable_from_diff":true,"reason":"two files"}`)
	if isErr || !strings.Contains(out, "sonnet") {
		t.Fatalf("classify = %q (err %v)", out, isErr)
	}
	out, isErr = NewTaskOpen(ctl, nil).Execute(ctx, `{"goal":"ship","scope":"api","done_when":"tests pass"}`)
	if isErr || !strings.Contains(out, "task #1 opened (medium): phase implement") {
		t.Fatalf("open = %q (err %v)", out, isErr)
	}
	if out, isErr = NewTaskAdvance(ctl, nil).Execute(ctx, `{"phase":"verify"}`); !isErr || !strings.Contains(out, "command") {
		t.Fatalf("verify without a command must be refused, got %q", out)
	}
	if out, _ = NewTaskStatus(ctl).Execute(ctx, `{}`); !strings.Contains(out, "task #1 · medium · implement") {
		t.Fatalf("status = %q", out)
	}
}

func TestTaskOpenNeedsAClassificationAndPreviewShowsTheThree(t *testing.T) {
	ctx := context.Background()
	ctl := flowController(t)
	open := NewTaskOpen(ctl, nil)
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
	if NewTaskOpen(ctl, nil).Risk() != RiskWrite {
		t.Fatal("task_open switches the model: it is a write")
	}
	for _, tl := range []Tool{NewTaskClassify(ctl), NewTaskAdvance(ctl, nil), NewTaskStatus(ctl), NewTaskVerify(ctl)} {
		if tl.Risk() != RiskRead {
			t.Fatalf("%s should be RiskRead", tl.Definition().Name)
		}
	}
}

func TestTaskClassifySchemaRequiresTheResumeAnswer(t *testing.T) {
	var def struct {
		Properties map[string]json.RawMessage `json:"properties"`
		Required   []string                   `json:"required"`
	}
	if err := json.Unmarshal(NewTaskClassify(flowController(t)).Definition().InputSchema, &def); err != nil {
		t.Fatal(err)
	}
	if _, ok := def.Properties["resumable_from_diff"]; !ok {
		t.Fatalf("schema lacks resumable_from_diff: %v", def.Properties)
	}
	want := []string{"size", "resumable_from_diff", "reason"}
	if !slices.Equal(def.Required, want) {
		t.Fatalf("required = %v, want %v", def.Required, want)
	}
	if desc := NewTaskClassify(flowController(t)).Definition().Description; !strings.Contains(desc, "could another agent resume it from the request plus the git diff alone?") {
		t.Fatalf("description lacks the resume test: %s", desc)
	}
}

func TestTaskClassifyRefusesAnAnswerThatContradictsTheSize(t *testing.T) {
	ctl := flowController(t)
	out, isErr := NewTaskClassify(ctl).Execute(context.Background(), `{"size":"large","resumable_from_diff":true,"reason":"x"}`)
	if !isErr || !strings.Contains(out, "classify medium") {
		t.Fatalf("large and resumable must be refused, got %q (err %v)", out, isErr)
	}
	out, isErr = NewTaskClassify(ctl).Execute(context.Background(), `{"size":"large","resumable_from_diff":false,"reason":"x"}`)
	if isErr || !strings.Contains(out, "next: task_open, write docs/tasks/<id>.md") {
		t.Fatalf("large and not resumable must be accepted, got %q (err %v)", out, isErr)
	}
}

func TestTaskVerifySchemaPreviewAndRisk(t *testing.T) {
	ctl := flowController(t)
	verify := NewTaskVerify(ctl)
	var def struct {
		Required []string `json:"required"`
	}
	if verify.Definition().Name != "task_verify" {
		t.Fatalf("name = %q", verify.Definition().Name)
	}
	if err := json.Unmarshal(verify.Definition().InputSchema, &def); err != nil {
		t.Fatal(err)
	}
	if want := []string{"spec", "result", "evidence"}; !slices.Equal(def.Required, want) {
		t.Fatalf("required = %v, want %v", def.Required, want)
	}
	if preview := verify.Preview(`{"spec":"S2","result":"fail","evidence":"totals"}`); !strings.Contains(preview, "S2") || !strings.Contains(preview, "fail") {
		t.Fatalf("preview = %q", preview)
	}
	if verify.Risk() != RiskRead {
		t.Fatal("task_verify is RiskRead, like the other task tools")
	}
}

func TestTaskVerifyParsesTheSpecNumber(t *testing.T) {
	ctx := context.Background()
	verify := NewTaskVerify(flowController(t))
	if out, isErr := verify.Execute(ctx, `{"spec":"two","result":"pass","evidence":"ok"}`); !isErr || !strings.Contains(out, "spec must be S<n>") {
		t.Fatalf("a spec that is not a number must be refused, got %q (err %v)", out, isErr)
	}
	for _, spec := range []string{"S2", "s2", "2"} {
		out, isErr := verify.Execute(ctx, `{"spec":"`+spec+`","result":"pass","evidence":"ok"}`)
		if !isErr || !strings.Contains(out, "no open task") {
			t.Fatalf("spec %q did not reach the controller: %q (err %v)", spec, out, isErr)
		}
	}
}
