// Package inbox stores messages exchanged between agents and groot. SQLite
// is the only adapter; the database lives at <GROOT_HOME>/inbox.db.
package inbox

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
	"unicode/utf8"

	_ "modernc.org/sqlite" // pure-Go SQLite driver
)

const (
	// MaxSubjectRunes caps the subject length.
	MaxSubjectRunes = 200
	// MaxBodyBytes caps the body size.
	MaxBodyBytes = 16 * 1024
)

const schema = `CREATE TABLE IF NOT EXISTS messages (
	id         INTEGER PRIMARY KEY AUTOINCREMENT,
	from_space TEXT NOT NULL,
	from_agent TEXT NOT NULL,
	to_space   TEXT NOT NULL,
	to_agent   TEXT NOT NULL,
	subject    TEXT NOT NULL,
	body       TEXT NOT NULL,
	created_at INTEGER NOT NULL,
	read_at    INTEGER
);
CREATE INDEX IF NOT EXISTS messages_to ON messages(to_space, to_agent, read_at);`

// Message is one note between two agents.
type Message struct {
	ID        int64
	FromSpace string
	FromAgent string
	ToSpace   string
	ToAgent   string
	Subject   string
	Body      string
	CreatedAt time.Time
	ReadAt    *time.Time
}

// Store is the SQLite-backed inbox.
type Store struct {
	db *sql.DB
}

// Open opens (creating if needed) the inbox database at path.
func Open(path string) (*Store, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, fmt.Errorf("create inbox dir: %w", err)
	}
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, fmt.Errorf("open inbox: %w", err)
	}
	// One connection keeps writes serialized, as in the memory store.
	db.SetMaxOpenConns(1)
	if _, err := db.Exec(schema); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("apply inbox schema: %w", err)
	}
	return &Store{db: db}, nil
}

// Close releases the database handle.
func (s *Store) Close() error { return s.db.Close() }

// Send stores m and returns its id. Subject and body are validated first.
func (s *Store) Send(ctx context.Context, m Message) (int64, error) {
	if m.ToSpace == "" || m.ToAgent == "" {
		return 0, errors.New("recipient space and agent are required")
	}
	if strings.TrimSpace(m.Subject) == "" && strings.TrimSpace(m.Body) == "" {
		return 0, errors.New("a message needs a subject or a body")
	}
	if utf8.RuneCountInString(m.Subject) > MaxSubjectRunes {
		return 0, fmt.Errorf("subject exceeds %d characters", MaxSubjectRunes)
	}
	if len(m.Body) > MaxBodyBytes {
		return 0, fmt.Errorf("body exceeds %d bytes", MaxBodyBytes)
	}
	res, err := s.db.ExecContext(ctx,
		`INSERT INTO messages (from_space, from_agent, to_space, to_agent, subject, body, created_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?)`,
		m.FromSpace, m.FromAgent, m.ToSpace, m.ToAgent, m.Subject, m.Body, time.Now().UnixNano())
	if err != nil {
		return 0, fmt.Errorf("insert message: %w", err)
	}
	return res.LastInsertId()
}

// Unread returns the unread messages for space/agent, oldest first.
func (s *Store) Unread(ctx context.Context, space, agent string) ([]Message, error) {
	return s.query(ctx,
		`SELECT id, from_space, from_agent, to_space, to_agent, subject, body, created_at, read_at
		 FROM messages WHERE to_space = ? AND to_agent = ? AND read_at IS NULL ORDER BY id`,
		space, agent)
}

// List returns up to limit messages for space/agent, newest first, read or not.
func (s *Store) List(ctx context.Context, space, agent string, limit int) ([]Message, error) {
	return s.query(ctx,
		`SELECT id, from_space, from_agent, to_space, to_agent, subject, body, created_at, read_at
		 FROM messages WHERE to_space = ? AND to_agent = ? ORDER BY id DESC LIMIT ?`,
		space, agent, limit)
}

// MarkRead sets read_at on the given message ids.
func (s *Store) MarkRead(ctx context.Context, ids []int64) error {
	if len(ids) == 0 {
		return nil
	}
	now := time.Now().UnixNano()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	for _, id := range ids {
		if _, err := tx.ExecContext(ctx,
			`UPDATE messages SET read_at = ? WHERE id = ? AND read_at IS NULL`, now, id); err != nil {
			return fmt.Errorf("mark read: %w", err)
		}
	}
	return tx.Commit()
}

// CountUnread returns how many messages space/agent has not read yet.
func (s *Store) CountUnread(ctx context.Context, space, agent string) (int, error) {
	var n int
	err := s.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM messages WHERE to_space = ? AND to_agent = ? AND read_at IS NULL`,
		space, agent).Scan(&n)
	if err != nil {
		return 0, fmt.Errorf("count unread: %w", err)
	}
	return n, nil
}

// UnreadCounts returns the unread count per "space/agent" with at least one unread message.
func (s *Store) UnreadCounts(ctx context.Context) (map[string]int, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT to_space, to_agent, COUNT(*) FROM messages WHERE read_at IS NULL GROUP BY to_space, to_agent`)
	if err != nil {
		return nil, fmt.Errorf("count unread: %w", err)
	}
	defer rows.Close()
	counts := map[string]int{}
	for rows.Next() {
		var space, agent string
		var n int
		if err := rows.Scan(&space, &agent, &n); err != nil {
			return nil, err
		}
		counts[space+"/"+agent] = n
	}
	return counts, rows.Err()
}

func (s *Store) query(ctx context.Context, q string, args ...any) ([]Message, error) {
	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, fmt.Errorf("query inbox: %w", err)
	}
	defer rows.Close()
	var out []Message
	for rows.Next() {
		var m Message
		var created int64
		var read sql.NullInt64
		if err := rows.Scan(&m.ID, &m.FromSpace, &m.FromAgent, &m.ToSpace, &m.ToAgent,
			&m.Subject, &m.Body, &created, &read); err != nil {
			return nil, err
		}
		m.CreatedAt = time.Unix(0, created)
		if read.Valid {
			t := time.Unix(0, read.Int64)
			m.ReadAt = &t
		}
		out = append(out, m)
	}
	return out, rows.Err()
}
