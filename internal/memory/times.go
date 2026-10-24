package memory

import (
	"context"
	"database/sql"
	"fmt"
	"time"
)

// SessionTimes returns the time each stored message of a session was written,
// in the order of SessionMessages. A message written before timestamps existed
// has the zero time.
func (s *SQLite) SessionTimes(ctx context.Context, id string) ([]time.Time, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT created_at FROM messages WHERE session_id = ? ORDER BY id`, id)
	if err != nil {
		return nil, fmt.Errorf("session times: %w", err)
	}
	defer rows.Close()
	var out []time.Time
	for rows.Next() {
		var at sql.NullTime
		if err := rows.Scan(&at); err != nil {
			return nil, fmt.Errorf("scan time: %w", err)
		}
		if at.Valid {
			out = append(out, at.Time)
		} else {
			out = append(out, time.Time{})
		}
	}
	return out, rows.Err()
}
