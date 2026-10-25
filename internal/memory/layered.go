package memory

import (
	"context"
	"errors"
	"math"
	"sort"
)

// ErrGlobalReadOnly is returned when a reader tries to change a global entry
// it may read but not write. Only the writer of global memory (groot) may
// change it.
var ErrGlobalReadOnly = errors.New("global memory can only be changed by groot")

// Layered is the store of a space that is not the root space. Sessions,
// messages and tasks, and the space and agent memories, live in the space
// database (the embedded Store). Global memory lives in the root database
// (Global). Id operations try the space database first, then the global one,
// so an id that exists in both resolves to the space entry.
type Layered struct {
	Store
	// Global is the root database. Only its global rows are read through this
	// store; its space and agent rows belong to the root space.
	Global Store
	// GlobalWrite lets the reader write global memory. Only groot has it.
	GlobalWrite bool
}

// localReader is the reader of the space database. Global rows never live
// there, so the space database is read without global access.
func localReader(r Reader) Reader {
	r.Global = false
	return r
}

// globalReader is the reader of the root database: global rows only.
func globalReader(r Reader) Reader {
	return Reader{Agent: r.Agent, Global: true, globalOnly: true}
}

// unbounded is a budget that keeps every entry, for merging before the real
// budget is applied.
func unbounded() Budget {
	return Budget{Tokens: math.MaxInt32, MaxSummaries: math.MaxInt32, EntryRunes: math.MaxInt32}
}

// Remember stores global entries in the root database when the writer may
// write global memory, and every other entry in the space database.
func (l *Layered) Remember(ctx context.Context, e Entry) (Result, error) {
	if e.Scope == ScopeGlobal {
		if !l.GlobalWrite {
			return Result{}, ErrGlobalReadOnly
		}
		return l.Global.Remember(ctx, e)
	}
	return l.Store.Remember(ctx, e)
}

// Recall searches the space database, and the global rows when the reader may
// see them. Pinned matches come first; the rest keep the order of their store.
func (l *Layered) Recall(ctx context.Context, r Reader, query string, limit int) ([]Entry, error) {
	if limit <= 0 {
		limit = 10
	}
	merged, err := l.Store.Recall(ctx, localReader(r), query, limit)
	if err != nil {
		return nil, err
	}
	if r.Global {
		global, err := l.Global.Recall(ctx, globalReader(r), query, limit)
		if err != nil {
			return nil, err
		}
		merged = append(merged, global...)
	}
	sort.SliceStable(merged, func(i, j int) bool { return merged[i].Pinned && !merged[j].Pinned })
	if len(merged) > limit {
		merged = merged[:limit]
	}
	return merged, nil
}

// Preamble applies the budget once over the entries of both databases. The
// order of the space database is kept within each group, and the newest entries
// of both databases come first.
func (l *Layered) Preamble(ctx context.Context, r Reader, b Budget) ([]Entry, error) {
	all, err := l.Store.Preamble(ctx, localReader(r), unbounded())
	if err != nil {
		return nil, err
	}
	if r.Global {
		global, err := l.Global.Preamble(ctx, globalReader(r), unbounded())
		if err != nil {
			return nil, err
		}
		all = append(all, global...)
	}
	sort.SliceStable(all, func(i, j int) bool { return all[i].CreatedAt.After(all[j].CreatedAt) })
	return fitBudget(all, b.withDefaults()), nil
}

// Overview adds the counts of both databases.
func (l *Layered) Overview(ctx context.Context, r Reader) (Overview, error) {
	ov, err := l.Store.Overview(ctx, localReader(r))
	if err != nil || !r.Global {
		return ov, err
	}
	g, err := l.Global.Overview(ctx, globalReader(r))
	if err != nil {
		return Overview{}, err
	}
	return Overview{
		Visible:     ov.Visible + g.Visible,
		Summaries:   ov.Summaries + g.Summaries,
		Pinned:      ov.Pinned + g.Pinned,
		NeedsReview: ov.NeedsReview + g.NeedsReview,
	}, nil
}

// Get returns the space entry with the id, else the global one when the reader
// may see global entries.
func (l *Layered) Get(ctx context.Context, id int64, r Reader) (Entry, error) {
	e, err := l.Store.Get(ctx, id, localReader(r))
	if !errors.Is(err, ErrNotFound) {
		return e, err
	}
	if !r.Global {
		return Entry{}, ErrNotFound
	}
	return l.Global.Get(ctx, id, globalReader(r))
}

// Update changes a space entry, else a global one the writer may change.
func (l *Layered) Update(ctx context.Context, id int64, r Reader, p Patch) error {
	err := l.Store.Update(ctx, id, localReader(r), p)
	if !errors.Is(err, ErrNotFound) {
		return err
	}
	return l.mutateGlobal(ctx, id, r, func(s Store, gr Reader) error { return s.Update(ctx, id, gr, p) })
}

// SetPinned pins or unpins a space entry, else a global one the writer may change.
func (l *Layered) SetPinned(ctx context.Context, id int64, r Reader, pinned bool) error {
	err := l.Store.SetPinned(ctx, id, localReader(r), pinned)
	if !errors.Is(err, ErrNotFound) {
		return err
	}
	return l.mutateGlobal(ctx, id, r, func(s Store, gr Reader) error { return s.SetPinned(ctx, id, gr, pinned) })
}

// MarkReviewed moves the review date of a space entry, else of a global one the
// writer may change.
func (l *Layered) MarkReviewed(ctx context.Context, id int64, r Reader) error {
	err := l.Store.MarkReviewed(ctx, id, localReader(r))
	if !errors.Is(err, ErrNotFound) {
		return err
	}
	return l.mutateGlobal(ctx, id, r, func(s Store, gr Reader) error { return s.MarkReviewed(ctx, id, gr) })
}

// Forget soft-deletes a space entry, else a global one the writer may change.
func (l *Layered) Forget(ctx context.Context, id int64, r Reader) error {
	err := l.Store.Forget(ctx, id, localReader(r))
	if !errors.Is(err, ErrNotFound) {
		return err
	}
	return l.mutateGlobal(ctx, id, r, func(s Store, gr Reader) error { return s.Forget(ctx, id, gr) })
}

// mutateGlobal runs a change on a global entry. A reader that cannot see the
// entry gets ErrNotFound; one that sees it but may not write it gets
// ErrGlobalReadOnly.
func (l *Layered) mutateGlobal(ctx context.Context, id int64, r Reader, change func(Store, Reader) error) error {
	if !r.Global {
		return ErrNotFound
	}
	if _, err := l.Global.Get(ctx, id, globalReader(r)); err != nil {
		return err
	}
	if !l.GlobalWrite {
		return ErrGlobalReadOnly
	}
	return change(l.Global, globalReader(r))
}

// NeedsReview lists the entries to review from both databases, oldest review
// date first, up to limit.
func (l *Layered) NeedsReview(ctx context.Context, r Reader, limit int) ([]Entry, error) {
	all, err := l.Store.NeedsReview(ctx, localReader(r), limit)
	if err != nil {
		return nil, err
	}
	if r.Global {
		global, err := l.Global.NeedsReview(ctx, globalReader(r), limit)
		if err != nil {
			return nil, err
		}
		all = append(all, global...)
	}
	sort.SliceStable(all, func(i, j int) bool { return all[i].ReviewAfter.Before(all[j].ReviewAfter) })
	if limit > 0 && len(all) > limit {
		all = all[:limit]
	}
	return all, nil
}
