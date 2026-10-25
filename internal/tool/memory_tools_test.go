package tool

import (
	"context"
	"strings"
	"testing"

	"github.com/tinchoram/groot-cli/internal/memory"
)

// recordingStore captures what the memory tools write and return.
type recordingStore struct {
	memory.NoMemory
	saved    []memory.Entry
	results  []memory.Entry
	gotQ     string
	gotLim   int
	gotAgent string
}

func (r *recordingStore) Remember(_ context.Context, e memory.Entry) (memory.Result, error) {
	r.saved = append(r.saved, e)
	return memory.Result{Outcome: memory.OutcomeCreated}, nil
}

func (r *recordingStore) Recall(_ context.Context, rd memory.Reader, q string, limit int) ([]memory.Entry, error) {
	r.gotQ, r.gotLim, r.gotAgent = q, limit, rd.Agent
	return r.results, nil
}

func TestRememberToolStoresEntry(t *testing.T) {
	st := &recordingStore{}
	rt := NewRemember(st, "payments-agent")
	if rt.Risk() != RiskRead {
		t.Fatal("remember is a local memory write, classed RiskRead like the other memory tools")
	}
	out, isErr := rt.Execute(context.Background(), mustJSON(t, map[string]string{
		"kind": "runbook", "text": "Drain node before kernel upgrade",
	}))
	if isErr {
		t.Fatalf("remember failed: %s", out)
	}
	if len(st.saved) != 1 {
		t.Fatalf("expected one saved entry, got %d", len(st.saved))
	}
	e := st.saved[0]
	if e.Kind != memory.KindRunbook || e.Scope != memory.ScopeAgent || e.Agent != "payments-agent" || e.Source != "tool:remember" {
		t.Fatalf("unexpected entry: %+v", e)
	}
}

func TestRememberToolRejectsBadKind(t *testing.T) {
	out, isErr := NewRemember(&recordingStore{}, "a").Execute(context.Background(), mustJSON(t, map[string]string{"kind": "gossip", "text": "x"}))
	if !isErr {
		t.Fatalf("bad kind must be an error, got %q", out)
	}
}

func TestRecallToolFormatsResults(t *testing.T) {
	st := &recordingStore{results: []memory.Entry{{Kind: memory.KindFact, Text: "ingress uses nginx class"}}}
	rc := NewRecall(st, "payments-agent")
	out, isErr := rc.Execute(context.Background(), mustJSON(t, map[string]any{"query": "ingress", "limit": 3}))
	if isErr {
		t.Fatalf("recall failed: %s", out)
	}
	if st.gotQ != "ingress" || st.gotLim != 3 || st.gotAgent != "payments-agent" {
		t.Fatalf("store got query=%q limit=%d", st.gotQ, st.gotLim)
	}
	if !strings.Contains(out, "[fact] ingress uses nginx class") {
		t.Fatalf("unexpected output: %q", out)
	}
}

func TestRecallToolNoResults(t *testing.T) {
	out, isErr := NewRecall(&recordingStore{}, "a").Execute(context.Background(), mustJSON(t, map[string]string{"query": "nothing"}))
	if isErr || !strings.Contains(out, "no memories") {
		t.Fatalf("got (%q, %v)", out, isErr)
	}
}
