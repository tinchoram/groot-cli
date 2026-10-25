package inbox

import (
	"context"
	"fmt"
)

// RenameSpace moves every message that names oldName, as sender or recipient,
// to newName, so unread messages stay reachable. It returns how many messages
// were addressed to the space.
func (s *Store) RenameSpace(ctx context.Context, oldName, newName string) (int64, error) {
	return s.renameColumns(ctx, "to_space", "from_space", oldName, newName)
}

// RenameAgent moves every message that names oldName, as sender or recipient,
// to newName. Agent names are global in the catalog, so the rename applies in
// every space. It returns how many messages were addressed to the agent.
func (s *Store) RenameAgent(ctx context.Context, oldName, newName string) (int64, error) {
	return s.renameColumns(ctx, "to_agent", "from_agent", oldName, newName)
}

func (s *Store) renameColumns(ctx context.Context, toCol, fromCol, oldName, newName string) (int64, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, fmt.Errorf("rename: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	res, err := tx.ExecContext(ctx, `UPDATE messages SET `+toCol+` = ? WHERE `+toCol+` = ?`, newName, oldName)
	if err != nil {
		return 0, fmt.Errorf("rename recipients: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("rename recipients: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `UPDATE messages SET `+fromCol+` = ? WHERE `+fromCol+` = ?`, newName, oldName); err != nil {
		return 0, fmt.Errorf("rename senders: %w", err)
	}
	return n, tx.Commit()
}
