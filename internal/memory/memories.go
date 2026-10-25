package memory

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"
)

// timeLayout is fixed width, so text timestamps compare correctly as strings.
const timeLayout = "2006-01-02T15:04:05.000000000Z"

func fmtTime(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.UTC().Format(timeLayout)
}

func parseTime(s string) time.Time {
	if s == "" {
		return time.Time{}
	}
	t, err := time.Parse(timeLayout, s)
	if err != nil {
		return time.Time{}
	}
	return t
}

func boolInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

// reviewInterval is how long an entry of kind k stays fresh, counted from
// from. Session summaries have no interval: they are never due for review.
func reviewInterval(k Kind, from time.Time) time.Time {
	switch k {
	case KindFact, KindDecision:
		return from.AddDate(0, 6, 0)
	case KindRunbook:
		return from.AddDate(0, 12, 0)
	}
	return time.Time{}
}

// normalizeText lowercases text and collapses its whitespace, so that variants
// of the same sentence hash alike.
func normalizeText(s string) string {
	return strings.Join(strings.Fields(strings.ToLower(s)), " ")
}

// contentHash identifies an entry without a topic key: same kind, scope, agent,
// title and text (after normalization) mean the same memory.
func contentHash(kind Kind, scope Scope, agent, title, text string) string {
	key := strings.Join([]string{string(kind), string(scope), agent, normalizeText(title), normalizeText(text)}, "|")
	sum := sha256.Sum256([]byte(key))
	return hex.EncodeToString(sum[:])
}

// memoryAddedColumns are the hygiene columns of memories, with the statement
// that adds each one to a database that lacks it.
var memoryAddedColumns = []struct{ name, ddl string }{
	{"title", `ALTER TABLE memories ADD COLUMN title TEXT NOT NULL DEFAULT ''`},
	{"topic_key", `ALTER TABLE memories ADD COLUMN topic_key TEXT NOT NULL DEFAULT ''`},
	{"normalized_hash", `ALTER TABLE memories ADD COLUMN normalized_hash TEXT NOT NULL DEFAULT ''`},
	{"pinned", `ALTER TABLE memories ADD COLUMN pinned INTEGER NOT NULL DEFAULT 0`},
	{"review_after", `ALTER TABLE memories ADD COLUMN review_after TEXT NOT NULL DEFAULT ''`},
	{"revision_count", `ALTER TABLE memories ADD COLUMN revision_count INTEGER NOT NULL DEFAULT 1`},
	{"duplicate_count", `ALTER TABLE memories ADD COLUMN duplicate_count INTEGER NOT NULL DEFAULT 0`},
	{"last_seen_at", `ALTER TABLE memories ADD COLUMN last_seen_at TEXT NOT NULL DEFAULT ''`},
	{"updated_at", `ALTER TABLE memories ADD COLUMN updated_at TEXT NOT NULL DEFAULT ''`},
	{"deleted_at", `ALTER TABLE memories ADD COLUMN deleted_at TEXT NOT NULL DEFAULT ''`},
	{"tainted", `ALTER TABLE memories ADD COLUMN tainted INTEGER NOT NULL DEFAULT 0`},
}

// memoryFTS rebuilds the full-text index over title and text. Soft-deleted rows
// are kept out of it by the triggers.
var memoryFTS = []string{
	`DROP TRIGGER IF EXISTS memories_ai`,
	`DROP TRIGGER IF EXISTS memories_ad`,
	`DROP TRIGGER IF EXISTS memories_au`,
	`DROP TABLE IF EXISTS memories_fts`,
	`CREATE VIRTUAL TABLE memories_fts USING fts5(
		title, text, content='memories', content_rowid='id'
	)`,
	`CREATE TRIGGER memories_ai AFTER INSERT ON memories BEGIN
		INSERT INTO memories_fts(rowid, title, text) SELECT new.id, new.title, new.text WHERE new.deleted_at = '';
	END`,
	`CREATE TRIGGER memories_ad AFTER DELETE ON memories BEGIN
		INSERT INTO memories_fts(memories_fts, rowid, title, text) SELECT 'delete', old.id, old.title, old.text WHERE old.deleted_at = '';
	END`,
	`CREATE TRIGGER memories_au AFTER UPDATE ON memories BEGIN
		INSERT INTO memories_fts(memories_fts, rowid, title, text) SELECT 'delete', old.id, old.title, old.text WHERE old.deleted_at = '';
		INSERT INTO memories_fts(rowid, title, text) SELECT new.id, new.title, new.text WHERE new.deleted_at = '';
	END`,
}

