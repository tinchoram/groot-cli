package memory

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
)

// CopyCounts says what CopyAgentData copied into the target database.
type CopyCounts struct {
	Memories int
	Sessions int
	Messages int
	Tasks    int
	Evidence int
	// SkippedSessions counts the sessions the target already has under the same
	// id. They are not copied, and neither are their messages.
	SkippedSessions int
}

// Columns copied for each table, in insert order. The task id and the evidence
// task id are new in the target and come from the copy, so they are not listed.
const (
	copyMemoryCols  = "kind, scope, agent, text, source, created_at"
	copySessionCols = "id, parent_id, agent, model, started_at, ended_at, cost_usd, cleared_at, context_tokens"
	copyMessageCols = "session_id, turn, role, content_json, in_tokens, out_tokens, created_at"
	copyTaskCols    = "space, agent, size, phase, goal, scope, done_when, plan_path, opened_at, phase_at, updated_at, closed_at, " +
		"delegate, manager, delegation_status, delegated_at, delegation_at, parent_space, parent_agent, parent_task"
	copyEvidenceCols = "task_id, phase, kind, text, at"
)

// parentTaskIndex is the position of parent_task in copyTaskCols.
const parentTaskIndex = 19

// CopyAgentData copies the memories, sessions (with their messages) and tasks
// (with their evidence) of agent from this database into dst. The copy is one
// transaction on dst. Session ids are kept; a session whose id dst already has
// is skipped with its messages. Task ids are new in dst, and the evidence and
// the parent link of each task follow them. A parent task that is not copied
// leaves the parent link empty. This database keeps its rows.
func (s *SQLite) CopyAgentData(ctx context.Context, dst *SQLite, agent string) (CopyCounts, error) {
	var c CopyCounts
	if dst == s {
		return c, errors.New("copy agent: source and target are the same database")
	}
	memories, err := selectAll(ctx, s.db,
		`SELECT kind, scope, agent, text, source, created_at FROM memories WHERE agent = ? ORDER BY id`, agent)
	if err != nil {
		return c, fmt.Errorf("copy agent: read memories: %w", err)
	}
	sessions, err := selectAll(ctx, s.db,
		`SELECT id, parent_id, agent, model, started_at, ended_at, cost_usd, cleared_at, context_tokens FROM sessions WHERE agent = ? ORDER BY started_at, id`, agent)
	if err != nil {
		return c, fmt.Errorf("copy agent: read sessions: %w", err)
	}
	tasks, err := selectAll(ctx, s.db,
		`SELECT id, space, agent, size, phase, goal, scope, done_when, plan_path, opened_at, phase_at, updated_at, closed_at, `+
			`delegate, manager, delegation_status, delegated_at, delegation_at, parent_space, parent_agent, parent_task `+
			`FROM tasks WHERE agent = ? ORDER BY id`, agent)
	if err != nil {
		return c, fmt.Errorf("copy agent: read tasks: %w", err)
	}

	tx, err := dst.db.BeginTx(ctx, nil)
	if err != nil {
		return c, fmt.Errorf("copy agent: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	if err := copyRows(ctx, tx, `INSERT INTO memories (`+copyMemoryCols+`) VALUES (?, ?, ?, ?, ?, ?)`, memories); err != nil {
		return c, fmt.Errorf("copy agent: memories: %w", err)
	}
	c.Memories = len(memories)

	for _, sess := range sessions {
		var exists int
		if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM sessions WHERE id = ?`, sess[0]).Scan(&exists); err != nil {
			return c, fmt.Errorf("copy agent: check session: %w", err)
		}
		if exists > 0 {
			c.SkippedSessions++
			continue
		}
		if err := copyRows(ctx, tx, `INSERT INTO sessions (`+copySessionCols+`) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`, [][]any{sess}); err != nil {
			return c, fmt.Errorf("copy agent: session: %w", err)
		}
		c.Sessions++
		msgs, err := selectAll(ctx, s.db,
			`SELECT session_id, turn, role, content_json, in_tokens, out_tokens, created_at FROM messages WHERE session_id = ? ORDER BY id`, sess[0])
		if err != nil {
			return c, fmt.Errorf("copy agent: read messages: %w", err)
		}
		if err := copyRows(ctx, tx, `INSERT INTO messages (`+copyMessageCols+`) VALUES (?, ?, ?, ?, ?, ?, ?)`, msgs); err != nil {
			return c, fmt.Errorf("copy agent: messages: %w", err)
		}
		c.Messages += len(msgs)
	}

	newID := map[int64]int64{}
	for _, tk := range tasks {
		oldID, _ := tk[0].(int64)
		row := tk[1:]
		parent, _ := row[parentTaskIndex].(int64)
		row[parentTaskIndex] = newID[parent] // zero when the parent was not copied
		res, err := tx.ExecContext(ctx, `INSERT INTO tasks (`+copyTaskCols+`) VALUES (`+placeholders(len(row))+`)`, row...)
		if err != nil {
			return c, fmt.Errorf("copy agent: task: %w", err)
		}
		id, err := res.LastInsertId()
		if err != nil {
			return c, fmt.Errorf("copy agent: task id: %w", err)
		}
		newID[oldID] = id
		c.Tasks++

		evidence, err := selectAll(ctx, s.db,
			`SELECT task_id, phase, kind, text, at FROM task_evidence WHERE task_id = ? ORDER BY id`, oldID)
		if err != nil {
			return c, fmt.Errorf("copy agent: read evidence: %w", err)
		}
		for _, ev := range evidence {
			ev[0] = id
		}
		if err := copyRows(ctx, tx, `INSERT INTO task_evidence (`+copyEvidenceCols+`) VALUES (?, ?, ?, ?, ?)`, evidence); err != nil {
			return c, fmt.Errorf("copy agent: evidence: %w", err)
		}
		c.Evidence += len(evidence)
	}

	if err := tx.Commit(); err != nil {
		return CopyCounts{}, fmt.Errorf("copy agent: commit: %w", err)
	}
	return c, nil
}

// selectAll reads every row of a query as raw values, so that timestamps and
// nulls are copied as the driver returns them.
func selectAll(ctx context.Context, db *sql.DB, query string, args ...any) ([][]any, error) {
	rows, err := db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	cols, err := rows.Columns()
	if err != nil {
		return nil, err
	}
	var out [][]any
	for rows.Next() {
		vals := make([]any, len(cols))
		ptrs := make([]any, len(cols))
		for i := range vals {
			ptrs[i] = &vals[i]
		}
		if err := rows.Scan(ptrs...); err != nil {
			return nil, err
		}
		out = append(out, vals)
	}
	return out, rows.Err()
}

// copyRows inserts each row with the statement, which takes one placeholder
// per value.
func copyRows(ctx context.Context, tx *sql.Tx, stmt string, rows [][]any) error {
	for _, row := range rows {
		if _, err := tx.ExecContext(ctx, stmt, row...); err != nil {
			return err
		}
	}
	return nil
}

// placeholders is the "?, ?, ..." list of n values.
func placeholders(n int) string {
	return strings.TrimSuffix(strings.Repeat("?, ", n), ", ")
}
