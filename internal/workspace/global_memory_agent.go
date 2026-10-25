package workspace

import (
	"context"
	"fmt"

	"github.com/tinchoram/groot-cli/internal/catalog"
	"github.com/tinchoram/groot-cli/internal/tool"
)

// SetAgentGlobalMemory lets an agent read global memory, or takes it away. The
// change is saved in the agent file and reaches its open conversations at once.
// groot always reads global memory, so it cannot be turned off for groot.
func (w *Workspace) SetAgentGlobalMemory(name string, on bool) error {
	d, err := w.agentDef(name)
	if err != nil {
		return err
	}
	if name == managedAgent {
		if !on {
			return fmt.Errorf("agent %q always reads global memory", managedAgent)
		}
		return nil
	}
	d.GlobalMemory = on
	if err := catalog.Save(w.opts.Home, d, d.Prompt); err != nil {
		return err
	}
	return w.refreshAgentMemory(name, d, on)
}

// refreshAgentMemory reloads the catalog and applies the global memory switch to
// every open conversation of the agent. Every registry is built before any
// conversation changes, so a failure leaves them as they were.
func (w *Workspace) refreshAgentMemory(name string, d catalog.AgentDef, on bool) error {
	if err := w.reload(); err != nil {
		return err
	}
	w.mu.Lock()
	defer w.mu.Unlock()

	type swap struct {
		conv  *Conversation
		reg   *tool.Registry
		prev  bool
		names []string
	}
	var swaps []swap
	revert := func() {
		for _, s := range swaps {
			s.conv.mem.setGlobal(s.prev)
		}
	}
	for _, c := range w.convs {
		if c.name != name || c.mem == nil || c.rebuildTools == nil {
			continue
		}
		prev := c.mem.reader().Global
		c.mem.setGlobal(on)
		reg, err := c.rebuildTools(d.Tools)
		if err != nil {
			c.mem.setGlobal(prev)
			revert()
			return fmt.Errorf("update open conversation %s/%s: %w", c.space, c.name, err)
		}
		swaps = append(swaps, swap{c, reg, prev, d.Tools})
	}
	for _, s := range swaps {
		s.conv.swapTools(s.reg, s.names)
		_ = s.conv.refreshSystem(context.Background())
	}
	return nil
}

// SetAgentGlobalMemory reaches the same method through the admin manager.
func (a adminManager) SetAgentGlobalMemory(name string, on bool) error {
	return a.w.SetAgentGlobalMemory(name, on)
}
