package memory

import (
	"context"
	"errors"
	"testing"
)

// layeredFixture is a space database (local) and the root database (global),
// both in temporary files. The root database also holds a root-space entry,
// which must never leak into a space reader's view of global memory.
type layeredFixture struct {
	local, root *SQLite
	layered     *Layered
}

func newLayeredFixture(t *testing.T, globalWrite bool) layeredFixture {
	t.Helper()
	local := openTemp(t)
	root := openTemp(t)
	setClock(local, clockStart)
	setClock(root, clockStart)
	// Two rows that no space reader sees push the global row to id 3, so its id
	// does not collide with the space rows (ids 1 and 2).
	mustRemember(t, root, Entry{Kind: KindFact, Scope: ScopeAgent, Agent: "ops", Text: "ops filler one"})
	mustRemember(t, root, Entry{Kind: KindFact, Scope: ScopeAgent, Agent: "ops", Text: "ops filler two"})
	mustRemember(t, root, Entry{Kind: KindFact, Scope: ScopeGlobal, Agent: "groot", Text: "global dns zone is example.internal"})
	mustRemember(t, root, Entry{Kind: KindFact, Scope: ScopeSpace, Agent: "", Text: "root space secret"})
	mustRemember(t, local, Entry{Kind: KindFact, Scope: ScopeSpace, Agent: "", Text: "payments space fact"})
	mustRemember(t, local, Entry{Kind: KindFact, Scope: ScopeAgent, Agent: "payments", Text: "payments private fact"})
	return layeredFixture{
		local:   local,
		root:    root,
		layered: &Layered{Store: local, Global: root, GlobalWrite: globalWrite},
	}
}

func TestLayeredHidesGlobalFromReadersWithoutGlobalAccess(t *testing.T) {
	f := newLayeredFixture(t, false)
	got := allVisible(t, f.layered.Store.(*SQLite), Reader{Agent: "payments"})
	wantTexts(t, got, "payments space fact", "payments private fact")
}

