package flow

import (
	"strings"
	"testing"
)

const nextLargeLine = "next: task_open, write docs/tasks/<id>.md with ## Specs (S1. quoting the user), ## Tasks (T1. (S1) …) and ## Log (L1. the original request), task_advance planned with its path, implement, verify each spec with task_verify, deliver with a note"

func TestClassifyMatchesTheSizeToTheResumeTest(t *testing.T) {
	tests := []struct {
		name      string
		size      Size
		resumable bool
		wantErr   string
	}{
		{"small can be resumed from the diff", SizeSmall, true, ""},
		{"small that cannot is large work", SizeSmall, false,
			"work that cannot be resumed from the request plus the diff is large: classify large and write the feature document"},
		{"medium can be resumed from the diff", SizeMedium, true, ""},
		{"medium that cannot is large work", SizeMedium, false,
			"work that cannot be resumed from the request plus the diff is large: classify large and write the feature document"},
		{"large cannot be resumed from the diff", SizeLarge, false, ""},
		{"large that can be resumed is medium work", SizeLarge, true,
			"large work is the work that cannot be resumed from the diff: answer resumable_from_diff false, or classify medium"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := newRig(t, ModeStrict)
			_, err := h.ctl.Classify(tt.size, tt.resumable, "reason")
			if tt.wantErr == "" {
				if err != nil {
					t.Fatalf("Classify: %v", err)
				}
				return
			}
			if err == nil || err.Error() != tt.wantErr {
				t.Fatalf("err = %v, want %q", err, tt.wantErr)
			}
			if h.ctl.Classified() != "" {
				t.Fatal("a refused classification changes nothing")
			}
		})
	}
}

func TestClassifyLargeNamesTheFeatureDocument(t *testing.T) {
	h := newRig(t, ModeStrict)
	text, err := h.ctl.Classify(SizeLarge, false, "design")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(text, nextLargeLine) {
		t.Fatalf("large next line differs:\n%s", text)
	}
}

func TestPlannedParsesTheFeatureDocumentAndStoresTheSpecs(t *testing.T) {
	h := newRig(t, ModeStrict)
	h.docs["docs/tasks/1.md"] = validFeatureDoc
	h.open(t, SizeLarge)
	mustAdvance(t, h, PhasePlanned, "docs/tasks/1.md")

	tk, _ := h.ctl.Active()
	if len(tk.Specs) != 2 || tk.Specs[0].Text != "split the billing module in two" || tk.Specs[1].N != 2 {
		t.Fatalf("active specs = %+v", tk.Specs)
	}
	if tk.Specs[0].Status != VerdictPending {
		t.Fatalf("a new spec is pending, got %q", tk.Specs[0].Status)
	}
	stored, err := h.store.Specs(ctx, tk.ID)
	if err != nil || len(stored) != 2 {
		t.Fatalf("stored specs = %+v, %v", stored, err)
	}
}

func TestPlannedRefusesAPlainPlan(t *testing.T) {
	h := newRig(t, ModeStrict)
	h.docs["docs/tasks/1.md"] = "# Plan\n\nWe will split it somehow.\n"
	h.open(t, SizeLarge)
	_, err := h.ctl.Advance(ctx, PhasePlanned, "docs/tasks/1.md")
	if err == nil || !strings.HasPrefix(err.Error(), `plan "docs/tasks/1.md" is not a feature document: `) {
		t.Fatalf("err = %v, want a feature document refusal", err)
	}
	if tk, _ := h.ctl.Active(); tk.Phase != PhaseClassified {
		t.Fatalf("a refused plan keeps the phase, got %s", tk.Phase)
	}
}

// verifyingLarge is a large task in verify whose plan has two specs.
func verifyingLarge(t *testing.T) *rig {
	t.Helper()
	h := newRig(t, ModeStrict)
	h.docs["docs/tasks/1.md"] = validFeatureDoc
	h.open(t, SizeLarge)
	mustAdvance(t, h, PhasePlanned, "docs/tasks/1.md")
	mustAdvance(t, h, PhaseImplement, "")
	h.ctl.RecordBash(ctx, `{"command":"go test ./billing/..."}`, "ok", false)
	mustAdvance(t, h, PhaseVerify, "")
	return h
}

