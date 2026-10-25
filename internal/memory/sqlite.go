package memory

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/tinchoram/groot-cli/internal/api"

	_ "modernc.org/sqlite" // pure-Go SQLite driver, FTS5 enabled
)

// SQLite is the default Store adapter.
type SQLite struct {
	db  *sql.DB
	now func() time.Time // the store's clock; tests replace it
}

// queryer is what the memory helpers need: a *sql.DB or a *sql.Tx. With one
// connection (SetMaxOpenConns(1)), code inside a transaction must use the tx.
type queryer interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
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
	// The full-text index, its triggers, the hygiene columns and the indexes
	// are created by migrateMemories, which also upgrades older databases.
	`CREATE TABLE IF NOT EXISTS schema_meta (
		key   TEXT PRIMARY KEY,
		value TEXT NOT NULL
	)`,
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
	return &SQLite{db: db, now: func() time.Time { return time.Now().UTC() }}, nil
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

// SetSessionCost stores the spend of a session so far.
func (s *SQLite) SetSessionCost(ctx context.Context, id string, costUSD float64) error {
	_, err := s.db.ExecContext(ctx, `UPDATE sessions SET cost_usd = ? WHERE id = ?`, costUSD, id)
	return err
}

// SetContextTokens stores the context size of the last model answer.
func (s *SQLite) SetContextTokens(ctx context.Context, id string, tokens int64) error {
	_, err := s.db.ExecContext(ctx, `UPDATE sessions SET context_tokens = ? WHERE id = ?`, tokens, id)
	return err
}

// SpendUSD sums the stored cost of every session of agent.
func (s *SQLite) SpendUSD(ctx context.Context, agent string) (float64, error) {
	var total float64
	err := s.db.QueryRowContext(ctx,
		`SELECT COALESCE(SUM(cost_usd), 0) FROM sessions WHERE agent = ?`, agent).Scan(&total)
	if err != nil {
		return 0, fmt.Errorf("spend of %s: %w", agent, err)
	}
	return total, nil
}

// SpendByAgent sums the stored cost of the sessions of each agent.
func (s *SQLite) SpendByAgent(ctx context.Context) (map[string]float64, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT agent, COALESCE(SUM(cost_usd), 0) FROM sessions GROUP BY agent`)
	if err != nil {
		return nil, fmt.Errorf("spend by agent: %w", err)
	}
	defer rows.Close()
	out := map[string]float64{}
	for rows.Next() {
		var agent string
		var total float64
		if err := rows.Scan(&agent, &total); err != nil {
			return nil, fmt.Errorf("scan spend: %w", err)
		}
		out[agent] = total
	}
	return out, rows.Err()
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
