package workspace

import (
	"context"
	"errors"

	"github.com/tinchoram/groot-cli/internal/config"
)

// OperatorName returns the operator's name without @, or "" when none is set.
func (w *Workspace) OperatorName() string {
	w.operatorMu.Lock()
	defer w.operatorMu.Unlock()
	return w.operator
}

// SetOperatorName validates the name, saves it to user.yaml, and refreshes the
// system prompts of the open conversations, so the People block names the new
// operator on the next turn. It returns the name as stored, without @.
func (w *Workspace) SetOperatorName(raw string) (string, error) {
	name, err := config.NormalizeUserName(raw)
	if err != nil {
		return "", err
	}
	if w.opts.UserConfigPath == "" {
		return "", errors.New("the name cannot be saved: no user.yaml path is configured")
	}
	if err := config.SaveUserName(w.opts.UserConfigPath, name); err != nil {
		return "", err
	}
	w.operatorMu.Lock()
	w.operator = name
	w.operatorMu.Unlock()
	w.mu.Lock()
	w.opts.Config.User.Name = name
	w.mu.Unlock()
	if err := w.refreshPrompts(context.Background()); err != nil {
		return name, err
	}
	return name, nil
}

// refreshPrompts recomputes the system prompt of every open conversation. The
// conversations are listed under the lock and refreshed outside it.
func (w *Workspace) refreshPrompts(ctx context.Context) error {
	w.mu.Lock()
	convs := make([]*Conversation, 0, len(w.convs))
	for _, c := range w.convs {
		convs = append(convs, c)
	}
	w.mu.Unlock()
	var errs []error
	for _, c := range convs {
		if err := c.refreshSystem(ctx); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}
