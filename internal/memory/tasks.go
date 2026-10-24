package memory

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/tinchoram/groot-cli/internal/flow"
)

// tasksSchema holds the ODD flow tasks of a space. CREATE ... IF NOT EXISTS
// makes it safe to run on every open.
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

// Tasks returns the flow task store of this space database.
func (s *SQLite) Tasks() flow.Store { return taskStore{db: s.db} }

type taskStore struct {
	db *sql.DB
}

func (s taskStore) Open(ctx context.Context, t flow.Task) (flow.Task, error) {
	res, err := s.db.ExecContext(ctx, `
		INSERT INTO tasks (space, agent, size, phase, goal, scope, done_when, plan_path, opened_at, phase_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		t.Space, t.Agent, string(t.Size), string(t.Phase), t.Goal, t.Scope, t.DoneWhen, t.PlanPath,
		t.OpenedAt, t.PhaseAt, t.UpdatedAt)
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
		UPDATE tasks SET phase = ?, plan_path = ?, phase_at = ?, updated_at = ?, closed_at = ?
		WHERE id = ?`,
		string(t.Phase), t.PlanPath, t.PhaseAt, t.UpdatedAt, closed, t.ID)
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

const taskColumns = `SELECT id, space, agent, size, phase, goal, scope, done_when, plan_path,
	opened_at, phase_at, updated_at, closed_at FROM tasks`

type rowScanner interface {
	Scan(dest ...any) error
}

func scanTask(r rowScanner) (flow.Task, error) {
	var (
		t           flow.Task
		size, phase string
		closed      sql.NullTime
	)
	err := r.Scan(&t.ID, &t.Space, &t.Agent, &size, &phase, &t.Goal, &t.Scope, &t.DoneWhen, &t.PlanPath,
		&t.OpenedAt, &t.PhaseAt, &t.UpdatedAt, &closed)
	if err != nil {
		return flow.Task{}, err
	}
	t.Size, t.Phase = flow.Size(size), flow.Phase(phase)
	if closed.Valid {
		t.ClosedAt = closed.Time
	}
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