// scopeMigrationKey marks that the legacy scopes were rewritten. The scopes
// global and project no longer exist, so the rewrite must run only once.
const scopeMigrationKey = "memory_scopes_v2"

// migrateMemories upgrades the memories table. Every step is idempotent.
func migrateMemories(db *sql.DB) error {
	for _, c := range memoryAddedColumns {
		has, err := hasColumn(db, "memories", c.name)
		if err != nil {
			return fmt.Errorf("migrate memories: %w", err)
		}
		if !has {
			if _, err := db.Exec(c.ddl); err != nil {
				return fmt.Errorf("migrate memories: add %s: %w", c.name, err)
			}
		}
	}
	indexed, err := hasColumn(db, "memories_fts", "title")
	if err != nil {
		return fmt.Errorf("migrate memories index: %w", err)
	}
	if !indexed {
		if err := rebuildMemoryIndex(db); err != nil {
			return err
		}
	}
	if err := migrateScopes(db); err != nil {
		return err
	}
	for _, stmt := range []string{
		`CREATE UNIQUE INDEX IF NOT EXISTS memories_topic ON memories(scope, agent, topic_key) WHERE topic_key <> '' AND deleted_at = ''`,
		`CREATE INDEX IF NOT EXISTS memories_hash ON memories(normalized_hash)`,
	} {
		if _, err := db.Exec(stmt); err != nil {
			return fmt.Errorf("migrate memories: index: %w", err)
		}
	}
	return nil
}

