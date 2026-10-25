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

// Ref names an entry by its id and by the database that holds it. Ids are per
// database, so the same number can name a space entry and a global one.
type Ref struct {
	ID int64
	// Global routes the id to the global database only. Without it, the space
	// database is tried first, then the global one.
	Global bool
}

// RefStore is a store whose ids are per database. The Ref methods route an id
// by its hint; the plain id methods are the hint-less form.
type RefStore interface {
	GetRef(ctx context.Context, ref Ref, r Reader) (Entry, error)
	UpdateRef(ctx context.Context, ref Ref, r Reader, p Patch) error
	SetPinnedRef(ctx context.Context, ref Ref, r Reader, pinned bool) error
	MarkReviewedRef(ctx context.Context, ref Ref, r Reader) error
	ForgetRef(ctx context.Context, ref Ref, r Reader) error
}

var _ RefStore = (*Layered)(nil)

// GetByRef reads an entry by ref. A store without per-database ids has one
// id space, so it ignores the hint.
func GetByRef(ctx context.Context, s Store, ref Ref, r Reader) (Entry, error) {
	if rs, ok := s.(RefStore); ok {
		return rs.GetRef(ctx, ref, r)
	}
	return s.Get(ctx, ref.ID, r)
}

// UpdateByRef changes an entry by ref; see GetByRef.
func UpdateByRef(ctx context.Context, s Store, ref Ref, r Reader, p Patch) error {
	if rs, ok := s.(RefStore); ok {
		return rs.UpdateRef(ctx, ref, r, p)
	}
	return s.Update(ctx, ref.ID, r, p)
}

// SetPinnedByRef pins or unpins an entry by ref; see GetByRef.
func SetPinnedByRef(ctx context.Context, s Store, ref Ref, r Reader, pinned bool) error {
	if rs, ok := s.(RefStore); ok {
		return rs.SetPinnedRef(ctx, ref, r, pinned)
	}
	return s.SetPinned(ctx, ref.ID, r, pinned)
}

// MarkReviewedByRef moves the review date of an entry by ref; see GetByRef.
func MarkReviewedByRef(ctx context.Context, s Store, ref Ref, r Reader) error {
	if rs, ok := s.(RefStore); ok {
		return rs.MarkReviewedRef(ctx, ref, r)
	}
	return s.MarkReviewed(ctx, ref.ID, r)
}

// ForgetByRef soft-deletes an entry by ref; see GetByRef.
func ForgetByRef(ctx context.Context, s Store, ref Ref, r Reader) error {
	if rs, ok := s.(RefStore); ok {
		return rs.ForgetRef(ctx, ref, r)
	}
	return s.Forget(ctx, ref.ID, r)
}

// Get returns the space entry with the id, else the global one when the reader
// may see global entries.
func (l *Layered) Get(ctx context.Context, id int64, r Reader) (Entry, error) {
	return l.GetRef(ctx, Ref{ID: id}, r)
}

// GetRef is Get with a hint: a global ref reads only the global database.
func (l *Layered) GetRef(ctx context.Context, ref Ref, r Reader) (Entry, error) {
	if !ref.Global {
		e, err := l.Store.Get(ctx, ref.ID, localReader(r))
		if !errors.Is(err, ErrNotFound) {
			return e, err
		}
	}
	if !r.Global {
		return Entry{}, ErrNotFound
	}
	return l.Global.Get(ctx, ref.ID, globalReader(r))
}

// Update changes a space entry, else a global one the writer may change.
func (l *Layered) Update(ctx context.Context, id int64, r Reader, p Patch) error {
	return l.UpdateRef(ctx, Ref{ID: id}, r, p)
}

// UpdateRef is Update with a hint.
func (l *Layered) UpdateRef(ctx context.Context, ref Ref, r Reader, p Patch) error {
	return l.mutate(ctx, ref, r,
		func() error { return l.Store.Update(ctx, ref.ID, localReader(r), p) },
		func(s Store, gr Reader) error { return s.Update(ctx, ref.ID, gr, p) })
}

// SetPinned pins or unpins a space entry, else a global one the writer may change.
func (l *Layered) SetPinned(ctx context.Context, id int64, r Reader, pinned bool) error {
	return l.SetPinnedRef(ctx, Ref{ID: id}, r, pinned)
}

// SetPinnedRef is SetPinned with a hint.
func (l *Layered) SetPinnedRef(ctx context.Context, ref Ref, r Reader, pinned bool) error {
	return l.mutate(ctx, ref, r,
		func() error { return l.Store.SetPinned(ctx, ref.ID, localReader(r), pinned) },
		func(s Store, gr Reader) error { return s.SetPinned(ctx, ref.ID, gr, pinned) })
}

// MarkReviewed moves the review date of a space entry, else of a global one the
// writer may change.
func (l *Layered) MarkReviewed(ctx context.Context, id int64, r Reader) error {
	return l.MarkReviewedRef(ctx, Ref{ID: id}, r)
}

// MarkReviewedRef is MarkReviewed with a hint.
func (l *Layered) MarkReviewedRef(ctx context.Context, ref Ref, r Reader) error {
	return l.mutate(ctx, ref, r,
		func() error { return l.Store.MarkReviewed(ctx, ref.ID, localReader(r)) },
		func(s Store, gr Reader) error { return s.MarkReviewed(ctx, ref.ID, gr) })
}

// Forget soft-deletes a space entry, else a global one the writer may change.
func (l *Layered) Forget(ctx context.Context, id int64, r Reader) error {
	return l.ForgetRef(ctx, Ref{ID: id}, r)
}

// ForgetRef is Forget with a hint.
func (l *Layered) ForgetRef(ctx context.Context, ref Ref, r Reader) error {
	return l.mutate(ctx, ref, r,
		func() error { return l.Store.Forget(ctx, ref.ID, localReader(r)) },
		func(s Store, gr Reader) error { return s.Forget(ctx, ref.ID, gr) })
}

// mutate runs local on the space database unless the ref is global, and falls
// back to the global database when the space database has no such entry.
func (l *Layered) mutate(ctx context.Context, ref Ref, r Reader, local func() error, global func(Store, Reader) error) error {
	if !ref.Global {
		err := local()
		if !errors.Is(err, ErrNotFound) {
			return err
		}
	}
	return l.mutateGlobal(ctx, ref.ID, r, global)
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
