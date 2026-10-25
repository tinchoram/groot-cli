package flow

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

// fakeStore keeps tasks in memory.
type fakeStore struct {
	tasks []Task
	next  int64
	specs map[int64][]SpecState
}

func (s *fakeStore) Open(_ context.Context, t Task) (Task, error) {
	s.next++
	t.ID = s.next
	s.tasks = append(s.tasks, t)
	return t, nil
}

func (s *fakeStore) withSpecs(t Task) Task {
	t.Specs = append([]SpecState(nil), s.specs[t.ID]...)
	return t
}

func (s *fakeStore) Get(_ context.Context, id int64) (Task, error) {
	for _, t := range s.tasks {
		if t.ID == id {
			return s.withSpecs(t), nil
		}
	}
	return Task{}, errors.New("no such task")
}

func (s *fakeStore) Active(_ context.Context, space, agent string) (Task, bool, error) {
	for _, t := range s.tasks {
		if t.Space == space && t.Agent == agent && !t.Closed() {
			return s.withSpecs(t), true, nil
		}
	}
	return Task{}, false, nil
}

func (s *fakeStore) SetSpecs(_ context.Context, id int64, specs []Spec) error {
	if s.specs == nil {
		s.specs = map[int64][]SpecState{}
	}
	var out []SpecState
	for _, sp := range specs {
		out = append(out, SpecState{Spec: sp})
	}
	s.specs[id] = out
	return nil
}

func (s *fakeStore) Specs(_ context.Context, id int64) ([]SpecState, error) {
	return append([]SpecState(nil), s.specs[id]...), nil
}

func (s *fakeStore) SetVerdict(_ context.Context, id int64, n int, status, note string) error {
	for i := range s.specs[id] {
		if s.specs[id][i].N == n {
			s.specs[id][i].Status, s.specs[id][i].Note = status, note
			return nil
		}
	}
	return errors.New("no such spec")
}

func (s *fakeStore) ResetVerdicts(_ context.Context, id int64) error {
	for i := range s.specs[id] {
		s.specs[id][i].Status, s.specs[id][i].Note = VerdictPending, ""
	}
	return nil
}

func (s *fakeStore) Update(_ context.Context, t Task) error {
	for i := range s.tasks {
		if s.tasks[i].ID == t.ID {
			ev := s.tasks[i].Evidence
			s.tasks[i] = t
			s.tasks[i].Evidence = ev
		}
	}
	return nil
}

func (s *fakeStore) AddEvidence(_ context.Context, id int64, ev Evidence) error {
	for i := range s.tasks {
		if s.tasks[i].ID == id {
			s.tasks[i].Evidence = append(s.tasks[i].Evidence, ev)
		}
	}
	return nil
}

func (s *fakeStore) List(context.Context, string, string, int) ([]Task, error) {
	return s.tasks, nil
}

// clock is a controllable time source.
type clock struct{ now time.Time }

func (c *clock) tick() time.Time { c.now = c.now.Add(time.Second); return c.now }

type rig struct {
	ctl      *Controller
	store    *fakeStore
	clk      *clock
	applied  []Route
	restored int
	warnings []string
	planOK   bool
	// docs are the plan files of the space, by path.
	docs map[string]string
}

func newRig(t *testing.T, mode Mode) *rig {
	t.Helper()
	h := &rig{store: &fakeStore{}, clk: &clock{now: t0}, planOK: true, docs: map[string]string{}}
	ctl, err := NewController(context.Background(), Options{
		Space: "payments", Agent: "reviewer", Store: h.store, Mode: mode,
		Routes: Routes{
			Small:  Route{Model: "small-model"},
			Medium: Route{Model: "medium-model"},
			Large:  Route{Model: "large-model", Agent: "developer"},
		},
		Now:        h.clk.tick,
		PlanExists: func(string) bool { return h.planOK },
		ReadPlan: func(path string) (string, error) {
			src, ok := h.docs[path]
			if !ok {
				return "", errors.New("no such plan")
			}
			return src, nil
		},
		ApplyRoute: func(r Route) func() {
			h.applied = append(h.applied, r)
			return func() { h.restored++ }
		},
		Warn: func(msg string) { h.warnings = append(h.warnings, msg) },
	})
	if err != nil {
		t.Fatal(err)
	}
	h.ctl = ctl
	return h
}

