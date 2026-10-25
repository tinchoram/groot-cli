package memory

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"testing"
	"time"
	"unicode/utf8"
)

var clockStart = time.Date(2026, 10, 25, 9, 0, 0, 0, time.UTC)

// setClock makes the store read the given time for every write and review.
func setClock(s *SQLite, at time.Time) {
	s.now = func() time.Time { return at }
}

func mustRemember(t *testing.T, s *SQLite, e Entry) Result {
	t.Helper()
	res, err := s.Remember(context.Background(), e)
	if err != nil {
		t.Fatalf("Remember(%+v): %v", e, err)
	}
	return res
}

// allVisible lists every entry the reader sees, through Preamble with a budget
// large enough to keep all of them.
func allVisible(t *testing.T, s *SQLite, r Reader) []Entry {
	t.Helper()
	got, err := s.Preamble(context.Background(), r, Budget{Tokens: 100000, MaxSummaries: 100, EntryRunes: 10000})
	if err != nil {
		t.Fatalf("Preamble(%+v): %v", r, err)
	}
	return got
}

func textsOf(entries []Entry) []string {
	out := make([]string, 0, len(entries))
	for _, e := range entries {
		out = append(out, e.Text)
	}
	sort.Strings(out)
	return out
}

func wantTexts(t *testing.T, got []Entry, want ...string) {
	t.Helper()
	sort.Strings(want)
	if strings.Join(textsOf(got), "|") != strings.Join(want, "|") {
		t.Fatalf("texts = %q, want %q", textsOf(got), want)
	}
}

func TestVisibilityMatrix(t *testing.T) {
	s := openTemp(t)
	setClock(s, clockStart)
	mustRemember(t, s, Entry{Kind: KindFact, Scope: ScopeAgent, Agent: "a", Text: "private to a"})
	mustRemember(t, s, Entry{Kind: KindFact, Scope: ScopeAgent, Agent: "b", Text: "private to b"})
	mustRemember(t, s, Entry{Kind: KindFact, Scope: ScopeSpace, Text: "shared in space"})
	mustRemember(t, s, Entry{Kind: KindFact, Scope: ScopeGlobal, Text: "root global"})

	tests := []struct {
		name   string
		reader Reader
		want   []string
	}{
		{"agent sees its own and space, not others or global", Reader{Agent: "a"}, []string{"private to a", "shared in space"}},
		{"agent with global also sees global", Reader{Agent: "a", Global: true}, []string{"private to a", "shared in space", "root global"}},
		{"empty reader sees only space", Reader{}, []string{"shared in space"}},
		{"empty reader with global sees space and global", Reader{Global: true}, []string{"shared in space", "root global"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			wantTexts(t, allVisible(t, s, tt.reader), tt.want...)
		})
	}
}

func TestRememberUpsertsByTopicKey(t *testing.T) {
	ctx := context.Background()
	s := openTemp(t)
	setClock(s, clockStart)
	first := mustRemember(t, s, Entry{Kind: KindRunbook, Scope: ScopeSpace, Title: "Ingress restart", Text: "old steps", TopicKey: "ops/ingress"})
	if first.Outcome != OutcomeCreated {
		t.Fatalf("first outcome = %s, want created", first.Outcome)
	}
	if err := s.SetPinned(ctx, first.ID, Reader{}, true); err != nil {
		t.Fatal(err)
	}

	setClock(s, clockStart.Add(time.Hour))
	second := mustRemember(t, s, Entry{Kind: KindRunbook, Scope: ScopeSpace, Title: "Ingress restart v2", Text: "new steps", TopicKey: "ops/ingress"})
	if second.Outcome != OutcomeUpdated || second.ID != first.ID {
		t.Fatalf("second = %+v, want updated id %d", second, first.ID)
	}
	got, err := s.Get(ctx, first.ID, Reader{})
	if err != nil {
		t.Fatal(err)
	}
	if got.Text != "new steps" || got.Title != "Ingress restart v2" {
		t.Fatalf("content not replaced: %+v", got)
	}
	if got.RevisionCount != 2 || !got.UpdatedAt.Equal(clockStart.Add(time.Hour)) {
		t.Fatalf("revision/updated_at = %d/%v", got.RevisionCount, got.UpdatedAt)
	}
	if !got.Pinned {
		t.Fatal("an upsert must keep the pin")
	}
	if n := len(allVisible(t, s, Reader{})); n != 1 {
		t.Fatalf("upsert created a second row: %d visible", n)
	}
}

