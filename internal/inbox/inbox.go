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
	// MaxAckRunes caps the body of an acknowledgement.
	MaxAckRunes = 200
)

// Kinds of inbox rows. A message is delivered into the agent's turn and counts
// as unread. An ack is a short confirmation: it is shown once as a note in the
// transcript of the recipient, never injected into a turn, and never counted.
const (
	KindMessage = "message"
	KindAck     = "ack"
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
	read_at    INTEGER,
	kind       TEXT NOT NULL DEFAULT 'message'
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
	// Kind is KindMessage or KindAck. Empty means KindMessage on Send.
	Kind string
}

// IsAck reports whether m is an acknowledgement rather than a message.
func (m Message) IsAck() bool { return m.Kind == KindAck }

// Store is the SQLite-backed inbox.
type Store struct {
	db *sql.DB
}

// Open opens (creating if needed) the inbox database at path. A database
// written before the kind column existed is migrated in place: its rows are
// messages.
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
	if err := addKindColumn(db); err != nil {
		_ = db.Close()
		return nil, err
	}
	return &Store{db: db}, nil
}

// addKindColumn adds the kind column to a messages table that lacks it. The
// default fills the existing rows with KindMessage.
func addKindColumn(db *sql.DB) error {
	rows, err := db.Query(`PRAGMA table_info(messages)`)
	if err != nil {
		return fmt.Errorf("inspect inbox: %w", err)
	}
	found := false
	for rows.Next() {
		var cid, notnull, pk int
		var name, typ string
		var dflt sql.NullString
		if err := rows.Scan(&cid, &name, &typ, &notnull, &dflt, &pk); err != nil {
			_ = rows.Close()
			return fmt.Errorf("inspect inbox: %w", err)
		}
		if name == "kind" {
			found = true
		}
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return fmt.Errorf("inspect inbox: %w", err)
	}
	_ = rows.Close()
	if found {
		return nil
	}
	if _, err := db.Exec(`ALTER TABLE messages ADD COLUMN kind TEXT NOT NULL DEFAULT 'message'`); err != nil {
		return fmt.Errorf("migrate inbox kind: %w", err)
	}
	return nil
}

// Close releases the database handle.
func (s *Store) Close() error { return s.db.Close() }

// Send stores m and returns its id. Subject, body and kind are validated first.
func (s *Store) Send(ctx context.Context, m Message) (int64, error) {
	if m.ToSpace == "" || m.ToAgent == "" {
		return 0, errors.New("recipient space and agent are required")
	}
	if m.Kind == "" {
		m.Kind = KindMessage
	}
	if m.Kind != KindMessage && m.Kind != KindAck {
		return 0, fmt.Errorf("unknown message kind %q", m.Kind)
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
	if m.Kind == KindAck && utf8.RuneCountInString(m.Body) > MaxAckRunes {
		return 0, fmt.Errorf("ack body exceeds %d characters", MaxAckRunes)
	}
	res, err := s.db.ExecContext(ctx,
		`INSERT INTO messages (from_space, from_agent, to_space, to_agent, subject, body, created_at, kind)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		m.FromSpace, m.FromAgent, m.ToSpace, m.ToAgent, m.Subject, m.Body, time.Now().UnixNano(), m.Kind)
	if err != nil {
		return 0, fmt.Errorf("insert message: %w", err)
	}
	return res.LastInsertId()
}

// Unread returns the unread messages for space/agent, oldest first. Acks are
// not messages and are left out: they never reach a turn.
func (s *Store) Unread(ctx context.Context, space, agent string) ([]Message, error) {
	return s.query(ctx,
		`SELECT id, from_space, from_agent, to_space, to_agent, subject, body, created_at, read_at, kind
		 FROM messages WHERE to_space = ? AND to_agent = ? AND read_at IS NULL AND kind = 'message' ORDER BY id`,
		space, agent)
}

// PendingAcks returns the acks for space/agent that nobody has shown yet, oldest first.
func (s *Store) PendingAcks(ctx context.Context, space, agent string) ([]Message, error) {
	return s.query(ctx,
		`SELECT id, from_space, from_agent, to_space, to_agent, subject, body, created_at, read_at, kind
		 FROM messages WHERE to_space = ? AND to_agent = ? AND read_at IS NULL AND kind = 'ack' ORDER BY id`,
		space, agent)
}

// List returns up to limit messages for space/agent, newest first, read or not.
// Acks are included, with their kind, so the history keeps them.
func (s *Store) List(ctx context.Context, space, agent string, limit int) ([]Message, error) {
	return s.query(ctx,
		`SELECT id, from_space, from_agent, to_space, to_agent, subject, body, created_at, read_at, kind
		 FROM messages WHERE to_space = ? AND to_agent = ? ORDER BY id DESC LIMIT ?`,
		space, agent, limit)
}

// MarkRead sets read_at on the given message ids.
func (s *Store) MarkRead(ctx context.Context, ids []int64) error {
	return s.markRead(ctx, ids, "")
}

// MarkAcksRead sets read_at on the given ack ids. A message id in the list is
// left unread.
func (s *Store) MarkAcksRead(ctx context.Context, ids []int64) error {
	return s.markRead(ctx, ids, " AND kind = 'ack'")
}

func (s *Store) markRead(ctx context.Context, ids []int64, only string) error {
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
			`UPDATE messages SET read_at = ? WHERE id = ? AND read_at IS NULL`+only, now, id); err != nil {
			return fmt.Errorf("mark read: %w", err)
		}
	}
	return tx.Commit()
}

// CountUnread returns how many messages space/agent has not read yet. Acks are
// not counted.
func (s *Store) CountUnread(ctx context.Context, space, agent string) (int, error) {
	var n int
	err := s.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM messages WHERE to_space = ? AND to_agent = ? AND read_at IS NULL AND kind = 'message'`,
		space, agent).Scan(&n)
	if err != nil {
		return 0, fmt.Errorf("count unread: %w", err)
	}
	return n, nil
}

// UnreadCounts returns the unread count per "space/agent" with at least one unread message. Acks are not counted.
func (s *Store) UnreadCounts(ctx context.Context) (map[string]int, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT to_space, to_agent, COUNT(*) FROM messages WHERE read_at IS NULL AND kind = 'message' GROUP BY to_space, to_agent`)
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
			&m.Subject, &m.Body, &created, &read, &m.Kind); err != nil {
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
