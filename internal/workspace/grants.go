package workspace

import (
	"fmt"

	"github.com/tinchoram/groot-cli/internal/gate"
)

// grantsFor returns the session grants of a conversation, created on first use.
// Callers hold w.mu.
func (w *Workspace) grantsFor(key string) *gate.Grants {
	if g, ok := w.grants[key]; ok {
		return g
	}
	g := gate.NewGrants()
	w.grants[key] = g
	return g
}

// GrantSession allows tool for the rest of the session in one conversation,
// keyed by "space/agent". Admin tools cannot be granted.
func (w *Workspace) GrantSession(key, tool string) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	g, ok := w.grants[key]
	if !ok {
		return fmt.Errorf("no open conversation %s", key)
	}
	if !g.Grant(tool) {
		return fmt.Errorf("%s cannot be approved for the session", tool)
	}
	return nil
}

// SessionGrants lists the tools approved for the session of one conversation.
func (w *Workspace) SessionGrants(key string) []string {
	w.mu.Lock()
	defer w.mu.Unlock()
	g, ok := w.grants[key]
	if !ok {
		return nil
	}
	return g.List()
}

// ClearSessionGrants drops the session approvals of one conversation.
func (w *Workspace) ClearSessionGrants(key string) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if g, ok := w.grants[key]; ok {
		g.Clear()
	}
}