func TestRememberDedupesByNormalizedContent(t *testing.T) {
	ctx := context.Background()
	s := openTemp(t)
	setClock(s, clockStart)
	first := mustRemember(t, s, Entry{Kind: KindFact, Scope: ScopeSpace, Title: "Ingress", Text: "Restart the ingress controller"})
	if first.Outcome != OutcomeCreated {
		t.Fatalf("first outcome = %s", first.Outcome)
	}

	variants := []struct {
		name, title, text string
	}{
		{"same content", "Ingress", "Restart the ingress controller"},
		{"case and whitespace", "  INGRESS ", "  restart\tthe   INGRESS controller\n"},
	}
	for _, v := range variants {
		t.Run(v.name, func(t *testing.T) {
			res, err := s.Remember(ctx, Entry{Kind: KindFact, Scope: ScopeSpace, Title: v.title, Text: v.text})
			if err != nil {
				t.Fatal(err)
			}
			if res.Outcome != OutcomeDuplicate || res.ID != first.ID {
				t.Fatalf("result = %+v, want duplicate of %d", res, first.ID)
			}
		})
	}

	got, err := s.Get(ctx, first.ID, Reader{})
	if err != nil {
		t.Fatal(err)
	}
	if got.DuplicateCount != 2 {
		t.Fatalf("duplicate_count = %d, want 2", got.DuplicateCount)
	}
	if n := len(allVisible(t, s, Reader{})); n != 1 {
		t.Fatalf("duplicates created rows: %d visible", n)
	}

	other := mustRemember(t, s, Entry{Kind: KindFact, Scope: ScopeAgent, Agent: "sre", Title: "Ingress", Text: "Restart the ingress controller"})
	if other.Outcome != OutcomeCreated {
		t.Fatalf("same text in another scope must be a new entry, got %s", other.Outcome)
	}
}

func TestForgetHidesTheEntryEverywhere(t *testing.T) {
	ctx := context.Background()
	s := openTemp(t)
	setClock(s, clockStart)
	kept := mustRemember(t, s, Entry{Kind: KindRunbook, Scope: ScopeSpace, Text: "kept runbook about quota"})
	gone := mustRemember(t, s, Entry{Kind: KindRunbook, Scope: ScopeSpace, Text: "forgotten runbook about quota", ReviewAfter: clockStart.Add(-time.Hour)})

	if err := s.Forget(ctx, gone.ID, Reader{}); err != nil {
		t.Fatalf("Forget: %v", err)
	}
	if got, err := s.Recall(ctx, Reader{}, "quota", 10); err != nil || len(got) != 1 || got[0].ID != kept.ID {
		t.Fatalf("Recall after forget = %+v, %v", got, err)
	}
	wantTexts(t, allVisible(t, s, Reader{}), "kept runbook about quota")
	if ov, err := s.Overview(ctx, Reader{}); err != nil || ov.Visible != 1 || ov.NeedsReview != 0 {
		t.Fatalf("Overview after forget = %+v, %v", ov, err)
	}
	if _, err := s.Get(ctx, gone.ID, Reader{}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Get of a forgotten entry = %v, want ErrNotFound", err)
	}
	if err := s.Forget(ctx, gone.ID, Reader{}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("second Forget = %v, want ErrNotFound", err)
	}
}

func TestForgetNeedsVisibility(t *testing.T) {
	ctx := context.Background()
	s := openTemp(t)
	setClock(s, clockStart)
	private := mustRemember(t, s, Entry{Kind: KindFact, Scope: ScopeAgent, Agent: "b", Text: "b's private fact"})
	if err := s.Forget(ctx, private.ID, Reader{Agent: "a"}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("agent a forgetting b's entry = %v, want ErrNotFound", err)
	}
	if _, err := s.Get(ctx, private.ID, Reader{Agent: "b"}); err != nil {
		t.Fatalf("the entry must survive a refused forget: %v", err)
	}
}