var ctx = context.Background()

func (h *rig) open(t *testing.T, size Size) Task {
	t.Helper()
	tk, err := h.openTask(size)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	return tk
}

func (h *rig) openTask(size Size) (Task, error) {
	if _, err := h.ctl.Classify(size, size != SizeLarge, "reason"); err != nil {
		return Task{}, err
	}
	tk, _, err := h.ctl.Open(ctx, "ship the change", "payments/api", "tests pass")
	return tk, err
}

// mustOpen is open for table setups, which have no *testing.T.
func mustOpen(h *rig, size Size) {
	if _, err := h.openTask(size); err != nil {
		panic(err)
	}
}

func TestClassifyStatesTheRouteAndRequirements(t *testing.T) {
	h := newRig(t, ModeStrict)
	text, err := h.ctl.Classify(SizeMedium, true, "several files")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"medium", "medium-model", "task_open"} {
		if !strings.Contains(text, want) {
			t.Fatalf("classification text misses %q: %s", want, text)
		}
	}
	if _, err := h.ctl.Classify(Size("huge"), true, ""); err == nil {
		t.Fatal("unknown sizes are refused")
	}
}

func TestOpenNeedsAClassification(t *testing.T) {
	h := newRig(t, ModeStrict)
	if _, _, err := h.ctl.Open(ctx, "g", "s", "d"); err == nil || !strings.Contains(err.Error(), "classify first") {
		t.Fatalf("err = %v", err)
	}
}

func TestSmallNeverOpensATask(t *testing.T) {
	h := newRig(t, ModeStrict)
	_, _ = h.ctl.Classify(SizeSmall, true, "one line")
	if _, _, err := h.ctl.Open(ctx, "g", "s", "d"); !errors.Is(err, ErrSmallNoTask) {
		t.Fatalf("err = %v, want ErrSmallNoTask", err)
	}
}

func TestOneOpenTaskPerAgent(t *testing.T) {
	h := newRig(t, ModeStrict)
	h.open(t, SizeMedium)
	_, _ = h.ctl.Classify(SizeMedium, true, "again")
	if _, _, err := h.ctl.Open(ctx, "second", "s", "d"); err == nil || !strings.Contains(err.Error(), "still open") {
		t.Fatalf("err = %v, want refusal while a task is open", err)
	}
}

func TestMediumLifecycleAppliesAndRestoresTheRoute(t *testing.T) {
	h := newRig(t, ModeStrict)
	h.open(t, SizeMedium)
	if len(h.applied) != 1 || h.applied[0].Model != "medium-model" {
		t.Fatalf("route applied = %+v", h.applied)
	}
	h.ctl.RecordBash(ctx, `{"command":"go test ./..."}`, "ok", false)
	mustAdvance(t, h, PhaseVerify, "")
	mustAdvance(t, h, PhaseDeliver, "changed the api; risk: none known")
	if h.restored != 0 {
		t.Fatal("the model is restored when the task closes, not before")
	}
	mustAdvance(t, h, PhaseDone, "")
	if h.restored != 1 {
		t.Fatalf("restored %d times, want 1", h.restored)
	}
	if _, ok := h.ctl.Active(); ok {
		t.Fatal("a done task is no longer active")
	}
}

func TestLargeNeedsAnExistingPlanFile(t *testing.T) {
	h := newRig(t, ModeStrict)
	h.open(t, SizeLarge)
	h.planOK = false
	if _, err := h.ctl.Advance(ctx, PhasePlanned, "docs/tasks/1.md"); err == nil || !strings.Contains(err.Error(), "not found") {
		t.Fatalf("err = %v, want a missing plan refusal", err)
	}
	h.planOK = true
	h.docs["docs/tasks/1.md"] = validFeatureDoc
	if _, err := h.ctl.Advance(ctx, PhasePlanned, "docs/tasks/1.md"); err != nil {
		t.Fatalf("plan: %v", err)
	}
	tk, _ := h.ctl.Active()
	if tk.PlanPath != "docs/tasks/1.md" || tk.Phase != PhasePlanned {
		t.Fatalf("task after plan = %+v", tk)
	}
}