func TestVerifyGuards(t *testing.T) {
	tests := []struct {
		name    string
		setup   func(t *testing.T) *rig
		spec    int
		wantErr string
	}{
		{"no open task", func(t *testing.T) *rig { return newRig(t, ModeStrict) }, 1, "no open task"},
		{"medium tasks do not verify per spec", func(t *testing.T) *rig {
			h := newRig(t, ModeStrict)
			h.open(t, SizeMedium)
			return h
		}, 1, "task_verify needs a large task in verify"},
		{"large in implement does not verify yet", func(t *testing.T) *rig {
			h := newRig(t, ModeStrict)
			h.docs["docs/tasks/1.md"] = validFeatureDoc
			h.open(t, SizeLarge)
			mustAdvance(t, h, PhasePlanned, "docs/tasks/1.md")
			mustAdvance(t, h, PhaseImplement, "")
			return h
		}, 1, "task_verify needs a large task in verify"},
		{"unknown spec", func(t *testing.T) *rig { return verifyingLarge(t) }, 9,
			"spec S9 does not exist: specs are S1..S2"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := tt.setup(t)
			_, err := h.ctl.Verify(ctx, tt.spec, VerdictPass, "ran the tests")
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("err = %v, want mention of %q", err, tt.wantErr)
			}
		})
	}
}

func TestVerifyRecordsTheVerdictAndCountsThePasses(t *testing.T) {
	h := verifyingLarge(t)
	text, err := h.ctl.Verify(ctx, 1, VerdictPass, "go test ./billing: ok")
	if err != nil || text != "S1 pass · specs 1/2 pass" {
		t.Fatalf("pass = %q, %v", text, err)
	}
	text, err = h.ctl.Verify(ctx, 2, VerdictFail, "totals are off by one")
	if err != nil || text != "S2 fail · specs 1/2 pass: go back with task_advance implement and the reason" {
		t.Fatalf("fail = %q, %v", text, err)
	}
	tk, _ := h.ctl.Active()
	if tk.Specs[1].Status != VerdictFail || tk.Specs[1].Note != "totals are off by one" {
		t.Fatalf("spec S2 = %+v", tk.Specs[1])
	}
	last := tk.Evidence[len(tk.Evidence)-1]
	if last.Kind != KindVerdict || last.Text != "S2 fail: totals are off by one" {
		t.Fatalf("verdict evidence = %+v", last)
	}
	stored, _ := h.store.Specs(ctx, tk.ID)
	if stored[0].Status != VerdictPass || stored[1].Status != VerdictFail {
		t.Fatalf("stored verdicts = %+v", stored)
	}
}

func TestDeliverNeedsEverySpecToPass(t *testing.T) {
	tests := []struct {
		name    string
		verdict func(h *rig)
		wantErr string
	}{
		{"pending spec", func(h *rig) {
			_, _ = h.ctl.Verify(ctx, 1, VerdictPass, "ok")
		}, "deliver needs a pass for every spec: pending S2"},
		{"failed spec", func(h *rig) {
			_, _ = h.ctl.Verify(ctx, 1, VerdictPass, "ok")
			_, _ = h.ctl.Verify(ctx, 2, VerdictFail, "wrong")
		}, "deliver is blocked: S2 failed; go back with task_advance implement <reason>"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := verifyingLarge(t)
			tt.verdict(h)
			_, err := h.ctl.Advance(ctx, PhaseDeliver, "changed billing; risk: none known")
			if err == nil || err.Error() != tt.wantErr {
				t.Fatalf("err = %v, want %q", err, tt.wantErr)
			}
		})
	}
}

func TestDeliverWithEveryPassAndTheDeliveryNote(t *testing.T) {
	h := verifyingLarge(t)
	_, _ = h.ctl.Verify(ctx, 1, VerdictPass, "ok")
	_, _ = h.ctl.Verify(ctx, 2, VerdictPass, "ok")
	mustAdvance(t, h, PhaseDeliver, "changed billing; risk: none known")
	if tk, _ := h.ctl.Active(); tk.Phase != PhaseDeliver {
		t.Fatalf("phase = %s, want deliver", tk.Phase)
	}
}

