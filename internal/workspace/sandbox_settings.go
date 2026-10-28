package workspace

import (
	"fmt"
	"slices"

	"github.com/tinchoram/groot-cli/internal/catalog"
	"github.com/tinchoram/groot-cli/internal/sandbox"
	"github.com/tinchoram/groot-cli/internal/space"
	"github.com/tinchoram/groot-cli/internal/tool"
)

// ResolveNetwork is the network of a space with its origin: the space's own
// override when it has one, the global bash.sandbox.network otherwise. Value is
// "on" or "off".
func ResolveNetwork(sp space.Space, global bool) Setting {
	if sp.Network != nil {
		return Setting{onOff(*sp.Network), SourceSpace}
	}
	return Setting{onOff(global), SourceGlobal}
}

func onOff(on bool) string {
	if on {
		return "on"
	}
	return "off"
}

// networkOf reports whether commands of a space may use the network.
func networkOf(sp space.Space, global bool) bool {
	return ResolveNetwork(sp, global).Value == "on"
}

// SpaceNetwork reports the network setting of a space and where it comes from.
func (w *Workspace) SpaceNetwork(name string) (Setting, error) {
	sp, ok := w.opts.Spaces.Get(name)
	if !ok {
		return Setting{}, fmt.Errorf("no space named %q", name)
	}
	return ResolveNetwork(sp, w.opts.Config.Bash.Sandbox.Network), nil
}

// SetSpaceNetwork turns the network of a space on or off, saved in spaces.yaml.
// The open conversations of the space get the new setting at once and keep
// their sessions.
func (w *Workspace) SetSpaceNetwork(name string, on bool) error {
	if err := w.opts.Spaces.SetNetwork(name, &on); err != nil {
		return err
	}
	return w.refreshSpaceTools(name)
}

// refreshSpaceTools rebuilds the tools of the open conversations of a space, so
// that their bash tools take the settings of the space as they are now. All
// registries are built before any is swapped.
func (w *Workspace) refreshSpaceTools(spaceName string) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	type swap struct {
		conv *Conversation
		reg  *tool.Registry
	}
	var swaps []swap
	for _, c := range w.convs {
		if c.space != spaceName || c.rebuildTools == nil {
			continue
		}
		reg, err := c.rebuildTools(c.toolNames)
		if err != nil {
			return fmt.Errorf("update open conversation %s/%s: %w", c.space, c.name, err)
		}
		swaps = append(swaps, swap{c, reg})
	}
	for _, s := range swaps {
		s.conv.swapTools(s.reg, s.conv.toolNames)
	}
	return nil
}

// networkAt reports whether the commands run in the directory root may use the
// network: the setting of the space whose directory it is, or the global one.
func (w *Workspace) networkAt(root string) bool {
	global := w.opts.Config.Bash.Sandbox.Network
	if w.opts.Spaces == nil {
		return global
	}
	for _, sp := range w.opts.Spaces.List() {
		if sp.Path == root {
			return networkOf(sp, global)
		}
	}
	return global
}

// credentialsOf returns the credential sets the agent is granted, or nil.
func (w *Workspace) credentialsOf(agentName string) []string {
	if d, ok := w.def(agentName); ok && len(d.Credentials) > 0 {
		return slices.Clone(d.Credentials)
	}
	return nil
}

// EditAgentCredentials grants and revokes credential sets of an agent and saves
// its file. A set must be one of sandbox.CredentialSets; .groot is never one.
// The open conversations of the agent get the change at once and keep their
// sessions.
func (w *Workspace) EditAgentCredentials(name string, add, remove []string) error {
	d, err := w.agentDef(name)
	if err != nil {
		return err
	}
	sets := slices.Clone(d.Credentials)
	for _, s := range add {
		if _, err := sandbox.CredentialDirs([]string{s}); err != nil {
			return err
		}
		if !slices.Contains(sets, s) {
			sets = append(sets, s)
		}
	}
	for _, s := range remove {
		sets = slices.DeleteFunc(sets, func(x string) bool { return x == s })
	}
	d.Credentials = sets
	if err := catalog.Save(w.opts.Home, d, d.Prompt); err != nil {
		return err
	}
	return w.refreshAgentTools(name, d.Tools)
}

// EditAgentCredentials and SetSpaceNetwork reach the same methods through the admin manager.
func (a adminManager) EditAgentCredentials(name string, add, remove []string) error {
	return a.w.EditAgentCredentials(name, add, remove)
}

func (a adminManager) SetSpaceNetwork(name string, on bool) error {
	return a.w.SetSpaceNetwork(name, on)
}