func TestUpdateBumpsRevisionOnlyWhenContentChanges(t *testing.T) {
	ctx := context.Background()
	s := openTemp(t)
	setClock(s, clockStart)
	e := mustRemember(t, s, Entry{Kind: KindFact, Scope: ScopeSpace, Text: "the backup runs at night"})

	setClock(s, clockStart.Add(time.Hour))
	pin := true
	if err := s.Update(ctx, e.ID, Reader{}, Patch{Pinned: &pin}); err != nil {
		t.Fatal(err)
	}
	got, _ := s.Get(ctx, e.ID, Reader{})
	if got.RevisionCount != 1 || !got.Pinned {
		t.Fatalf("pin-only update: revision=%d pinned=%v", got.RevisionCount, got.Pinned)
	}

	setClock(s, clockStart.Add(2*time.Hour))
	text := "the backup runs at 02:00"
	if err := s.Update(ctx, e.ID, Reader{}, Patch{Text: &text}); err != nil {
		t.Fatal(err)
	}
	got, _ = s.Get(ctx, e.ID, Reader{})
	if got.RevisionCount != 2 || got.Text != text || !got.UpdatedAt.Equal(clockStart.Add(2*time.Hour)) {
		t.Fatalf("text update: %+v", got)
	}
	if r, err := s.Recall(ctx, Reader{}, "02:00", 10); err != nil || len(r) != 1 {
		t.Fatalf("the index must follow the new text: %d (err %v)", len(r), err)
	}
	if r, err := s.Recall(ctx, Reader{}, "night", 10); err != nil || len(r) != 0 {
		t.Fatalf("the old text must leave the index: %d (err %v)", len(r), err)
	}
}

func TestPinnedEntriesLeadThePreambleAndRecall(t *testing.T) {
	ctx := context.Background()
	s := openTemp(t)
	setClock(s, clockStart)
	old := mustRemember(t, s, Entry{Kind: KindFact, Scope: ScopeSpace, Text: "alpha fact pinned long ago"})
	setClock(s, clockStart.Add(time.Hour))
	mustRemember(t, s, Entry{Kind: KindDecision, Scope: ScopeSpace, Text: "alpha decision, newer"})
	if err := s.SetPinned(ctx, old.ID, Reader{}, true); err != nil {
		t.Fatal(err)
	}

	pre, err := s.Preamble(ctx, Reader{}, Budget{})
	if err != nil || len(pre) != 2 || pre[0].ID != old.ID {
		t.Fatalf("preamble = %+v, %v; want the pinned fact first", pre, err)
	}
	rec, err := s.Recall(ctx, Reader{}, "alpha", 10)
	if err != nil || len(rec) != 2 || rec[0].ID != old.ID {
		t.Fatalf("recall = %+v, %v; want the pinned fact first", rec, err)
	}
	if ov, _ := s.Overview(ctx, Reader{}); ov.Pinned != 1 {
		t.Fatalf("Overview.Pinned = %d, want 1", ov.Pinned)
	}
}

