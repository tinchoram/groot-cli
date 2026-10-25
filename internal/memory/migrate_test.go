package memory

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"
)

// legacyStatements is the schema of a space database written before the
// memory hygiene columns, scopes and title index existed.
var legacyStatements = []string{
	`CREATE TABLE sessions (
		id         TEXT PRIMARY KEY,
		parent_id  TEXT NULL,
		agent      TEXT NOT NULL DEFAULT '',
		model      TEXT NOT NULL DEFAULT '',
		started_at DATETIME NOT NULL,
		ended_at   DATETIME NULL,
		cost_usd   REAL NOT NULL DEFAULT 0
	)`,
	`CREATE TABLE messages (
		id           INTEGER PRIMARY KEY AUTOINCREMENT,
		session_id   TEXT NOT NULL,
		turn         INTEGER NOT NULL,
		role         TEXT NOT NULL,
		content_json TEXT NOT NULL,
		in_tokens    INTEGER NOT NULL DEFAULT 0,
		out_tokens   INTEGER NOT NULL DEFAULT 0
	)`,
	`CREATE TABLE memories (
		id         INTEGER PRIMARY KEY AUTOINCREMENT,
		kind       TEXT NOT NULL,
		scope      TEXT NOT NULL,
		agent      TEXT NOT NULL DEFAULT '',
		text       TEXT NOT NULL,
		source     TEXT NOT NULL DEFAULT '',
		created_at DATETIME NOT NULL
	)`,
	`CREATE VIRTUAL TABLE memories_fts USING fts5(
		text, content='memories', content_rowid='id'
	)`,
	`CREATE TRIGGER memories_ai AFTER INSERT ON memories BEGIN
		INSERT INTO memories_fts(rowid, text) VALUES (new.id, new.text);
	END`,
	`CREATE TRIGGER memories_ad AFTER DELETE ON memories BEGIN
		INSERT INTO memories_fts(memories_fts, rowid, text) VALUES ('delete', old.id, old.text);
	END`,
	`CREATE TRIGGER memories_au AFTER UPDATE ON memories BEGIN
		INSERT INTO memories_fts(memories_fts, rowid, text) VALUES ('delete', old.id, old.text);
		INSERT INTO memories_fts(rowid, text) VALUES (new.id, new.text);
	END`,
}

func TestMigrationFromTheLegacySchema(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "legacy.db")
	raw, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	for _, stmt := range legacyStatements {
		if _, err := raw.Exec(stmt); err != nil {
			t.Fatalf("legacy schema: %v", err)
		}
	}
	created := clockStart
	seed := []struct{ kind, scope, agent, text string }{
		{"fact", "project", "a", "project row of a about kubectl"},
		{"fact", "project", "", "project row without agent about kubectl"},
		{"fact", "global", "", "global row about kubectl"},
		{"fact", "global", "b", "global row with an agent about kubectl"},
		{"fact", "agent", "", "orphan agent row about kubectl"},
		{"fact", "agent", "b", "agent row of b about kubectl"},
	}
	for _, r := range seed {
		if _, err := raw.Exec(`INSERT INTO memories (kind, scope, agent, text, source, created_at) VALUES (?, ?, ?, ?, 'legacy', ?)`,
			r.kind, r.scope, r.agent, r.text, created); err != nil {
			t.Fatal(err)
		}
	}
	if err := raw.Close(); err != nil {
		t.Fatal(err)
	}

	s, err := OpenSQLite(path)
	if err != nil {
		t.Fatalf("OpenSQLite on a legacy database: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })

	var projects int
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM memories WHERE scope IN ('project', 'global')`).Scan(&projects); err != nil || projects != 0 {
		t.Fatalf("legacy scopes left behind: %d (err %v)", projects, err)
	}

	gotA, err := s.Recall(ctx, Reader{Agent: "a"}, "kubectl", 20)
	if err != nil {
		t.Fatal(err)
	}
	// Reader a sees: its project row, the two space rows, the global rows that
	// became space rows, and the orphan agent row that became space. Not b's agent row.
	if len(gotA) != 5 {
		t.Fatalf("reader a recalls %d legacy rows, want 5: %+v", len(gotA), gotA)
	}
	for _, e := range gotA {
		if e.Agent == "b" && e.Scope == ScopeAgent {
			t.Fatalf("reader a sees b's agent row: %+v", e)
		}
	}
	gotB, err := s.Recall(ctx, Reader{Agent: "b"}, "kubectl", 20)
	if err != nil || len(gotB) != 5 {
		t.Fatalf("reader b recalls %d legacy rows, want 5 (err %v)", len(gotB), err)
	}

	if e := gotA[0]; e.ReviewAfter.IsZero() || !e.ReviewAfter.Equal(created.AddDate(0, 6, 0)) {
		t.Fatalf("legacy fact review_after = %v, want created + 6 months", e.ReviewAfter)
	}
	var missingHash int
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM memories WHERE normalized_hash = ''`).Scan(&missingHash); err != nil || missingHash != 0 {
		t.Fatalf("legacy rows without a content hash: %d (err %v)", missingHash, err)
	}

	// Migrating is idempotent: reopening keeps the same rows, and a global
	// entry written after the migration is not rewritten to space.
	if _, err := s.Remember(ctx, Entry{Kind: KindFact, Scope: ScopeGlobal, Text: "root fact after migration"}); err != nil {
		t.Fatal(err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	s, err = OpenSQLite(path)
	if err != nil {
		t.Fatalf("second open: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	var globals int
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM memories WHERE scope = 'global'`).Scan(&globals); err != nil || globals != 1 {
		t.Fatalf("global rows after reopen = %d (err %v), want the one written after migration", globals, err)
	}
	var total int
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM memories`).Scan(&total); err != nil || total != len(seed)+1 {
		t.Fatalf("rows after reopen = %d (err %v)", total, err)
	}

	// Titles of new rows are indexed next to the text.
	if _, err := s.Remember(ctx, Entry{Kind: KindFact, Scope: ScopeSpace, Title: "Payments rota", Text: "on call this week"}); err != nil {
		t.Fatal(err)
	}
	if got, err := s.Recall(ctx, Reader{}, "rota", 5); err != nil || len(got) != 1 {
		t.Fatalf("title search after migration = %d (err %v)", len(got), err)
	}
}
