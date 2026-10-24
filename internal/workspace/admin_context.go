package workspace

import (
	"context"

	"github.com/tinchoram/groot-cli/internal/audit"
)

// AuditReport lets the administrator agent read the audit records of a space.
func (a adminManager) AuditReport(ctx context.Context, space string, q audit.Query) (string, error) {
	return a.w.AuditReport(ctx, space, q)
}

// ContextReport and SetContextWindow let the administrator agent read the
// context table and set model windows through the admin manager.
func (a adminManager) ContextReport(ctx context.Context, space string) (string, error) {
	return a.w.ContextReport(ctx, space)
}

func (a adminManager) SetContextWindow(model string, tokens int) error {
	return a.w.SetContextWindow(model, tokens)
}

// TaskReport lets the administrator agent list the open tasks through the admin manager.
func (a adminManager) TaskReport(ctx context.Context, space string, includeClosed bool) (string, error) {
	return a.w.TaskReport(ctx, space, includeClosed)
}