func TestBashEvidenceOnlyDuringImplementAndVerify(t *testing.T) {
	h := newRig(t, ModeStrict)
	h.open(t, SizeMedium)
	h.ctl.RecordBash(ctx, `{"command":"go test ./..."}`, strings.Repeat("q", 400), true)
	tk, _ := h.ctl.Active()
	if len(tk.Evidence) != 1 {
		t.Fatalf("evidence = %d, want only the implement command", len(tk.Evidence))
	}
	got := tk.Evidence[0]
	if got.Kind != KindCommand || !strings.Contains(got.Text, "$ go test ./...") ||
		!strings.Contains(got.Text, "exit: non-zero") {
		t.Fatalf("command evidence = %+v", got)
	}
	if kept := strings.Count(got.Text, "q"); kept != 300 {
		t.Fatalf("output must be cut to 300 characters, evidence keeps %d", kept)
	}
}

func TestAgentHintIsRecordedAsANote(t *testing.T) {
	h := newRig(t, ModeStrict)
	_, _ = h.ctl.Classify(SizeLarge, false, "design")
	_, op, err := h.ctl.Open(ctx, "build it", "s", "d")
	if err != nil {
		t.Fatal(err)
	}
	if hint := op.Hint; !strings.Contains(hint, "developer") || !strings.Contains(hint, "send_message") {
		t.Fatalf("hint = %q", hint)
	}
	tk, _ := h.ctl.Active()
	if len(tk.Evidence) != 1 || tk.Evidence[0].Kind != KindNote {
		t.Fatalf("hint evidence = %+v", tk.Evidence)
	}
}

func TestContextBlockDescribesClassificationAndTask(t *testing.T) {
	h := newRig(t, ModeStrict)
	if got := h.ctl.ContextBlock(ctx); got != "" {
		t.Fatalf("no classification gives no block, got %q", got)
	}
	_, _ = h.ctl.Classify(SizeMedium, true, "x")
	if got := h.ctl.ContextBlock(ctx); got != "[classified medium]" {
		t.Fatalf("block = %q", got)
	}
	h.ctl.Open(ctx, "ship the change", "s", "tests pass")
	block := h.ctl.ContextBlock(ctx)
	for _, want := range []string{"[task #1 · medium · implement]", "goal: ship the change", "done when: tests pass", "evidence so far: 0"} {
		if !strings.Contains(block, want) {
			t.Fatalf("block misses %q: %s", want, block)
		}
	}
}

func TestStatusShowsTheTask(t *testing.T) {
	h := newRig(t, ModeStrict)
	if !strings.Contains(h.ctl.Status(ctx), "no open task") {
		t.Fatal("status without a task says so")
	}
	h.open(t, SizeMedium)
	out := h.ctl.Status(ctx)
	for _, want := range []string{"task #1", "medium", "implement", "ship the change", "tests pass"} {
		if !strings.Contains(out, want) {
			t.Fatalf("status misses %q:\n%s", want, out)
		}
	}
}

func TestFailClosesTheTaskAndRestoresTheModel(t *testing.T) {
	h := newRig(t, ModeStrict)
	h.open(t, SizeMedium)
	if _, err := h.ctl.Fail(ctx, "user changed the scope"); err != nil {
		t.Fatal(err)
	}
	if h.restored != 1 {
		t.Fatal("failing a task restores the model")
	}
	if _, ok := h.ctl.Active(); ok {
		t.Fatal("a failed task is closed")
	}
}

