package memory

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/tinchoram/groot-cli/internal/flow"
)

// tasksSchema holds the ODD flow tasks of a space. CREATE ... IF NOT EXISTS
// makes it safe to run on every open. The delegation and parent columns are
// added by migrate, for databases written before them.
var tasksSchema = []string{
	`CREATE TABLE IF NOT EXISTS tasks (
		id         INTEGER PRIMARY KEY AUTOINCREMENT,
		space      TEXT NOT NULL,
		agent      TEXT NOT NULL,
		size       TEXT NOT NULL,
		phase      TEXT NOT NULL,
		goal       TEXT NOT NULL,
		scope      TEXT NOT NULL DEFAULT '',
		done_when  TEXT NOT NULL DEFAULT '',
		plan_path  TEXT NOT NULL DEFAULT '',
		opened_at  DATETIME NOT NULL,
		phase_at   DATETIME NOT NULL,
		updated_at DATETIME NOT NULL,
		closed_at  DATETIME NULL
	)`,
	`CREATE INDEX IF NOT EXISTS tasks_owner ON tasks(space, agent, closed_at)`,
	`CREATE TABLE IF NOT EXISTS task_evidence (
		id      INTEGER PRIMARY KEY AUTOINCREMENT,
		task_id INTEGER NOT NULL,
		phase   TEXT NOT NULL,
		kind    TEXT NOT NULL,
		text    TEXT NOT NULL,
		at      DATETIME NOT NULL
	)`,
	`CREATE INDEX IF NOT EXISTS task_evidence_task ON task_evidence(task_id, id)`,
}

// taskAddedColumns are the columns of the delegation record and the parent
// link, with the statement that adds each one to a database that lacks it.
var taskAddedColumns = []struct{ name, ddl string }{
	{"delegate", `ALTER TABLE tasks ADD COLUMN delegate TEXT NOT NULL DEFAULT ''`},
	{"manager", `ALTER TABLE tasks ADD COLUMN manager TEXT NOT NULL DEFAULT ''`},
	{"delegation_status", `ALTER TABLE tasks ADD COLUMN delegation_status TEXT NOT NULL DEFAULT ''`},
	{"delegated_at", `ALTER TABLE tasks ADD COLUMN delegated_at DATETIME NULL`},
	{"delegation_at", `ALTER TABLE tasks ADD COLUMN delegation_at DATETIME NULL`},
	{"parent_space", `ALTER TABLE tasks ADD COLUMN parent_space TEXT NOT NULL DEFAULT ''`},
	{"parent_agent", `ALTER TABLE tasks ADD COLUMN parent_agent TEXT NOT NULL DEFAULT ''`},
	{"parent_task", `ALTER TABLE tasks ADD COLUMN parent_task INTEGER NOT NULL DEFAULT 0`},
}

// Tasks returns the flow task store of this space database.
func (s *SQLite) Tasks() flow.Store { return taskStore{db: s.db} }

type taskStore struct {
	db *sql.DB
}

// nullTime stores the zero time as NULL.
func nullTime(t time.Time) sql.NullTime {
	return sql.NullTime{Time: t, Valid: !t.IsZero()}
}