func TestReviewAfterDefaultsAndNeedsReview(t *testing.T) {
	ctx := context.Background()
	s := openTemp(t)
	setClock(s, clockStart)
	fact := mustRemember(t, s, Entry{Kind: KindFact, Scope: ScopeSpace, Text: "fact that goes stale"})
	runbook := mustRemember(t, s, Entry{Kind: KindRunbook, Scope: ScopeSpace, Text: "runbook that lasts a year"})
	summary := mustRemember(t, s, Entry{Kind: KindSessionSummary, Scope: ScopeAgent, Agent: "sre", Text: "summary never reviewed"})

	for _, tc := range []struct {
		name string
		id   int64
		want time.Time
	}{
		{"fact", fact.ID, clockStart.AddDate(0, 6, 0)},
		{"runbook", runbook.ID, clockStart.AddDate(0, 12, 0)},
		{"summary", summary.ID, time.Time{}},
	} {
		got, err := s.Get(ctx, tc.id, Reader{Agent: "sre"})
		if err != nil {
			t.Fatal(err)
		}
		if !got.ReviewAfter.Equal(tc.want) {
			t.Errorf("%s review_after = %v, want %v", tc.name, got.ReviewAfter, tc.want)
		}
	}

	setClock(s, clockStart.AddDate(0, 7, 0))
	stale, err := s.NeedsReview(ctx, Reader{Agent: "sre"}, 10)
	if err != nil || len(stale) != 1 || stale[0].ID != fact.ID || stale[0].State != StateNeedsReview {
		t.Fatalf("NeedsReview = %+v, %v; want only the fact", stale, err)
	}
	if ov, _ := s.Overview(ctx, Reader{Agent: "sre"}); ov.NeedsReview != 1 {
		t.Fatalf("Overview.NeedsReview = %d, want 1", ov.NeedsReview)
	}

	if err := s.MarkReviewed(ctx, fact.ID, Reader{Agent: "sre"}); err != nil {
		t.Fatal(err)
	}
	got, _ := s.Get(ctx, fact.ID, Reader{Agent: "sre"})
	if got.State != StateActive || !got.ReviewAfter.Equal(clockStart.AddDate(0, 13, 0)) {
		t.Fatalf("after MarkReviewed: state=%s review_after=%v", got.State, got.ReviewAfter)
	}
	if err := s.MarkReviewed(ctx, summary.ID, Reader{Agent: "sre"}); err != nil {
		t.Fatal(err)
	}
	if got, _ := s.Get(ctx, summary.ID, Reader{Agent: "sre"}); !got.ReviewAfter.IsZero() {
		t.Fatalf("a kind without an interval must clear review_after, got %v", got.ReviewAfter)
	}
}

func TestRecallPrefixOrFallbackAndTitles(t *testing.T) {
	ctx := context.Background()
	s := openTemp(t)
	setClock(s, clockStart)
	mustRemember(t, s, Entry{Kind: KindRunbook, Scope: ScopeSpace, Text: "restart the ingress controller"})
	mustRemember(t, s, Entry{Kind: KindFact, Scope: ScopeSpace, Title: "Payments rota", Text: "who is on call this week"})

	tests := []struct {
		name  string
		query string
		want  int
	}{
		{"last term is a prefix", "ingr", 1},
		{"no entry has every term: falls back to any term", "kubectl ingress", 1},
		{"title is indexed", "rota", 1},
		{"nothing matches", "zebra", 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := s.Recall(ctx, Reader{}, tt.query, 10)
			if err != nil || len(got) != tt.want {
				t.Fatalf("Recall(%q) = %d entries, %v; want %d", tt.query, len(got), err, tt.want)
			}
		})
	}
}

