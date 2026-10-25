package tool

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/tinchoram/groot-cli/internal/memory"
)

// recordingStore captures what the memory tools write and return.
type recordingStore struct {
	memory.NoMemory
	saved     []memory.Entry
	results   []memory.Entry
	gotQ      string
	gotLim    int
	gotReader memory.Reader
	outcome   memory.Result
	err       error
	entry     memory.Entry
	forgot    []int64
	patches   []memory.Patch
	pinned    map[int64]bool
}

func (r *recordingStore) Remember(_ context.Context, e memory.Entry) (memory.Result, error) {
	r.saved = append(r.saved, e)
	if r.err != nil {
		return memory.Result{}, r.err
	}
	if r.outcome.Outcome == "" {
		return memory.Result{ID: 7, Outcome: memory.OutcomeCreated}, nil
	}
	return r.outcome, nil
}

func (r *recordingStore) Recall(_ context.Context, rd memory.Reader, q string, limit int) ([]memory.Entry, error) {
	r.gotQ, r.gotLim, r.gotReader = q, limit, rd
	return r.results, r.err
}

func (r *recordingStore) Get(_ context.Context, id int64, rd memory.Reader) (memory.Entry, error) {
	r.gotReader = rd
	if r.err != nil {
		return memory.Entry{}, r.err
	}
	return r.entry, nil
}

func (r *recordingStore) Forget(_ context.Context, id int64, _ memory.Reader) error {
	if r.err != nil {
		return r.err
	}
	r.forgot = append(r.forgot, id)
	return nil
}

func (r *recordingStore) Update(_ context.Context, id int64, _ memory.Reader, p memory.Patch) error {
	if r.err != nil {
		return r.err
	}
	r.patches = append(r.patches, p)
	return nil
}

func (r *recordingStore) SetPinned(_ context.Context, id int64, _ memory.Reader, pinned bool) error {
	if r.pinned == nil {
		r.pinned = map[int64]bool{}
	}
	r.pinned[id] = pinned
	return r.err
}

func access(st memory.Store, agent string) MemoryAccess {
	return MemoryAccess{Store: st, Reader: memory.Reader{Agent: agent}}
}

func TestRememberDefaultsToSpaceAndStoresTitleTopicAndTaint(t *testing.T) {
	st := &recordingStore{}
	acc := access(st, "payments-agent")
	acc.Tainted = func() bool { return true }
	rt := NewRemember(acc)
	if rt.Risk() != RiskRead {
		t.Fatal("remember is a local memory write, classed RiskRead like the other memory tools")
	}
	out, isErr := rt.Execute(context.Background(), mustJSON(t, map[string]string{
		"kind": "runbook", "text": "Drain node before kernel upgrade", "title": "Kernel", "topic_key": "kernel-upgrade",
	}))
	if isErr {
		t.Fatalf("remember failed: %s", out)
	}
	if len(st.saved) != 1 {
		t.Fatalf("expected one saved entry, got %d", len(st.saved))
	}
	e := st.saved[0]
	if e.Kind != memory.KindRunbook || e.Scope != memory.ScopeSpace || e.Agent != "payments-agent" || e.Source != "tool:remember" {
		t.Fatalf("unexpected entry: %+v", e)
	}
	if e.Title != "Kernel" || e.TopicKey != "kernel-upgrade" || !e.Tainted {
		t.Fatalf("title, topic key or taint lost: %+v", e)
	}
}

func TestRememberWithoutTaintCallbackIsNotTainted(t *testing.T) {
	st := &recordingStore{}
	if out, isErr := NewRemember(access(st, "a")).Execute(context.Background(), mustJSON(t, map[string]string{"kind": "fact", "text": "x"})); isErr {
		t.Fatalf("remember failed: %s", out)
	}
	if st.saved[0].Tainted {
		t.Fatal("an entry is tainted only when the turn read untrusted output")
	}
}

