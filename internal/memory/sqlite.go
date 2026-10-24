package memory

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/tinchoram/groot-cli/internal/api"

	_ "modernc.org/sqlite" // pure-Go SQLite driver, FTS5 enabled
)

// SQLite is the default Store adapter.
type SQLite struct {
	db *sql.DB
}

var schema = []string{
	`CREATE TABLE IF NOT EXISTS sessions (
		id         TEXT PRIMARY KEY,
		parent_id  TEXT NULL,
		agent      TEXT NOT NULL DEFAULT '',
		model      TEXT NOT NULL DEFAULT '',
		started_at DATETIME NOT NULL,
		ended_at   DATETIME NULL,
		cost_usd   REAL NOT NULL DEFAULT 0
	)`,
	`CREATE TABLE IF NOT EXISTS messages (
		id           INTEGER PRIMARY KEY AUTOINCREMENT,
		session_id   TEXT NOT NULL,
		turn         INTEGER NOT NULL,
		role         TEXT NOT NULL,
		content_json TEXT NOT NULL,
		in_tokens    INTEGER NOT NULL DEFAULT 0,
		out_tokens   INTEGER NOT NULL DEFAULT 0
	)`,
	`CREATE INDEX IF NOT EXISTS messages_session ON messages(session_id, id)`,
	`CREATE TABLE IF NOT EXISTS memories (
		id         INTEGER PRIMARY KEY AUTOINCREMENT,
		kind       TEXT NOT NULL,
		scope      TEXT NOT NULL,
		agent      TEXT NOT NULL DEFAULT '',
		text       TEXT NOT NULL,
		source     TEXT NOT NULL DEFAULT '',
		created_at DATETIME NOT NULL
	)`,
	`CREATE VIRTUAL TABLE IF NOT EXISTS memories_fts USING fts5(
		text, content='memories', content_rowid='id'
	)`,
	`CREATE TRIGGER IF NOT EXISTS memories_ai AFTER INSERT ON memories BEGIN
		INSERT INTO memories_fts(rowid, text) VALUES (new.id, new.text);
	END`,
	`CREATE TRIGGER IF NOT EXISTS memories_ad AFTER DELETE ON memories BEGIN
		INSERT INTO memories_fts(memories_fts, rowid, text) VALUES ('delete', old.id, old.text);
	END`,
	`CREATE TRIGGER IF NOT EXISTS memories_au AFTER UPDATE ON memories BEGIN
		INSERT INTO memories_fts(memories_fts, rowid, text) VALUES ('delete', old.id, old.text);
		INSERT INTO memories_fts(rowid, text) VALUES (new.id, new.text);
	END`,
}

// OpenSQLite opens (creating if needed) the database at path and applies the schema.
func OpenSQLite(path string) (*SQLite, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, fmt.Errorf("create memory dir: %w", err)
	}
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, fmt.Errorf("open sqlite: %w", err)
	}
	// One connection keeps writes serialized and avoids SQLITE_BUSY in a CLI.
	db.SetMaxOpenConns(1)
	for _, stmt := range schema {
		if _, err := db.Exec(stmt); err != nil {
			_ = db.Close()
			return nil, fmt.Errorf("apply schema: %w", err)
		}
	}
	if err := migrate(db); err != nil {
		_ = db.Close()
		return nil, err
	}
	return &SQLite{db: db}, nil
}

// Close releases the database handle.
func (s *SQLite) Close() error { return s.db.Close() }

func nullIfEmpty(v string) any {
	if v == "" {
		return nil
	}
	return v
}

func (s *SQLite) StartSession(ctx context.Context, sess Session) error {
	if sess.ID == "" {
		return errors.New("session id is required")
	}
	started := sess.StartedAt
	if started.IsZero() {
		started = time.Now().UTC()
	}
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO sessions (id, parent_id, agent, model, started_at) VALUES (?, ?, ?, ?, ?)`,
		sess.ID, nullIfEmpty(sess.ParentID), sess.Agent, sess.Model, started)
	return err
}

func (s *SQLite) EndSession(ctx context.Context, id string, costUSD float64) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE sessions SET ended_at = ?, cost_usd = ? WHERE id = ?`,
		time.Now().UTC(), costUSD, id)
	return err
}

