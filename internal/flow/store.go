package flow

import "context"

// Store is the persistence port for tasks. Tasks and their evidence live in the
// database of the space they belong to. Get, Active and List return the
// evidence of each task in order.
type Store interface {
	// Open saves a new task and returns it with its id.
	Open(ctx context.Context, t Task) (Task, error)
	// Get returns one task by id.
	Get(ctx context.Context, id int64) (Task, error)
	// Active returns the open task of an agent in a space, if there is one.
	Active(ctx context.Context, space, agent string) (Task, bool, error)
	// Update saves the phase, plan path, timestamps and closing of a task.
	Update(ctx context.Context, t Task) error
	// AddEvidence appends one evidence record to a task.
	AddEvidence(ctx context.Context, taskID int64, ev Evidence) error
	// List returns the newest tasks of an agent, open or closed.
	List(ctx context.Context, space, agent string, limit int) ([]Task, error)
}