func (s taskStore) Open(ctx context.Context, t flow.Task) (flow.Task, error) {
	res, err := s.db.ExecContext(ctx, `
		INSERT INTO tasks (space, agent, size, phase, goal, scope, done_when, plan_path, opened_at, phase_at, updated_at,
			delegate, manager, delegation_status, delegated_at, delegation_at, parent_space, parent_agent, parent_task)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		t.Space, t.Agent, string(t.Size), string(t.Phase), t.Goal, t.Scope, t.DoneWhen, t.PlanPath,
		t.OpenedAt, t.PhaseAt, t.UpdatedAt,
		t.Delegation.Delegate, t.Delegation.Manager, string(t.Delegation.Status),
		nullTime(t.Delegation.HandedAt), nullTime(t.Delegation.StatusAt),
		t.Parent.Space, t.Parent.Agent, t.Parent.Task)
	if err != nil {
		return flow.Task{}, fmt.Errorf("insert task: %w", err)
	}
	t.ID, err = res.LastInsertId()
	if err != nil {
		return flow.Task{}, fmt.Errorf("task id: %w", err)
	}
	return t, nil
}

func (s taskStore) Get(ctx context.Context, id int64) (flow.Task, error) {
	row := s.db.QueryRowContext(ctx, taskColumns+` WHERE id = ?`, id)
	t, err := scanTask(row)
	if err != nil {
		return flow.Task{}, fmt.Errorf("task #%d: %w", id, err)
	}
	if t.Evidence, err = s.evidence(ctx, t.ID); err != nil {
		return flow.Task{}, err
	}
	return t, nil
}

func (s taskStore) Active(ctx context.Context, space, agent string) (flow.Task, bool, error) {
	row := s.db.QueryRowContext(ctx, taskColumns+` WHERE space = ? AND agent = ? AND closed_at IS NULL ORDER BY id DESC LIMIT 1`,
		space, agent)
	t, err := scanTask(row)
	if err == sql.ErrNoRows {
		return flow.Task{}, false, nil
	}
	if err != nil {
		return flow.Task{}, false, fmt.Errorf("active task: %w", err)
	}
	if t.Evidence, err = s.evidence(ctx, t.ID); err != nil {
		return flow.Task{}, false, err
	}
	return t, true, nil
}

func (s taskStore) Update(ctx context.Context, t flow.Task) error {
	var closed sql.NullTime
	if !t.ClosedAt.IsZero() {
		closed = sql.NullTime{Time: t.ClosedAt, Valid: true}
	}
	_, err := s.db.ExecContext(ctx, `
		UPDATE tasks SET phase = ?, plan_path = ?, phase_at = ?, updated_at = ?, closed_at = ?,
			delegate = ?, manager = ?, delegation_status = ?, delegated_at = ?, delegation_at = ?
		WHERE id = ?`,
		string(t.Phase), t.PlanPath, t.PhaseAt, t.UpdatedAt, closed,
		t.Delegation.Delegate, t.Delegation.Manager, string(t.Delegation.Status),
		nullTime(t.Delegation.HandedAt), nullTime(t.Delegation.StatusAt), t.ID)
	if err != nil {
		return fmt.Errorf("update task #%d: %w", t.ID, err)
	}
	return nil
}

func (s taskStore) AddEvidence(ctx context.Context, taskID int64, ev flow.Evidence) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO task_evidence (task_id, phase, kind, text, at) VALUES (?, ?, ?, ?, ?)`,
		taskID, string(ev.Phase), string(ev.Kind), ev.Text, ev.At)
	if err != nil {
		return fmt.Errorf("add evidence: %w", err)
	}
	return nil
}

func (s taskStore) List(ctx context.Context, space, agent string, limit int) ([]flow.Task, error) {
	if limit <= 0 {
		limit = 10
	}
	rows, err := s.db.QueryContext(ctx, taskColumns+` WHERE space = ? AND agent = ? ORDER BY id DESC LIMIT ?`,
		space, agent, limit)
	if err != nil {
		return nil, fmt.Errorf("list tasks: %w", err)
	}
	var out []flow.Task
	for rows.Next() {
		t, err := scanTask(rows)
		if err != nil {
			_ = rows.Close()
			return nil, err
		}
		out = append(out, t)
	}
	if err := rows.Close(); err != nil {
		return nil, err
	}
	for i := range out {
		if out[i].Evidence, err = s.evidence(ctx, out[i].ID); err != nil {
			return nil, err
		}
	}
	return out, nil
}

// TaskSummary is one task as the listing across spaces shows it.
type TaskSummary struct {
	ID        int64
	Space     string
	Agent     string
	Size      flow.Size
	Phase     flow.Phase
	Goal      string
	Evidence  int
	OpenedAt  time.Time
	UpdatedAt time.Time
	// Delegation is the handover of the task, if it was handed over; Parent is
	// the origin, if another agent opened the task to take a handover.
	Delegation flow.Delegation
	Parent     flow.Parent
}