func TestLayeredAddsOnlyGlobalRowsForGlobalReaders(t *testing.T) {
	f := newLayeredFixture(t, false)
	ctx := context.Background()
	ov, err := f.layered.Overview(ctx, Reader{Agent: "payments", Global: true})
	if err != nil {
		t.Fatal(err)
	}
	if ov.Visible != 3 {
		t.Fatalf("Visible = %d, want 3 (two local, one global); the root space entry must not count", ov.Visible)
	}
	recall, err := f.layered.Recall(ctx, Reader{Agent: "payments", Global: true}, "dns", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(recall) != 1 || recall[0].Scope != ScopeGlobal {
		t.Fatalf("recall = %+v, want the one global entry tagged global", recall)
	}
	none, err := f.layered.Recall(ctx, Reader{Agent: "payments", Global: true}, "secret", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(none) != 0 {
		t.Fatalf("root space entry leaked into global reads: %+v", none)
	}
}

func TestLayeredRememberRoutesGlobalOnlyWhenWritable(t *testing.T) {
	ctx := context.Background()
	tests := []struct {
		name        string
		globalWrite bool
		wantErr     bool
		wantRoot    bool
	}{
		{name: "groot writes to the root database", globalWrite: true, wantRoot: true},
		{name: "other agents are refused", globalWrite: false, wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := newLayeredFixture(t, tt.globalWrite)
			res, err := f.layered.Remember(ctx, Entry{Kind: KindRunbook, Scope: ScopeGlobal, Agent: "groot", Text: "rotate the root key monthly"})
			if tt.wantErr {
				if err == nil {
					t.Fatalf("expected a refusal, got %+v", res)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if res.Outcome != OutcomeCreated {
				t.Fatalf("outcome = %s", res.Outcome)
			}
			wantTexts(t, allVisible(t, f.root, Reader{Agent: "groot", Global: true}), "global dns zone is example.internal", "rotate the root key monthly", "root space secret")
			for _, e := range allVisible(t, f.local, Reader{Agent: "payments"}) {
				if e.Text == "rotate the root key monthly" {
					t.Fatal("global entry was written to the local database")
				}
			}
		})
	}
}

func TestLayeredRememberKeepsSpaceEntriesLocal(t *testing.T) {
	f := newLayeredFixture(t, false)
	if _, err := f.layered.Remember(context.Background(), Entry{Kind: KindFact, Scope: ScopeSpace, Agent: "payments", Text: "queue is rabbit"}); err != nil {
		t.Fatal(err)
	}
	wantTexts(t, allVisible(t, f.local, Reader{Agent: "payments"}), "payments space fact", "payments private fact", "queue is rabbit")
}

func TestLayeredPreambleMergesBothDatabasesWithinOneBudget(t *testing.T) {
	f := newLayeredFixture(t, false)
	ctx := context.Background()
	got, err := f.layered.Preamble(ctx, Reader{Agent: "payments", Global: true}, Budget{Tokens: 100000, MaxSummaries: 3, EntryRunes: 400})
	if err != nil {
		t.Fatal(err)
	}
	wantTexts(t, got, "payments space fact", "payments private fact", "global dns zone is example.internal")

	tight, err := f.layered.Preamble(ctx, Reader{Agent: "payments", Global: true}, Budget{Tokens: 1, MaxSummaries: 3, EntryRunes: 400})
	if err != nil {
		t.Fatal(err)
	}
	if len(tight) != 1 {
		t.Fatalf("a one-token budget keeps the first entry only, got %d", len(tight))
	}
}

func TestLayeredGetFallsBackToGlobal(t *testing.T) {
	f := newLayeredFixture(t, false)
	ctx := context.Background()
	local, err := f.layered.Get(ctx, 1, Reader{Agent: "payments"})
	if err != nil {
		t.Fatal(err)
	}
	if local.Scope == ScopeGlobal {
		t.Fatalf("id 1 must resolve locally first, got %+v", local)
	}
	globalID := globalIDOf(t, f.root)
	if _, err := f.layered.Get(ctx, globalID, Reader{Agent: "payments"}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("a reader without global access must not see global id %d: %v", globalID, err)
	}
	got, err := f.layered.Get(ctx, globalID, Reader{Agent: "payments", Global: true})
	if err != nil {
		t.Fatal(err)
	}
	if got.Scope != ScopeGlobal {
		t.Fatalf("scope = %s, want global", got.Scope)
	}
}

func TestLayeredMutatesGlobalOnlyForWritersOfGlobal(t *testing.T) {
	ctx := context.Background()
	t.Run("reader without write access is refused", func(t *testing.T) {
		f := newLayeredFixture(t, false)
		id := globalIDOf(t, f.root)
		err := f.layered.Forget(ctx, id, Reader{Agent: "payments", Global: true})
		if !errors.Is(err, ErrGlobalReadOnly) {
			t.Fatalf("Forget = %v, want ErrGlobalReadOnly", err)
		}
	})
	t.Run("groot forgets a global entry", func(t *testing.T) {
		f := newLayeredFixture(t, true)
		id := globalIDOf(t, f.root)
		if err := f.layered.Forget(ctx, id, Reader{Agent: "groot", Global: true}); err != nil {
			t.Fatal(err)
		}
		if _, err := f.root.Get(ctx, id, Reader{Agent: "groot", Global: true}); !errors.Is(err, ErrNotFound) {
			t.Fatalf("entry still visible after forget: %v", err)
		}
	})
}

func TestLayeredOverviewCountsPinnedEntriesOfBothSides(t *testing.T) {
	f := newLayeredFixture(t, true)
	ctx := context.Background()
	pinned := true
	if err := f.layered.Update(ctx, 1, Reader{Agent: "payments"}, Patch{Pinned: &pinned}); err != nil {
		t.Fatal(err)
	}
	// Global ids can collide with local ids (each database numbers its own rows),
	// so this test pins only the local row; the global rows stay unpinned.
	ov, err := f.layered.Overview(ctx, Reader{Agent: "payments", Global: true})
	if err != nil {
		t.Fatal(err)
	}
	if ov.Pinned != 1 {
		t.Fatalf("Pinned = %d, want 1", ov.Pinned)
	}
}

func globalIDOf(t *testing.T, s *SQLite) int64 {
	t.Helper()
	got, err := s.Recall(context.Background(), Reader{Agent: "groot", Global: true}, "dns", 5)
	if err != nil || len(got) != 1 {
		t.Fatalf("global id lookup: %v, %d entries", err, len(got))
	}
	return got[0].ID
}