func (s *SQLite) AppendMessage(ctx context.Context, sessionID string, turn int, m api.Message, u api.Usage) error {
	raw, err := json.Marshal(m)
	if err != nil {
		return fmt.Errorf("encode message: %w", err)
	}
	_, err = s.db.ExecContext(ctx,
		`INSERT INTO messages (session_id, turn, role, content_json, in_tokens, out_tokens, created_at) VALUES (?, ?, ?, ?, ?, ?, ?)`,
		sessionID, turn, string(m.Role), string(raw), u.InputTokens, u.OutputTokens, time.Now().UTC())
	return err
}

func (s *SQLite) Remember(ctx context.Context, e Entry) error {
	if !e.Kind.Valid() {
		return fmt.Errorf("invalid memory kind %q", e.Kind)
	}
	if strings.TrimSpace(e.Text) == "" {
		return errors.New("memory text is empty")
	}
	if e.Scope == "" {
		e.Scope = ScopeProject
	}
	if !e.Scope.Valid() {
		return fmt.Errorf("invalid memory scope %q", e.Scope)
	}
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO memories (kind, scope, agent, text, source, created_at) VALUES (?, ?, ?, ?, ?, ?)`,
		string(e.Kind), string(e.Scope), e.Agent, e.Text, e.Source, time.Now().UTC())
	return err
}

// ftsQuery turns free text into a conjunction of quoted FTS5 terms, so that
// punctuation typed by a user cannot be interpreted as FTS syntax.
func ftsQuery(query string) string {
	var terms []string
	for _, f := range strings.Fields(query) {
		f = strings.ReplaceAll(f, `"`, "")
		if f != "" {
			terms = append(terms, `"`+f+`"`)
		}
	}
	return strings.Join(terms, " ")
}

func (s *SQLite) Recall(ctx context.Context, query, agent string, limit int) ([]Entry, error) {
	match := ftsQuery(query)
	if match == "" {
		return nil, nil
	}
	if limit <= 0 {
		limit = 10
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT m.id, m.kind, m.scope, m.agent, m.text, m.source, m.created_at
		FROM memories_fts
		JOIN memories m ON m.id = memories_fts.rowid
		WHERE memories_fts MATCH ? AND `+Visible+`
		ORDER BY memories_fts.rank
		LIMIT ?`, match, agent, agent, limit)
	if err != nil {
		return nil, fmt.Errorf("recall: %w", err)
	}
	defer rows.Close()
	return scanEntries(rows)
}

func (s *SQLite) Preamble(ctx context.Context, agent string, limit int) (string, error) {
	if limit <= 0 {
		limit = 10
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT m.id, m.kind, m.scope, m.agent, m.text, m.source, m.created_at
		FROM memories m
		WHERE `+Visible+`
		ORDER BY m.id DESC
		LIMIT ?`, agent, agent, limit)
	if err != nil {
		return "", fmt.Errorf("preamble: %w", err)
	}
	defer rows.Close()
	entries, err := scanEntries(rows)
	if err != nil {
		return "", err
	}
	var sb strings.Builder
	for _, e := range entries {
		fmt.Fprintf(&sb, "- [%s] %s\n", e.Kind, e.Text)
	}
	return sb.String(), nil
}

func scanEntries(rows *sql.Rows) ([]Entry, error) {
	var out []Entry
	for rows.Next() {
		var e Entry
		var kind, scope string
		if err := rows.Scan(&e.ID, &kind, &scope, &e.Agent, &e.Text, &e.Source, &e.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan memory: %w", err)
		}
		e.Kind = Kind(kind)
		e.Scope = Scope(scope)
		out = append(out, e)
	}
	return out, rows.Err()
}
