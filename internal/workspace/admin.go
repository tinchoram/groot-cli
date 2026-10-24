package workspace

import (
	"fmt"
	"slices"

	"github.com/tinchoram/groot-cli/internal/admintool"
	"github.com/tinchoram/groot-cli/internal/agentimport"
	"github.com/tinchoram/groot-cli/internal/catalog"
)

// adminManager implements admintool.Manager on top of the workspace. It is
// the only place where the admin tools reach the spaces and the catalog.
type adminManager struct {
	w *Workspace
}

// Manager returns the admin operations for this workspace.
func (w *Workspace) Manager() admintool.Manager { return adminManager{w} }

func (a adminManager) ListSpaces() []admintool.SpaceInfo {
	var out []admintool.SpaceInfo
	for _, sp := range a.w.opts.Spaces.List() {
		out = append(out, admintool.SpaceInfo{Name: sp.Name, Path: sp.Path, Agents: sp.Agents})
	}
	return out
}

func (a adminManager) CreateSpace(name, path string) error {
	return a.w.opts.Spaces.Add(name, path, a.w.opts.Now())
}

func (a adminManager) SetWorkMode(space, agent string, on bool) error {
	return a.w.SetWorkMode(space, agent, on)
}

func (a adminManager) UpgradeAgent(name string) ([]string, error) {
	return a.w.UpgradeAgents(name)
}

func (a adminManager) AssignAgent(space, agent string) error {
	a.w.mu.Lock()
	_, ok := a.w.def(agent)
	a.w.mu.Unlock()
	if !ok {
		return fmt.Errorf("no agent named %q in the catalog", agent)
	}
	if err := a.w.opts.Spaces.Assign(space, agent); err != nil {
		return err
	}
	a.w.mu.Lock()
	defer a.w.mu.Unlock()
	if cur, ok := a.w.opts.Spaces.Current(); ok && cur.Name == space && a.w.active == "" {
		a.w.active = agent
	}
	return nil
}

func (a adminManager) UnassignAgent(space, agent string) error {
	if err := a.w.opts.Spaces.Unassign(space, agent); err != nil {
		return err
	}
	a.w.mu.Lock()
	defer a.w.mu.Unlock()
	if cur, ok := a.w.opts.Spaces.Current(); ok && cur.Name == space && a.w.active == agent {
		a.w.active = a.w.firstAssigned()
	}
	return nil
}

func (a adminManager) ListAgents() []admintool.AgentInfo {
	a.w.mu.Lock()
	defer a.w.mu.Unlock()
	var out []admintool.AgentInfo
	for _, d := range a.w.defs {
		out = append(out, admintool.AgentInfo{
			Name:        d.Name,
			Description: d.Description,
			Model:       d.Model,
			Tools:       slices.Clone(d.Tools),
		})
	}
	return out
}

func (a adminManager) ImportAgent(plan agentimport.Planned, overwrite bool) (admintool.ImportResult, error) {
	res, err := a.w.importPlan(plan, overwrite)
	if err != nil {
		return admintool.ImportResult{}, err
	}
	return admintool.ImportResult{Name: res.Name, Warnings: res.Warnings}, nil
}

func (a adminManager) CreateAgent(spec admintool.AgentSpec) error {
	if spec.Name == managedAgent {
		return fmt.Errorf("agent %q is managed and cannot be created again", managedAgent)
	}
	if catalog.Exists(a.w.opts.Home, spec.Name) {
		return fmt.Errorf("agent %q already exists", spec.Name)
	}
	def := catalog.AgentDef{
		Name:        spec.Name,
		Description: spec.Description,
		Model:       spec.Model,
		Tools:       spec.Tools,
	}
	if err := catalog.Save(a.w.opts.Home, def, spec.Prompt); err != nil {
		return err
	}
	return a.w.reload()
}

func (a adminManager) ListLibrary() ([]admintool.LibraryInfo, error) {
	return a.w.LibraryAgents()
}

func (a adminManager) InstallAgent(name, space string, overwrite bool) error {
	return a.w.InstallLibraryAgent(name, space, overwrite)
}