func TestRememberRefusesGlobalScopeUnlessWriterOfGlobal(t *testing.T) {
	tests := []struct {
		name        string
		canWrite    bool
		wantRefused bool
	}{
		{name: "agent other than groot is refused", canWrite: false, wantRefused: true},
		{name: "groot may write global", canWrite: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			st := &recordingStore{}
			acc := access(st, "payments-agent")
			acc.CanWriteGlobal = tt.canWrite
			out, isErr := NewRemember(acc).Execute(context.Background(), mustJSON(t, map[string]string{"kind": "fact", "text": "x", "scope": "global"}))
			if tt.wantRefused {
				if !isErr || !strings.Contains(out, "groot") || len(st.saved) != 0 {
					t.Fatalf("expected a refusal naming groot, got (%q, %v) saved=%d", out, isErr, len(st.saved))
				}
				return
			}
			if isErr || st.saved[0].Scope != memory.ScopeGlobal {
				t.Fatalf("groot write failed: (%q, %v)", out, isErr)
			}
		})
	}
}

func TestRememberReportsTheOutcome(t *testing.T) {
	tests := []struct {
		name    string
		result  memory.Result
		want    string
		wantErr bool
	}{
		{name: "created", result: memory.Result{ID: 12, Outcome: memory.OutcomeCreated}, want: "remembered #12 (created)"},
		{name: "updated", result: memory.Result{ID: 12, Outcome: memory.OutcomeUpdated}, want: "updated #12 (revision"},
		{name: "duplicate", result: memory.Result{ID: 12, Outcome: memory.OutcomeDuplicate}, want: "duplicate of #12"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			st := &recordingStore{outcome: tt.result, entry: memory.Entry{ID: 12, RevisionCount: 3}}
			out, isErr := NewRemember(access(st, "a")).Execute(context.Background(), mustJSON(t, map[string]string{"kind": "fact", "text": "x"}))
			if isErr != tt.wantErr || !strings.Contains(out, tt.want) {
				t.Fatalf("got (%q, %v), want it to contain %q", out, isErr, tt.want)
			}
		})
	}
}

func TestRememberRejectsSessionSummaryBadKindScopeAndEmptyText(t *testing.T) {
	tests := []struct {
		name  string
		input map[string]string
	}{
		{name: "session summary is written by the system only", input: map[string]string{"kind": "session_summary", "text": "x"}},
		{name: "unknown kind", input: map[string]string{"kind": "gossip", "text": "x"}},
		{name: "unknown scope", input: map[string]string{"kind": "fact", "text": "x", "scope": "project"}},
		{name: "empty text", input: map[string]string{"kind": "fact", "text": "  "}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			st := &recordingStore{}
			out, isErr := NewRemember(access(st, "a")).Execute(context.Background(), mustJSON(t, tt.input))
			if !isErr || len(st.saved) != 0 {
				t.Fatalf("must be refused, got (%q, %v) saved=%d", out, isErr, len(st.saved))
			}
		})
	}
}

func TestRememberCallsChangedOnlyAfterASuccessfulWrite(t *testing.T) {
	st := &recordingStore{err: errors.New("disk full")}
	calls := 0
	acc := access(st, "a")
	acc.Changed = func() { calls++ }
	if _, isErr := NewRemember(acc).Execute(context.Background(), mustJSON(t, map[string]string{"kind": "fact", "text": "x"})); !isErr {
		t.Fatal("a store error must be an error result")
	}
	if calls != 0 {
		t.Fatalf("Changed ran %d times after a failed write", calls)
	}
	st.err = nil
	if _, isErr := NewRemember(acc).Execute(context.Background(), mustJSON(t, map[string]string{"kind": "fact", "text": "x"})); isErr {
		t.Fatal("write failed")
	}
	if calls != 1 {
		t.Fatalf("Changed ran %d times after a successful write, want 1", calls)
	}
}

func TestRecallFormatsCompactLinesWithIDsAndAHint(t *testing.T) {
	long := strings.Repeat("ö", 200)
	st := &recordingStore{results: []memory.Entry{
		{ID: 12, Kind: memory.KindFact, Scope: memory.ScopeSpace, Title: "Ingress", Text: "uses nginx class"},
		{ID: 13, Kind: memory.KindRunbook, Scope: memory.ScopeGlobal, Text: long},
	}}
	rc := NewRecall(access(st, "payments-agent"))
	out, isErr := rc.Execute(context.Background(), mustJSON(t, map[string]any{"query": "ingress", "limit": 3}))
	if isErr {
		t.Fatalf("recall failed: %s", out)
	}
	if st.gotQ != "ingress" || st.gotLim != 3 || st.gotReader.Agent != "payments-agent" {
		t.Fatalf("store got query=%q limit=%d reader=%+v", st.gotQ, st.gotLim, st.gotReader)
	}
	if !strings.Contains(out, "#12 [fact|space] Ingress — uses nginx class") {
		t.Fatalf("unexpected line: %q", out)
	}
	if !strings.Contains(out, "#13 [runbook|global] ") || strings.Contains(out, long) {
		t.Fatalf("long text must be cut to 160 runes: %q", out)
	}
	if !strings.Contains(out, "memory_get") {
		t.Fatalf("output must point at memory_get: %q", out)
	}
}

