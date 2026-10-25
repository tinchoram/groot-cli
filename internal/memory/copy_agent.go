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
	// SkippedMemories counts the memories the target already has: the same
	// content hash, or the same topic key in the same scope and agent.
	SkippedMemories int
	Sessions        int
	Messages        int
	Tasks           int
	// SkippedTasks counts the tasks the target already has under the same agent,
	// goal and opened time. Their evidence and specs are not copied either, and
	// the parent links of copied tasks point at the existing task.
	SkippedTasks int
	Evidence     int
	// SkippedSessions counts the sessions the target already has under the same
	// id. They are not copied, and neither are their messages.
	SkippedSessions int
}

// Columns copied for each table, in insert order. The task id and the evidence
// task id are new in the target and come from the copy, so they are not listed.
const (
	copyMemoryCols  = "kind, scope, agent, title, text, topic_key, source, normalized_hash, pinned, tainted, review_after, revision_count, duplicate_count, created_at, updated_at, deleted_at"
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
		`SELECT kind, scope, agent, title, text, topic_key, source, pinned, tainted, review_after, revision_count, duplicate_count,
			created_at, updated_at, deleted_at FROM memories WHERE agent = ? AND deleted_at = '' ORDER BY id`, agent)
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

	for _, m := range memories {
		// Columns as selected: kind, scope, agent, title, text, topic_key, source,
		// pinned, tainted, review_after, revision_count, duplicate_count,
		// created_at, updated_at, deleted_at. The hash is recomputed rather than
		// copied, so the copy never trusts the source's value.
		kind, scope, agent := Kind(textOf(m[0])), Scope(textOf(m[1])), textOf(m[2])
		title, text, topic := textOf(m[3]), textOf(m[4]), textOf(m[5])
		hash := contentHash(kind, scope, agent, title, text)
		row := []any{m[0], m[1], m[2], m[3], m[4], m[5], m[6], hash, m[7], m[8], m[9], m[10], m[11], m[12], m[13], m[14]}
		exists, err := hasSameMemory(ctx, tx, scope, agent, topic, hash)
		if err != nil {
			return c, fmt.Errorf("copy agent: check memory: %w", err)
		}
		if exists {
			c.SkippedMemories++
			continue
		}
		if err := copyRows(ctx, tx, `INSERT INTO memories (`+copyMemoryCols+`) VALUES (`+placeholders(16)+`)`, [][]any{row}); err != nil {
			return c, fmt.Errorf("copy agent: memories: %w", err)
		}
		c.Memories++
	}

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
		// A task is identified in the target by its agent, goal and opened time.
		// A task the target already has is skipped with its evidence and specs,
		// so copying twice does not duplicate them.
		twin, found, err := existingTask(ctx, tx, row[1], row[4], row[8])
		if err != nil {
			return c, fmt.Errorf("copy agent: check task: %w", err)
		}
		if found {
			newID[oldID] = twin
			c.SkippedTasks++
			continue
		}
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

		specs, err := selectAll(ctx, s.db,
			`SELECT task_id, n, text, status, note, at FROM task_specs WHERE task_id = ? ORDER BY n`, oldID)
		if err != nil {
			return c, fmt.Errorf("copy agent: read specs: %w", err)
		}
		for _, sp := range specs {
			sp[0] = id
		}
		if err := copyRows(ctx, tx, `INSERT INTO task_specs (task_id, n, text, status, note, at) VALUES (?, ?, ?, ?, ?, ?)`, specs); err != nil {
			return c, fmt.Errorf("copy agent: specs: %w", err)
		}
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

// existingTask returns the id of a task of tx with the same agent, goal and
// opened time. The opened time is compared as the driver returned it, so its
// sub-second part takes part in the match.
func existingTask(ctx context.Context, tx *sql.Tx, agent, goal, openedAt any) (int64, bool, error) {
	var id int64
	err := tx.QueryRowContext(ctx,
		`SELECT id FROM tasks WHERE agent = ? AND goal = ? AND opened_at = ? ORDER BY id LIMIT 1`,
		agent, goal, openedAt).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, false, nil
	}
	if err != nil {
		return 0, false, err
	}
	return id, true, nil
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

// hasSameMemory reports whether tx already holds a live memory with the same
// content hash, or with the same topic key in the same scope and agent.
func hasSameMemory(ctx context.Context, tx *sql.Tx, scope Scope, agent, topic, hash string) (bool, error) {
	var n int
	if err := tx.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM memories WHERE normalized_hash = ? AND deleted_at = ''`, hash).Scan(&n); err != nil {
		return false, err
	}
	if n == 0 && topic != "" {
		if err := tx.QueryRowContext(ctx,
			`SELECT COUNT(*) FROM memories WHERE scope = ? AND agent = ? AND topic_key = ? AND deleted_at = ''`,
			string(scope), agent, topic).Scan(&n); err != nil {
			return false, err
		}
	}
	return n > 0, nil
}

// textOf reads a text value as the driver returns it; nulls read as empty.
func textOf(v any) string {
	s, _ := v.(string)
	return s
}