// TaskSummaries lists the open tasks of this space: every task whose phase is
// neither done nor failed. With includeClosed it also lists the done and failed
// tasks closed at or after closedSince. Rows are ordered by space, then by the
// newest update first.
func (s *SQLite) TaskSummaries(ctx context.Context, includeClosed bool, closedSince time.Time) ([]TaskSummary, error) {
	done, failed := string(flow.PhaseDone), string(flow.PhaseFailed)
	where := `t.phase NOT IN (?, ?)`
	args := []any{done, failed}
	if includeClosed {
		where += ` OR (t.phase IN (?, ?) AND COALESCE(t.closed_at, t.updated_at) >= ?)`
		args = append(args, done, failed, closedSince.UTC())
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT t.id, t.space, t.agent, t.size, t.phase, t.goal, t.opened_at, t.updated_at,
			(SELECT COUNT(*) FROM task_evidence e WHERE e.task_id = t.id),
			t.delegate, t.manager, t.delegation_status, t.delegated_at, t.delegation_at,
			t.parent_space, t.parent_agent, t.parent_task
		FROM tasks t
		WHERE `+where+`
		ORDER BY t.space, t.updated_at DESC, t.id DESC`, args...)
	if err != nil {
		return nil, fmt.Errorf("list task summaries: %w", err)
	}
	defer rows.Close()
	var out []TaskSummary
	for rows.Next() {
		var (
			r                         TaskSummary
			size, phase               string
			delegate, manager, status string
			delegatedAt, delegationAt sql.NullTime
			parentSpace, parentAgent  string
		)
		if err := rows.Scan(&r.ID, &r.Space, &r.Agent, &size, &phase, &r.Goal, &r.OpenedAt, &r.UpdatedAt, &r.Evidence,
			&delegate, &manager, &status, &delegatedAt, &delegationAt,
			&parentSpace, &parentAgent, &r.Parent.Task); err != nil {
			return nil, fmt.Errorf("scan task summary: %w", err)
		}
		r.Size, r.Phase = flow.Size(size), flow.Phase(phase)
		r.Delegation = delegationOf(delegate, manager, status, delegatedAt, delegationAt)
		r.Parent.Space, r.Parent.Agent = parentSpace, parentAgent
		out = append(out, r)
	}
	return out, rows.Err()
}

const taskColumns = `SELECT id, space, agent, size, phase, goal, scope, done_when, plan_path,
	opened_at, phase_at, updated_at, closed_at,
	delegate, manager, delegation_status, delegated_at, delegation_at,
	parent_space, parent_agent, parent_task FROM tasks`

type rowScanner interface {
	Scan(dest ...any) error
}

// delegationOf builds the handover record from its stored columns.
func delegationOf(delegate, manager, status string, handedAt, statusAt sql.NullTime) flow.Delegation {
	d := flow.Delegation{Delegate: delegate, Manager: manager, Status: flow.DelegationStatus(status)}
	if handedAt.Valid {
		d.HandedAt = handedAt.Time
	}
	if statusAt.Valid {
		d.StatusAt = statusAt.Time
	}
	return d
}

func scanTask(r rowScanner) (flow.Task, error) {
	var (
		t                         flow.Task
		size, phase               string
		closed                    sql.NullTime
		delegate, manager, status string
		delegatedAt, delegationAt sql.NullTime
	)
	err := r.Scan(&t.ID, &t.Space, &t.Agent, &size, &phase, &t.Goal, &t.Scope, &t.DoneWhen, &t.PlanPath,
		&t.OpenedAt, &t.PhaseAt, &t.UpdatedAt, &closed,
		&delegate, &manager, &status, &delegatedAt, &delegationAt,
		&t.Parent.Space, &t.Parent.Agent, &t.Parent.Task)
	if err != nil {
		return flow.Task{}, err
	}
	t.Size, t.Phase = flow.Size(size), flow.Phase(phase)
	if closed.Valid {
		t.ClosedAt = closed.Time
	}
	t.Delegation = delegationOf(delegate, manager, status, delegatedAt, delegationAt)
	return t, nil
}

func (s taskStore) evidence(ctx context.Context, taskID int64) ([]flow.Evidence, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT phase, kind, text, at FROM task_evidence WHERE task_id = ? ORDER BY id`, taskID)
	if err != nil {
		return nil, fmt.Errorf("task evidence: %w", err)
	}
	defer rows.Close()
	var out []flow.Evidence
	for rows.Next() {
		var ev flow.Evidence
		var phase, kind string
		var at time.Time
		if err := rows.Scan(&phase, &kind, &ev.Text, &at); err != nil {
			return nil, fmt.Errorf("scan evidence: %w", err)
		}
		ev.Phase, ev.Kind, ev.At = flow.Phase(phase), flow.EvidenceKind(kind), at
		out = append(out, ev)
	}
	return out, rows.Err()
}