func TestRecallNoResultsAndUntrustedOutput(t *testing.T) {
	rc := NewRecall(access(&recordingStore{}, "a"))
	out, isErr := rc.Execute(context.Background(), mustJSON(t, map[string]string{"query": "nothing"}))
	if isErr || !strings.Contains(out, "no memories") {
		t.Fatalf("got (%q, %v)", out, isErr)
	}
	if !rc.Untrusted() {
		t.Fatal("recall output carries remembered text and must stay untrusted")
	}
}

func TestMemoryGetReturnsTheFullEntry(t *testing.T) {
	st := &recordingStore{entry: memory.Entry{ID: 4, Kind: memory.KindDecision, Scope: memory.ScopeSpace, Title: "Use Helm", Text: "charts stay in the repo", Source: "tool:remember"}}
	out, isErr := NewMemoryGet(access(st, "a")).Execute(context.Background(), mustJSON(t, map[string]int{"id": 4}))
	if isErr {
		t.Fatalf("memory_get failed: %s", out)
	}
	if !strings.Contains(out, "#4 [decision|space] Use Helm") || !strings.Contains(out, "charts stay in the repo") {
		t.Fatalf("unexpected output: %q", out)
	}
}

func TestMemoryGetReportsMissingEntries(t *testing.T) {
	st := &recordingStore{err: memory.ErrNotFound}
	out, isErr := NewMemoryGet(access(st, "a")).Execute(context.Background(), mustJSON(t, map[string]int{"id": 9}))
	if !isErr || !strings.Contains(out, "#9") {
		t.Fatalf("got (%q, %v)", out, isErr)
	}
}

func TestForgetSoftDeletesAndNotifies(t *testing.T) {
	st := &recordingStore{}
	calls := 0
	acc := access(st, "a")
	acc.Changed = func() { calls++ }
	out, isErr := NewForget(acc).Execute(context.Background(), mustJSON(t, map[string]int{"id": 5}))
	if isErr || out != "forgot #5" {
		t.Fatalf("got (%q, %v)", out, isErr)
	}
	if len(st.forgot) != 1 || st.forgot[0] != 5 || calls != 1 {
		t.Fatalf("forgot=%v calls=%d", st.forgot, calls)
	}
	if NewForget(acc).Risk() != RiskWrite {
		t.Fatal("forget changes memory and is RiskWrite")
	}
}

func TestUpdateMemoryAppliesOnlyTheFieldsGiven(t *testing.T) {
	st := &recordingStore{}
	calls := 0
	acc := access(st, "a")
	acc.Changed = func() { calls++ }
	ut := NewUpdateMemory(acc)
	if ut.Risk() != RiskWrite {
		t.Fatal("update_memory changes memory and is RiskWrite")
	}
	out, isErr := ut.Execute(context.Background(), mustJSON(t, map[string]any{"id": 3, "pinned": true, "text": "new text"}))
	if isErr {
		t.Fatalf("update failed: %s", out)
	}
	p := st.patches[0]
	if p.Pinned == nil || !*p.Pinned || p.Text == nil || *p.Text != "new text" || p.Title != nil || p.TopicKey != nil {
		t.Fatalf("patch = %+v", p)
	}
	if st.pinned[3] {
		t.Fatal("pinned goes through the patch, not SetPinned")
	}
	if calls != 1 {
		t.Fatalf("Changed ran %d times", calls)
	}
}

func TestUpdateMemoryNeedsAField(t *testing.T) {
	st := &recordingStore{}
	out, isErr := NewUpdateMemory(access(st, "a")).Execute(context.Background(), mustJSON(t, map[string]int{"id": 3}))
	if !isErr || len(st.patches) != 0 {
		t.Fatalf("an empty update must be refused, got (%q, %v)", out, isErr)
	}
}
