package agent

import (
	"context"
	"testing"

	"github.com/tinchoram/groot-cli/internal/memory"
	"github.com/tinchoram/groot-cli/internal/provider"
)

// startRecorder keeps the sessions the agent starts.
type startRecorder struct {
	memory.NoMemory
	started []memory.Session
}

func (r *startRecorder) StartSession(_ context.Context, s memory.Session) error {
	r.started = append(r.started, s)
	return nil
}

func TestSessionAfterClearNamesTheClearedOneAsParent(t *testing.T) {
	rec := &startRecorder{}
	a := newAgent(provider.NewMock(textResponse("first"), textResponse("second"), textResponse("third")), registry(t), &recordGate{allow: true})
	a.Memory = rec

	if _, err := a.Send(context.Background(), "one"); err != nil {
		t.Fatal(err)
	}
	cleared := a.SessionID()
	if id, _, _ := a.Detach(); id != cleared {
		t.Fatalf("Detach returned %q, want %q", id, cleared)
	}
	if _, err := a.Send(context.Background(), "two"); err != nil {
		t.Fatal(err)
	}

	if len(rec.started) != 2 {
		t.Fatalf("started %d sessions, want 2", len(rec.started))
	}
	if rec.started[0].ParentID != "" {
		t.Fatalf("the first session has no parent, got %q", rec.started[0].ParentID)
	}
	if rec.started[1].ParentID != cleared {
		t.Fatalf("parent = %q, want the cleared session %q", rec.started[1].ParentID, cleared)
	}

	if _, err := a.Send(context.Background(), "three"); err != nil {
		t.Fatal(err)
	}
	if len(rec.started) != 2 {
		t.Fatal("a later turn in the same session must not start another one")
	}
}
