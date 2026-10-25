package workspace

import (
	"context"
	"os"

	"github.com/tinchoram/groot-cli/internal/config"
)

// DefaultModel is the model that conversations use when their agent sets none:
// provider.model from the configuration, or the provider's own default when the
// configuration names none.
func (w *Workspace) DefaultModel() string {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.defaultModelLocked()
}

// defaultModelLocked is DefaultModel for callers that hold w.mu.
func (w *Workspace) defaultModelLocked() string {
	if id := w.opts.Config.Provider.Model; id != "" {
		return id
	}
	return w.opts.NewProvider("").Model()
}

// ConfigFile is the configuration file that the workspace writes: the project
// file when it exists, otherwise the global file in the groot home.
func (w *Workspace) ConfigFile() string {
	project := w.opts.ConfigPath
	if project == "" {
		project = "groot.yaml"
	}
	if w.opts.GlobalConfigPath == "" {
		return project
	}
	if _, err := os.Stat(project); err == nil {
		return project
	}
	return w.opts.GlobalConfigPath
}

// SetDefaultModel makes model the default. The id is checked like /model's, unless
// force is set. The configuration file gets provider.model, and the new default
// applies to conversations opened from now on. Conversations already open keep
// their model, and agents with their own model keep it too.
func (w *Workspace) SetDefaultModel(ctx context.Context, model string, force bool) (string, error) {
	warn, err := w.CheckModel(ctx, model, force)
	if err != nil {
		return "", err
	}
	if _, err := config.SetProviderModel(w.ConfigFile(), model); err != nil {
		return "", err
	}
	w.mu.Lock()
	w.opts.Config.Provider.Model = model
	w.mu.Unlock()
	return warn, nil
}