func TestPreambleOrderingBudgetTruncationAndSummaryCap(t *testing.T) {
	ctx := context.Background()
	s := openTemp(t)
	setClock(s, clockStart)
	for i := 1; i <= 4; i++ {
		setClock(s, clockStart.Add(time.Duration(i)*time.Minute))
		mustRemember(t, s, Entry{Kind: KindSessionSummary, Scope: ScopeSpace, Text: fmt.Sprintf("summary %d", i)})
	}
	setClock(s, clockStart.Add(10*time.Minute))
	mustRemember(t, s, Entry{Kind: KindFact, Scope: ScopeSpace, Text: "fact older"})
	mustRemember(t, s, Entry{Kind: KindFact, Scope: ScopeSpace, Text: "fact newer"})
	mustRemember(t, s, Entry{Kind: KindDecision, Scope: ScopeSpace, Text: "decision"})
	mustRemember(t, s, Entry{Kind: KindRunbook, Scope: ScopeSpace, Text: "runbook"})
	pinned := mustRemember(t, s, Entry{Kind: KindFact, Scope: ScopeSpace, Text: "pinned fact"})
	if err := s.SetPinned(ctx, pinned.ID, Reader{}, true); err != nil {
		t.Fatal(err)
	}

	got, err := s.Preamble(ctx, Reader{}, Budget{Tokens: 100000, MaxSummaries: 2, EntryRunes: 400})
	if err != nil {
		t.Fatal(err)
	}
	var order []string
	for _, e := range got {
		order = append(order, e.Text)
	}
	want := "pinned fact|runbook|decision|fact newer|fact older|summary 4|summary 3"
	if strings.Join(order, "|") != want {
		t.Fatalf("order = %q, want %q", strings.Join(order, "|"), want)
	}

	long := strings.Repeat("é", 50)
	mustRemember(t, s, Entry{Kind: KindDecision, Scope: ScopeSpace, Text: long})
	cut, err := s.Preamble(ctx, Reader{}, Budget{Tokens: 100000, MaxSummaries: 2, EntryRunes: 20})
	if err != nil {
		t.Fatal(err)
	}
	var truncated Entry
	for _, e := range cut {
		if e.Truncated {
			truncated = e
		}
	}
	if !truncated.Truncated || utf8.RuneCountInString(truncated.Text) != 21 || !strings.HasSuffix(truncated.Text, "…") {
		t.Fatalf("long entry not cut to 20 runes plus an ellipsis: %+v", truncated)
	}

	// Every fact line below is "- [fact] " (9 runes) plus 40 runes, so 49 runes
	// and a cost of 12 tokens each. A budget of 25 keeps two of them.
	setClock(s, clockStart.Add(time.Hour))
	var lines []string
	for i := 0; i < 3; i++ {
		lines = append(lines, fmt.Sprintf("%02d", i)+strings.Repeat("x", 38))
	}
	fresh := openTemp(t)
	for _, l := range lines {
		mustRemember(t, fresh, Entry{Kind: KindFact, Scope: ScopeSpace, Text: l})
	}
	budgeted, err := fresh.Preamble(ctx, Reader{}, Budget{Tokens: 25, MaxSummaries: 2, EntryRunes: 400})
	if err != nil || len(budgeted) != 2 {
		t.Fatalf("budget 25 kept %d entries, %v; want 2", len(budgeted), err)
	}
	one, err := fresh.Preamble(ctx, Reader{}, Budget{Tokens: 1, MaxSummaries: 2, EntryRunes: 400})
	if err != nil || len(one) != 1 {
		t.Fatalf("a tiny budget must still keep one entry, got %d (err %v)", len(one), err)
	}
}

func TestFormatPreambleLinesEntries(t *testing.T) {
	got := FormatPreamble([]Entry{
		{Kind: KindDecision, Text: "global decision"},
		{Kind: KindFact, Title: "Rota", Text: "who is on call"},
	})
	want := "- [decision] global decision\n- [fact] Rota: who is on call\n"
	if got != want {
		t.Fatalf("FormatPreamble = %q, want %q", got, want)
	}
}

func TestOverviewCountsVisibleSummariesPinnedAndReview(t *testing.T) {
	ctx := context.Background()
	s := openTemp(t)
	setClock(s, clockStart)
	mustRemember(t, s, Entry{Kind: KindFact, Scope: ScopeAgent, Agent: "a", Text: "fact for a"})
	mustRemember(t, s, Entry{Kind: KindSessionSummary, Scope: ScopeSpace, Text: "space summary"})
	mustRemember(t, s, Entry{Kind: KindFact, Scope: ScopeAgent, Agent: "b", Text: "private to b"})

	ov, err := s.Overview(ctx, Reader{Agent: "a"})
	if err != nil {
		t.Fatal(err)
	}
	if ov.Visible != 2 || ov.Summaries != 1 || ov.Pinned != 0 || ov.NeedsReview != 0 {
		t.Fatalf("Overview = %+v", ov)
	}
}

func TestPreambleEntriesCarryTheirState(t *testing.T) {
	s := openTemp(t)
	setClock(s, clockStart)
	mustRemember(t, s, Entry{Kind: KindFact, Scope: ScopeSpace, Text: "state check"})
	setClock(s, clockStart.AddDate(1, 0, 0))
	got := allVisible(t, s, Reader{})
	if len(got) != 1 || got[0].State != StateNeedsReview {
		t.Fatalf("preamble entries = %+v, want needs_review", got)
	}
}
