package memory

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	"github.com/tinchoram/groot-cli/internal/api"
)

// migrate applies the column additions that CREATE TABLE IF NOT EXISTS cannot.
// Each step checks first, so opening a database any number of times is safe.
func migrate(db *sql.DB) error {
	for _, stmt := range tasksSchema {
		if _, err := db.Exec(stmt); err != nil {
			return fmt.Errorf("apply task schema: %w", err)
		}
	}
	for _, c := range taskAddedColumns {
		has, err := hasColumn(db, "tasks", c.name)
		if err != nil {
			return fmt.Errorf("migrate tasks: %w", err)
		}
		if !has {
			if _, err := db.Exec(c.ddl); err != nil {
				return fmt.Errorf("migrate tasks: add %s: %w", c.name, err)
			}
		}
	}
	hasAt, err := hasColumn(db, "messages", "created_at")
	if err != nil {
		return fmt.Errorf("migrate messages: %w", err)
	}
	if !hasAt {
		if _, err := db.Exec(`ALTER TABLE messages ADD COLUMN created_at DATETIME NULL`); err != nil {
			return fmt.Errorf("migrate messages: add created_at: %w", err)
		}
	}
	has, err := hasColumn(db, "sessions", "cleared_at")
	if err != nil {
		return fmt.Errorf("migrate sessions: %w", err)
	}
	if !has {
		if _, err := db.Exec(`ALTER TABLE sessions ADD COLUMN cleared_at DATETIME NULL`); err != nil {
			return fmt.Errorf("migrate sessions: add cleared_at: %w", err)
		}
	}
	hasCtx, err := hasColumn(db, "sessions", "context_tokens")
	if err != nil {
		return fmt.Errorf("migrate sessions: %w", err)
	}
	if !hasCtx {
		if _, err := db.Exec(`ALTER TABLE sessions ADD COLUMN context_tokens INTEGER NOT NULL DEFAULT 0`); err != nil {
			return fmt.Errorf("migrate sessions: add context_tokens: %w", err)
		}
	}
	return migrateMemories(db)
}

func hasColumn(db *sql.DB, table, column string) (bool, error) {
	rows, err := db.Query(`SELECT name FROM pragma_table_info(?)`, table)
	if err != nil {
		return false, err
	}
	defer rows.Close()
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return false, err
		}
		if name == column {
			return true, nil
		}
	}
	return false, rows.Err()
}

// LatestSession returns the most recently started session of agent, including
// cleared ones, so that the caller can tell that the latest one was cleared.
func (s *SQLite) LatestSession(ctx context.Context, agent string) (Session, bool, error) {
	var (
		sess            Session
		ended, cleared  sql.NullTime
		messages, turns int
	)
	err := s.db.QueryRowContext(ctx, `
		SELECT ss.id, ss.agent, ss.model, ss.started_at, ss.ended_at, ss.cleared_at, ss.cost_usd, ss.context_tokens,
		       (SELECT COUNT(*) FROM messages m WHERE m.session_id = ss.id),
		       (SELECT COALESCE(MAX(m.turn), 0) FROM messages m WHERE m.session_id = ss.id)
		FROM sessions ss
		WHERE ss.agent = ?
		ORDER BY ss.started_at DESC, ss.rowid DESC
		LIMIT 1`, agent).Scan(&sess.ID, &sess.Agent, &sess.Model, &sess.StartedAt, &ended, &cleared, &sess.CostUSD, &sess.ContextTokens, &messages, &turns)
	if err == sql.ErrNoRows {
		return Session{}, false, nil
	}
	if err != nil {
		return Session{}, false, fmt.Errorf("latest session: %w", err)
	}
	if ended.Valid {
		sess.EndedAt = ended.Time
	}
	sess.Cleared = cleared.Valid
	sess.Messages = messages
	sess.Turns = turns
	return sess, true, nil
}

// SessionMessages returns the stored messages of a session in the order they were written.
func (s *SQLite) SessionMessages(ctx context.Context, id string) ([]api.Message, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT content_json FROM messages WHERE session_id = ? ORDER BY id`, id)
	if err != nil {
		return nil, fmt.Errorf("session messages: %w", err)
	}
	defer rows.Close()
	var out []api.Message
	for rows.Next() {
		var raw string
		if err := rows.Scan(&raw); err != nil {
			return nil, fmt.Errorf("scan message: %w", err)
		}
		var m api.Message
		if err := json.Unmarshal([]byte(raw), &m); err != nil {
			return nil, fmt.Errorf("decode message: %w", err)
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

// MarkCleared records the time the user cleared the session.
func (s *SQLite) MarkCleared(ctx context.Context, id string) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE sessions SET cleared_at = ? WHERE id = ?`, time.Now().UTC(), id)
	return err
}
