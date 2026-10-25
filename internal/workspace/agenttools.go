package workspace

import (
	"fmt"
	"slices"

	"github.com/tinchoram/groot-cli/internal/catalog"
	"github.com/tinchoram/groot-cli/internal/tool"
)

// requiredTools are the tools a managed agent keeps: the base set and the tools
// its bootstrap definition gives it. Removing one is refused.
func requiredTools(name string) []string {
	switch name {
	case managedAgent:
		return append(catalog.ManagedTools(), grootDef().Tools...)
	case debugAgent:
		return append(catalog.ManagedTools(), debugDef().Tools...)
	}
	return nil
}

// AgentTools lists the tools of an agent.
func (w *Workspace) AgentTools(name string) ([]string, error) {
	d, err := w.agentDef(name)
	if err != nil {
		return nil, err
	}
	return slices.Clone(d.Tools), nil
}

// EditAgentTools adds and removes tools of an agent and saves its file. The
// catalog is reloaded and every open conversation of the agent gets the new
// tools at once, keeping its session.
func (w *Workspace) EditAgentTools(name string, add, remove []string) error {
	d, err := w.agentDef(name)
	if err != nil {
		return err
	}
	up, err := catalog.EditTools(d, add, remove, requiredTools(name))
	if err != nil {
		return err
	}
	if err := catalog.Save(w.opts.Home, up, up.Prompt); err != nil {
		return err
	}
	return w.refreshAgentTools(name, up.Tools)
}

// refreshAgentTools reloads the catalog and swaps the tool registry of every
// open conversation of the agent. All registries are built before any is
// swapped, so a failure leaves the open conversations as they were.
func (w *Workspace) refreshAgentTools(name string, tools []string) error {
	if err := w.reload(); err != nil {
		return err
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	type swap struct {
		conv *Conversation
		reg  *tool.Registry
	}
	var swaps []swap
	for _, c := range w.convs {
		if c.name != name || c.rebuildTools == nil {
			continue
		}
		reg, err := c.rebuildTools(tools)
		if err != nil {
			return fmt.Errorf("update open conversation %s/%s: %w", c.space, c.name, err)
		}
		swaps = append(swaps, swap{c, reg})
	}
	for _, s := range swaps {
		s.conv.SetTools(s.reg)
	}
	return nil
}

func (w *Workspace) agentDef(name string) (catalog.AgentDef, error) {
	defs, err := catalog.Load(w.opts.Home)
	if err != nil {
		return catalog.AgentDef{}, err
	}
	for _, d := range defs {
		if d.Name == name {
			return d, nil
		}
	}
	return catalog.AgentDef{}, fmt.Errorf("unknown agent %q", name)
}

// AgentTools and EditAgentTools reach the same methods through the admin manager.
func (a adminManager) AgentTools(name string) ([]string, error) {
	return a.w.AgentTools(name)
}

func (a adminManager) EditAgentTools(name string, add, remove []string) error {
	return a.w.EditAgentTools(name, add, remove)
}