func TestReturningToImplementResetsTheVerdicts(t *testing.T) {
	h := verifyingLarge(t)
	_, _ = h.ctl.Verify(ctx, 1, VerdictPass, "ok")
	_, _ = h.ctl.Verify(ctx, 2, VerdictFail, "totals are off by one")
	mustAdvance(t, h, PhaseImplement, "totals are off by one")

	tk, _ := h.ctl.Active()
	for _, s := range tk.Specs {
		if s.Status != VerdictPending || s.Note != "" {
			t.Fatalf("spec S%d after the return = %+v, want pending", s.N, s)
		}
	}
	stored, _ := h.store.Specs(ctx, tk.ID)
	for _, s := range stored {
		if s.Status != VerdictPending {
			t.Fatalf("stored spec S%d kept its verdict: %+v", s.N, s)
		}
	}
	var kept bool
	for _, e := range tk.Evidence {
		kept = kept || (e.Kind == KindVerdict && e.Text == "S2 fail: totals are off by one")
	}
	if !kept {
		t.Fatal("the fail stays as evidence after the return")
	}
}

func TestPlanPathStaysWritableUntilTheTaskCloses(t *testing.T) {
	tests := []struct {
		name   string
		setup  func(h *rig)
		path   string
		denied bool
	}{
		{"plan path in verify", func(h *rig) {}, "docs/tasks/1.md", false},
		{"other file in verify", func(h *rig) {}, "billing/x.go", true},
		{"plan path in deliver", deliverReached, "docs/tasks/1.md", false},
		// Deliver releases the writes until the next classification, as before.
		{"other file in deliver is released", deliverReached, "billing/x.go", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := verifyingLarge(t)
			tt.setup(h)
			deny, _, _ := h.ctl.Check(ctx, "write_file", `{"path":"`+tt.path+`","content":"x"}`, true)
			if deny != tt.denied {
				t.Fatalf("deny = %v, want %v", deny, tt.denied)
			}
		})
	}
}

// deliverReached moves a verifying large task to deliver, for table setups.
func deliverReached(h *rig) {
	_, _ = h.ctl.Verify(ctx, 1, VerdictPass, "ok")
	_, _ = h.ctl.Verify(ctx, 2, VerdictPass, "ok")
	_, _ = h.ctl.Advance(ctx, PhaseDeliver, "note")
}

func TestContextBlockAndStatusShowTheSpecs(t *testing.T) {
	h := verifyingLarge(t)
	_, _ = h.ctl.Verify(ctx, 1, VerdictPass, "ok")
	if block := h.ctl.ContextBlock(ctx); !strings.Contains(block, "· specs: 1/2 pass") {
		t.Fatalf("block = %q", block)
	}
	status := h.ctl.Status(ctx)
	for _, want := range []string{"S1 pass — split the billing module in two", "S2 pending — invoices keep their totals"} {
		if !strings.Contains(status, want) {
			t.Fatalf("status misses %q:\n%s", want, status)
		}
	}
}

func TestHandoverBodyListsTheSpecs(t *testing.T) {
	tk := Task{ID: 7, Size: SizeLarge, Goal: "split", Scope: "payments", DoneWhen: "tests pass",
		PlanPath: "docs/tasks/7.md", Agent: "reviewer", Space: "payments",
		Specs: []SpecState{{Spec: Spec{N: 1, Text: "split the module"}}, {Spec: Spec{N: 2, Text: "keep the totals"}}}}
	body := HandoverBody(tk, "payments/reviewer")
	for _, want := range []string{"Spec S1: split the module", "Spec S2: keep the totals"} {
		if !strings.Contains(body, want) {
			t.Fatalf("handover body misses %q:\n%s", want, body)
		}
	}
}

func TestSpecLineCutsTheTextToEightyRunes(t *testing.T) {
	long := strings.Repeat("x", 120)
	got := SpecState{Spec: Spec{N: 3, Text: long}}.Line()
	if got != "S3 pending — "+strings.Repeat("x", 80)+"…" {
		t.Fatalf("line = %q", got)
	}
}
