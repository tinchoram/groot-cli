package workspace

import (
	"context"
	"testing"
)

func TestClearedSessionShowsNoContext(t *testing.T) {
	// After /clear the latest session row is the cleared one, with the stored
	// size of the conversation that was dropped. The sidebar must show zero.
	f := newFixture(t, text("unused"))
	seedContext(t, f, "reader", 84_000)
	f.ws.mu.Lock()
	st, err := f.ws.storesFor(f.dir)
	f.ws.mu.Unlock()
	if err != nil {
		t.Fatal(err)
	}
	if err := st.mem.MarkCleared(context.Background(), "ctx-reader"); err != nil {
		t.Fatal(err)
	}

	rows, err := f.ws.Contexts(context.Background(), false)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 {
		t.Fatalf("rows = %+v, want the one agent of the active space", rows)
	}
	if r := rows[0]; r.Tokens != 0 || r.Percent != 0 || r.Triggers {
		t.Fatalf("cleared session row = %+v, want no context", r)
	}
}
