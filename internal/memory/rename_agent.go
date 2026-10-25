package memory

import (
	"context"
	"fmt"
)

// renameStatements rename an agent in the columns that name agents: the task
// owner, the delegate, the manager that tracks a handover and the origin of a
// handed-over task, the memory entries and the sessions. parent_space is left
// alone: it names a space, not an agent.
var renameStatements = []string{
	`UPDATE tasks SET agent = ? WHERE agent = ?`,
	`UPDATE tasks SET delegate = ? WHERE delegate = ?`,
	`UPDATE tasks SET manager = ? WHERE manager = ?`,
	`UPDATE tasks SET parent_agent = ? WHERE parent_agent = ?`,
	`UPDATE memories SET agent = ? WHERE agent = ?`,
	`UPDATE sessions SET agent = ? WHERE agent = ?`,
}

// RenameAgent renames an agent in this space's database, in one transaction.
// Messages are not part of it: they live in the inbox.
func (s *SQLite) RenameAgent(ctx context.Context, oldName, newName string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("rename agent: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	for _, q := range renameStatements {
		if _, err := tx.ExecContext(ctx, q, newName, oldName); err != nil {
			return fmt.Errorf("rename agent: %w", err)
		}
	}
	return tx.Commit()
}