func TestStartTurnKeepsClassificationOnlyWithoutATask(t *testing.T) {
	h := newRig(t, ModeStrict)
	_, _ = h.ctl.Classify(SizeMedium, true, "x")
	h.ctl.StartTurn()
	if h.ctl.Classified() != "" {
		t.Fatal("a new turn series without a task forgets the classification")
	}
	h.open(t, SizeMedium)
	h.ctl.StartTurn()
	if h.ctl.Classified() != SizeMedium {
		t.Fatal("an open task keeps the classification")
	}
}

func TestStrictGateRules(t *testing.T) {
	tests := []struct {
		name     string
		setup    func(h *rig)
		tool     string
		input    string
		risky    bool
		wantDeny bool
		wantText string
	}{
		{"reads are never gated", func(h *rig) {}, "read_file", `{"path":"a"}`, false, false, ""},
		{"no classification blocks writes", func(h *rig) {}, "write_file", `{"path":"a"}`, true, true, "classify first"},
		{"small passes", func(h *rig) { _, _ = h.ctl.Classify(SizeSmall, true, "x") }, "write_file", `{"path":"a"}`, true, false, ""},
		{"medium without a task blocks", func(h *rig) { _, _ = h.ctl.Classify(SizeMedium, true, "x") }, "bash", `{"command":"rm x"}`, true, true, "open a task first"},
		{"task_open is exempt", func(h *rig) { _, _ = h.ctl.Classify(SizeMedium, true, "x") }, "task_open", `{}`, true, false, ""},
		{"send_message is exempt", func(h *rig) { _, _ = h.ctl.Classify(SizeMedium, true, "x") }, "send_message", `{}`, true, false, ""},
		{"implement allows writes", func(h *rig) { mustOpen(h, SizeMedium) }, "write_file", `{"path":"a"}`, true, false, ""},
		{"verify does not allow writes", func(h *rig) {
			mustOpen(h, SizeMedium)
			h.ctl.RecordBash(ctx, `{"command":"go test"}`, "ok", false)
			mustAdvance(nil, h, PhaseVerify, "")
		}, "write_file", `{"path":"a"}`, true, true, "implement"},
		{"deliver releases the rule", func(h *rig) {
			mustOpen(h, SizeMedium)
			h.ctl.RecordBash(ctx, `{"command":"go test"}`, "ok", false)
			mustAdvance(nil, h, PhaseVerify, "")
			mustAdvance(nil, h, PhaseDeliver, "summary")
		}, "bash", `{"command":"rm x"}`, true, false, ""},
		{"large plan document is writable before the plan is recorded", func(h *rig) {
			mustOpen(h, SizeLarge)
		}, "write_file", `{"path":"docs/tasks/1.md"}`, true, false, ""},
		{"other paths stay blocked before the plan", func(h *rig) {
			mustOpen(h, SizeLarge)
		}, "write_file", `{"path":"internal/x.go"}`, true, true, "implement"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := newRig(t, ModeStrict)
			tt.setup(h)
			deny, reason, warn := h.ctl.Check(ctx, tt.tool, tt.input, tt.risky)
			if deny != tt.wantDeny {
				t.Fatalf("deny = %v (%s), want %v", deny, reason, tt.wantDeny)
			}
			if tt.wantText != "" && !strings.Contains(reason, tt.wantText) {
				t.Fatalf("reason = %q, want mention of %q", reason, tt.wantText)
			}
			if warn != "" {
				t.Fatalf("strict mode does not warn, got %q", warn)
			}
		})
	}
}

func TestAdvisoryModeWarnsAndAllows(t *testing.T) {
	h := newRig(t, ModeAdvisory)
	_, _ = h.ctl.Classify(SizeMedium, true, "x")
	deny, _, warn := h.ctl.Check(ctx, "write_file", `{"path":"a"}`, true)
	if deny || !strings.Contains(warn, "open a task first") {
		t.Fatalf("advisory: deny=%v warn=%q", deny, warn)
	}
}

func mustAdvance(t *testing.T, h *rig, to Phase, text string) {
	if _, err := h.ctl.Advance(ctx, to, text); err != nil {
		if t == nil {
			panic(err)
		}
		t.Fatalf("advance to %s: %v", to, err)
	}
}
