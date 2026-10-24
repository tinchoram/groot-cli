package workspace

import (
	"fmt"
	"slices"
	"strings"

	"github.com/tinchoram/groot-cli/internal/catalog"
)

// managedAgents are the agents that bootstrap owns: they are kept current on
// every start and cannot be created, imported over, or upgraded by hand.
var managedAgents = []string{managedAgent, debugAgent}

func isManaged(name string) bool { return slices.Contains(managedAgents, name) }

// UpgradeAgents adds the managed tools that agents lack: the one named agent,
// or every agent when name is "". It never removes a tool. It returns one line
// per agent. Managed agents are not upgraded here: bootstrap keeps them current.
func (w *Workspace) UpgradeAgents(name string) ([]string, error) {
	if isManaged(name) {
		return nil, fmt.Errorf("%s keeps its tools current at every start; it is not upgraded by hand", name)
	}
	defs, err := catalog.Load(w.opts.Home)
	if err != nil {
		return nil, err
	}
	var report []string
	found := false
	for _, d := range defs {
		if isManaged(d.Name) || (name != "" && d.Name != name) {
			continue
		}
		found = true
		up, added := catalog.Upgrade(d)
		if len(added) == 0 {
			report = append(report, d.Name+": up to date")
			continue
		}
		if err := catalog.Save(w.opts.Home, up, up.Prompt); err != nil {
			return report, err
		}
		report = append(report, fmt.Sprintf("%s: added %s", d.Name, strings.Join(added, ", ")))
	}
	if name != "" && !found {
		return nil, fmt.Errorf("no agent named %q in the catalog", name)
	}
	if err := w.reload(); err != nil {
		return report, err
	}
	return report, nil
}

// keepManagedCurrent makes sure every managed agent's file carries the managed
// tools. It keeps the file's other tools, their order and the prompt, and writes
// only when a tool was missing, so running it again changes nothing.
func keepManagedCurrent(home string) error {
	defs, err := catalog.Load(home)
	if err != nil {
		return err
	}
	for _, d := range defs {
		if !isManaged(d.Name) {
			continue
		}
		up, added := catalog.Upgrade(d)
		// Managed agents also carry their own tool set (admin or backlog
		// tools), which grows between versions; the base set is not enough.
		for _, t := range managedRequiredTools(d.Name) {
			if !slices.Contains(up.Tools, t) {
				up.Tools = append(up.Tools, t)
				added = append(added, t)
			}
		}
		if len(added) == 0 {
			continue
		}
		if err := catalog.Save(home, up, up.Prompt); err != nil {
			return fmt.Errorf("upgrade %s: %w", d.Name, err)
		}
	}
	return nil
}

// managedRequiredTools is the full tool set a managed agent must carry.
func managedRequiredTools(name string) []string {
	switch name {
	case managedAgent:
		return grootDef().Tools
	case debugAgent:
		return debugDef().Tools
	}
	return nil
}
