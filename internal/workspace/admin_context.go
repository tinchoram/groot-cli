package workspace

import "context"

// ContextReport and SetContextWindow let the administrator agent read the
// context table and set model windows through the admin manager.
func (a adminManager) ContextReport(ctx context.Context, space string) (string, error) {
	return a.w.ContextReport(ctx, space)
}

func (a adminManager) SetContextWindow(model string, tokens int) error {
	return a.w.SetContextWindow(model, tokens)
}