func rebuildMemoryIndex(db *sql.DB) error {
	tx, err := db.Begin()
	if err != nil {
		return fmt.Errorf("migrate memories index: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	for _, stmt := range memoryFTS {
		if _, err := tx.Exec(stmt); err != nil {
			return fmt.Errorf("migrate memories index: %w", err)
		}
	}
	if _, err := tx.Exec(`INSERT INTO memories_fts(rowid, title, text) SELECT id, title, text FROM memories WHERE deleted_at = ''`); err != nil {
		return fmt.Errorf("migrate memories index: fill: %w", err)
	}
	return tx.Commit()
}

// migrateScopes maps the legacy scopes onto agent and space, then fills in the
// content hash and review date of the rows that predate them. Project rows
// written by an agent stay private to it; the others, and global rows, become
// space rows, which is what the old database-wide scope meant.
func migrateScopes(db *sql.DB) error {
	var done int
	if err := db.QueryRow(`SELECT COUNT(*) FROM schema_meta WHERE key = ?`, scopeMigrationKey).Scan(&done); err != nil {
		return fmt.Errorf("migrate scopes: %w", err)
	}
	if done > 0 {
		return nil
	}
	tx, err := db.Begin()
	if err != nil {
		return fmt.Errorf("migrate scopes: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	for _, stmt := range []string{
		`UPDATE memories SET scope = 'agent' WHERE scope = 'project' AND agent <> ''`,
		`UPDATE memories SET scope = 'space' WHERE scope IN ('project', 'global') OR (scope = 'agent' AND agent = '')`,
	} {
		if _, err := tx.Exec(stmt); err != nil {
			return fmt.Errorf("migrate scopes: %w", err)
		}
	}
	type legacy struct {
		id                 int64
		kind, scope, agent string
		title, text        string
		created            time.Time
	}
	rows, err := tx.Query(`SELECT id, kind, scope, agent, title, text, created_at FROM memories WHERE normalized_hash = ''`)
	if err != nil {
		return fmt.Errorf("migrate scopes: %w", err)
	}
	var pending []legacy
	for rows.Next() {
		var l legacy
		if err := rows.Scan(&l.id, &l.kind, &l.scope, &l.agent, &l.title, &l.text, &l.created); err != nil {
			_ = rows.Close()
			return fmt.Errorf("migrate scopes: scan: %w", err)
		}
		pending = append(pending, l)
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return fmt.Errorf("migrate scopes: %w", err)
	}
	_ = rows.Close()
	for _, l := range pending {
		// l.scope was read after the rewrite above, so it is the new scope.
		hash := contentHash(Kind(l.kind), Scope(l.scope), l.agent, l.title, l.text)
		next := fmtTime(reviewInterval(Kind(l.kind), l.created))
		if _, err := tx.Exec(`UPDATE memories SET normalized_hash = ?, review_after = ? WHERE id = ?`, hash, next, l.id); err != nil {
			return fmt.Errorf("migrate scopes: backfill: %w", err)
		}
	}
	if _, err := tx.Exec(`INSERT INTO schema_meta (key, value) VALUES (?, 'done')`, scopeMigrationKey); err != nil {
		return fmt.Errorf("migrate scopes: mark: %w", err)
	}
	return tx.Commit()
}

// entryColumns are the columns scanEntry reads, for a query that names memories as m.
const entryColumns = `m.id, m.kind, m.scope, m.agent, m.title, m.text, m.topic_key, m.source, m.pinned, m.tainted,
	m.review_after, m.revision_count, m.duplicate_count, m.created_at, m.updated_at`

type scanner interface {
	Scan(dest ...any) error
}

func scanEntry(sc scanner, now time.Time) (Entry, error) {
	var (
		e                      Entry
		kind, scope            string
		reviewAfter, updatedAt string
		pinned, tainted        int
	)
	if err := sc.Scan(&e.ID, &kind, &scope, &e.Agent, &e.Title, &e.Text, &e.TopicKey, &e.Source, &pinned, &tainted,
		&reviewAfter, &e.RevisionCount, &e.DuplicateCount, &e.CreatedAt, &updatedAt); err != nil {
		return Entry{}, err
	}
	e.Kind = Kind(kind)
	e.Scope = Scope(scope)
	e.Pinned = pinned != 0
	e.Tainted = tainted != 0
	e.ReviewAfter = parseTime(reviewAfter)
	e.UpdatedAt = parseTime(updatedAt)
	e.State = StateActive
	if !e.ReviewAfter.IsZero() && e.ReviewAfter.Before(now) {
		e.State = StateNeedsReview
	}
	return e, nil
}

func scanEntries(rows *sql.Rows, now time.Time) ([]Entry, error) {
	defer func() { _ = rows.Close() }()
	var out []Entry
	for rows.Next() {
		e, err := scanEntry(rows, now)
		if err != nil {
			return nil, fmt.Errorf("scan memory: %w", err)
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

// inTx runs fn in one transaction. Inside fn, use the tx and never the store's
// database handle: the handle has a single connection, so it would wait forever.
func (s *SQLite) inTx(ctx context.Context, fn func(*sql.Tx) error) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	if err := fn(tx); err != nil {
		return err
	}
	return tx.Commit()
}

// Remember stores an entry, or counts or revises the one it matches.
func (s *SQLite) Remember(ctx context.Context, e Entry) (Result, error) {
	if !e.Kind.Valid() {
		return Result{}, fmt.Errorf("invalid memory kind %q", e.Kind)
	}
	if strings.TrimSpace(e.Text) == "" {
		return Result{}, errors.New("memory text is empty")
	}
	if e.Scope == "" {
		e.Scope = ScopeSpace
	}
	if !e.Scope.Valid() {
		return Result{}, fmt.Errorf("invalid memory scope %q", e.Scope)
	}
	if e.Scope == ScopeAgent && strings.TrimSpace(e.Agent) == "" {
		return Result{}, errors.New("agent scope needs an agent")
	}
	now := s.now()
	var res Result
	err := s.inTx(ctx, func(tx *sql.Tx) error {
		var err error
		res, err = rememberTx(ctx, tx, e, now)
		return err
	})
	return res, err
}

func rememberTx(ctx context.Context, q queryer, e Entry, now time.Time) (Result, error) {
	hash := contentHash(e.Kind, e.Scope, e.Agent, e.Title, e.Text)
	review := e.ReviewAfter
	if review.IsZero() {
		review = reviewInterval(e.Kind, now)
	}

	var id int64
	var err error
	if e.TopicKey != "" {
		err = q.QueryRowContext(ctx,
			`SELECT id FROM memories WHERE scope = ? AND agent = ? AND topic_key = ? AND deleted_at = ''`,
			string(e.Scope), e.Agent, e.TopicKey).Scan(&id)
		if err == nil {
			if _, err := q.ExecContext(ctx,
				`UPDATE memories SET title = ?, text = ?, source = ?, tainted = ?, normalized_hash = ?, review_after = ?,
					revision_count = revision_count + 1, updated_at = ? WHERE id = ?`,
				e.Title, e.Text, e.Source, boolInt(e.Tainted), hash, fmtTime(review), fmtTime(now), id); err != nil {
				return Result{}, fmt.Errorf("revise memory: %w", err)
			}
			return Result{ID: id, Outcome: OutcomeUpdated}, nil
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return Result{}, fmt.Errorf("find topic: %w", err)
		}
	} else {
		err = q.QueryRowContext(ctx,
			`SELECT id FROM memories WHERE normalized_hash = ? AND deleted_at = '' ORDER BY id LIMIT 1`, hash).Scan(&id)
		if err == nil {
			if _, err := q.ExecContext(ctx,
				`UPDATE memories SET duplicate_count = duplicate_count + 1, last_seen_at = ? WHERE id = ?`,
				fmtTime(now), id); err != nil {
				return Result{}, fmt.Errorf("count duplicate: %w", err)
			}
			return Result{ID: id, Outcome: OutcomeDuplicate}, nil
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return Result{}, fmt.Errorf("find duplicate: %w", err)
		}
	}

	res, err := q.ExecContext(ctx, `INSERT INTO memories
		(kind, scope, agent, title, text, topic_key, source, normalized_hash, pinned, tainted, review_after,
		 revision_count, duplicate_count, last_seen_at, updated_at, deleted_at, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, 1, 0, ?, ?, '', ?)`,
		string(e.Kind), string(e.Scope), e.Agent, e.Title, e.Text, e.TopicKey, e.Source, hash,
		boolInt(e.Pinned), boolInt(e.Tainted), fmtTime(review), fmtTime(now), fmtTime(now), now)
	if err != nil {
		return Result{}, fmt.Errorf("insert memory: %w", err)
	}
	id, err = res.LastInsertId()
	if err != nil {
		return Result{}, fmt.Errorf("memory id: %w", err)
	}
	return Result{ID: id, Outcome: OutcomeCreated}, nil
}

// queryTerms splits free text into terms. Quotes are dropped, so punctuation
// typed by a user never reaches FTS as syntax.
func queryTerms(query string) []string {
	var terms []string
	for _, f := range strings.Fields(query) {
		f = strings.ReplaceAll(f, `"`, "")
		if f != "" {
			terms = append(terms, f)
		}
	}
	return terms
}

// ftsMatch quotes every term and joins them with sep. The last term is a prefix
// match, so a half-typed word still finds its entries.
func ftsMatch(terms []string, sep string) string {
	parts := make([]string, len(terms))
	for i, t := range terms {
		parts[i] = `"` + t + `"`
		if i == len(terms)-1 {
			parts[i] += "*"
		}
	}
	return strings.Join(parts, sep)
}

// Recall matches every term first. When no entry has them all, it retries with
// any term, so a query with one unknown word still finds the rest.
func (s *SQLite) Recall(ctx context.Context, r Reader, query string, limit int) ([]Entry, error) {
	terms := queryTerms(query)
	if len(terms) == 0 {
		return nil, nil
	}
	if limit <= 0 {
		limit = 10
	}
	out, err := s.recallMatch(ctx, r, ftsMatch(terms, " "), limit)
	if err != nil {
		return nil, err
	}
	if len(out) > 0 || len(terms) < 2 {
		return out, nil
	}
	return s.recallMatch(ctx, r, ftsMatch(terms, " OR "), limit)
}

func (s *SQLite) recallMatch(ctx context.Context, r Reader, match string, limit int) ([]Entry, error) {
	cond, args := VisibleSQL(r)
	args = append([]any{match}, append(args, limit)...)
	rows, err := s.db.QueryContext(ctx, `SELECT `+entryColumns+`
		FROM memories_fts
		JOIN memories m ON m.id = memories_fts.rowid
		WHERE memories_fts MATCH ? AND `+cond+`
		ORDER BY m.pinned DESC, memories_fts.rank
		LIMIT ?`, args...)
	if err != nil {
		return nil, fmt.Errorf("recall: %w", err)
	}
	return scanEntries(rows, s.now())
}

// Preamble returns the visible entries in prompt order, cut to the budget.
func (s *SQLite) Preamble(ctx context.Context, r Reader, b Budget) ([]Entry, error) {
	cond, args := VisibleSQL(r)
	rows, err := s.db.QueryContext(ctx, `SELECT `+entryColumns+` FROM memories m WHERE `+cond+`
		ORDER BY m.pinned DESC, m.id DESC`, args...)
	if err != nil {
		return nil, fmt.Errorf("preamble: %w", err)
	}
	all, err := scanEntries(rows, s.now())
	if err != nil {
		return nil, err
	}
	return fitBudget(all, b.withDefaults()), nil
}

// fitBudget orders the entries for the prompt and keeps those that fit. Pinned
// entries come first, then decisions and runbooks, then facts, each newest
// first; session summaries come last and only the newest MaxSummaries of them
// are considered. The walk stops at the first entry that would exceed the
// budget, except that one entry is always kept when any exist.
func fitBudget(all []Entry, b Budget) []Entry {
	var pinned, rules, facts, summaries []Entry
	for _, e := range all {
		switch {
		case e.Pinned:
			pinned = append(pinned, e)
		case e.Kind == KindDecision || e.Kind == KindRunbook:
			rules = append(rules, e)
		case e.Kind == KindSessionSummary:
			summaries = append(summaries, e)
		default:
			facts = append(facts, e)
		}
	}
	if len(summaries) > b.MaxSummaries {
		summaries = summaries[:b.MaxSummaries]
	}
	candidates := append(append(append(pinned, rules...), facts...), summaries...)

	var out []Entry
	used := 0
	for _, e := range candidates {
		text, cut := truncateRunes(e.Text, b.EntryRunes)
		e.Text = text
		e.Truncated = cut
		cost := utf8.RuneCountInString(preambleLine(e)) / 4
		if len(out) > 0 && used+cost > b.Tokens {
			break
		}
		used += cost
		out = append(out, e)
	}
	return out
}

func truncateRunes(s string, n int) (string, bool) {
	if utf8.RuneCountInString(s) <= n {
		return s, false
	}
	return string([]rune(s)[:n]) + "…", true
}

// Overview counts the entries a reader can see.
func (s *SQLite) Overview(ctx context.Context, r Reader) (Overview, error) {
	cond, args := VisibleSQL(r)
	params := append([]any{string(KindSessionSummary), fmtTime(s.now())}, args...)
	var ov Overview
	err := s.db.QueryRowContext(ctx, `
		SELECT COUNT(*),
		       COALESCE(SUM(CASE WHEN m.kind = ? THEN 1 ELSE 0 END), 0),
		       COALESCE(SUM(m.pinned), 0),
		       COALESCE(SUM(CASE WHEN m.review_after <> '' AND m.review_after < ? THEN 1 ELSE 0 END), 0)
		FROM memories m
		WHERE `+cond, params...).Scan(&ov.Visible, &ov.Summaries, &ov.Pinned, &ov.NeedsReview)
	if err != nil {
		return Overview{}, fmt.Errorf("overview: %w", err)
	}
	return ov, nil
}

// getEntry reads one visible, live entry. It works on a tx too.
func getEntry(ctx context.Context, q queryer, now time.Time, id int64, r Reader) (Entry, error) {
	cond, args := VisibleSQL(r)
	row := q.QueryRowContext(ctx, `SELECT `+entryColumns+` FROM memories m WHERE m.id = ? AND `+cond,
		append([]any{id}, args...)...)
	e, err := scanEntry(row, now)
	if errors.Is(err, sql.ErrNoRows) {
		return Entry{}, ErrNotFound
	}
	if err != nil {
		return Entry{}, fmt.Errorf("memory #%d: %w", id, err)
	}
	return e, nil
}

// Get returns one visible entry.
func (s *SQLite) Get(ctx context.Context, id int64, r Reader) (Entry, error) {
	return getEntry(ctx, s.db, s.now(), id, r)
}

// Update changes the fields the patch sets. Changing the title or the text
// starts a new revision.
func (s *SQLite) Update(ctx context.Context, id int64, r Reader, p Patch) error {
	now := s.now()
	return s.inTx(ctx, func(tx *sql.Tx) error {
		cur, err := getEntry(ctx, tx, now, id, r)
		if err != nil {
			return err
		}
		title, text, topic, pinned := cur.Title, cur.Text, cur.TopicKey, cur.Pinned
		if p.Title != nil {
			title = *p.Title
		}
		if p.Text != nil {
			text = *p.Text
		}
		if p.TopicKey != nil {
			topic = *p.TopicKey
		}
		if p.Pinned != nil {
			pinned = *p.Pinned
		}
		if strings.TrimSpace(text) == "" {
			return errors.New("memory text is empty")
		}
		revision, updated := cur.RevisionCount, cur.UpdatedAt
		if title != cur.Title || text != cur.Text {
			revision++
			updated = now
		}
		_, err = tx.ExecContext(ctx, `UPDATE memories SET title = ?, text = ?, topic_key = ?, pinned = ?,
			normalized_hash = ?, revision_count = ?, updated_at = ? WHERE id = ?`,
			title, text, topic, boolInt(pinned), contentHash(cur.Kind, cur.Scope, cur.Agent, title, text),
			revision, fmtTime(updated), id)
		if err != nil {
			return fmt.Errorf("update memory: %w", err)
		}
		return nil
	})
}

// SetPinned pins or unpins a visible entry.
func (s *SQLite) SetPinned(ctx context.Context, id int64, r Reader, pinned bool) error {
	return s.Update(ctx, id, r, Patch{Pinned: &pinned})
}

// MarkReviewed moves the review date of a visible entry one interval ahead, or
// clears it when its kind has no interval.
func (s *SQLite) MarkReviewed(ctx context.Context, id int64, r Reader) error {
	now := s.now()
	return s.inTx(ctx, func(tx *sql.Tx) error {
		cur, err := getEntry(ctx, tx, now, id, r)
		if err != nil {
			return err
		}
		_, err = tx.ExecContext(ctx, `UPDATE memories SET review_after = ? WHERE id = ?`,
			fmtTime(reviewInterval(cur.Kind, now)), id)
		if err != nil {
			return fmt.Errorf("mark reviewed: %w", err)
		}
		return nil
	})
}

// Forget soft-deletes a visible entry. The index triggers drop it from search.
func (s *SQLite) Forget(ctx context.Context, id int64, r Reader) error {
	now := s.now()
	return s.inTx(ctx, func(tx *sql.Tx) error {
		if _, err := getEntry(ctx, tx, now, id, r); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `UPDATE memories SET deleted_at = ? WHERE id = ?`, fmtTime(now), id); err != nil {
			return fmt.Errorf("forget memory: %w", err)
		}
		return nil
	})
}

// NeedsReview returns the visible entries whose review date has passed, oldest first.
func (s *SQLite) NeedsReview(ctx context.Context, r Reader, limit int) ([]Entry, error) {
	if limit <= 0 {
		limit = 10
	}
	cond, args := VisibleSQL(r)
	args = append(args, fmtTime(s.now()), limit)
	rows, err := s.db.QueryContext(ctx, `SELECT `+entryColumns+` FROM memories m
		WHERE `+cond+` AND m.review_after <> '' AND m.review_after < ?
		ORDER BY m.review_after ASC
		LIMIT ?`, args...)
	if err != nil {
		return nil, fmt.Errorf("needs review: %w", err)
	}
	return scanEntries(rows, s.now())
}
