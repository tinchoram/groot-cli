package workspace

import "context"

// DefaultModel, SessionModel and SetDefaultModel reach the default model through
// the admin manager. SessionModel is the model of the conversation that is in
// view; groot's own tools run in it.
func (a adminManager) DefaultModel() string { return a.w.DefaultModel() }

func (a adminManager) SessionModel() string { return a.w.Chat().Model() }

func (a adminManager) SetDefaultModel(ctx context.Context, model string) (string, error) {
	return a.w.SetDefaultModel(ctx, model, false)
}
