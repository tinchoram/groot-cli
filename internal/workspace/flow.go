package workspace

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/tinchoram/groot-cli/internal/config"
	"github.com/tinchoram/groot-cli/internal/flow"
	"github.com/tinchoram/groot-cli/internal/provider"
	"github.com/tinchoram/groot-cli/internal/space"
	"github.com/tinchoram/groot-cli/internal/tool"
)

// newFlow builds the ODD controller of one conversation from the flow of its
// space, merged over the global settings (see ResolveFlow). Its tasks live in
// the space database. Opening a task switches the conversation's model to the
// route's model, and the switch is undone when the task closes. A running
// conversation keeps the controller it was built with.
func (w *Workspace) newFlow(ctx context.Context, sp space.Space, agentName string, tasks flow.Store, prov provider.Provider, onOpen, onDeliver func(flow.Task)) (*flow.Controller, error) {
	flowCfg := ResolveFlow(sp, w.opts.Config.Flow).FlowConfig()
	mode, err := flow.ParseMode(flowCfg.Mode)
	if err != nil {
		return nil, err
	}
	return flow.NewController(ctx, flow.Options{
		Space:     sp.Name,
		Agent:     agentName,
		Store:     tasks,
		Mode:      mode,
		Routes:    routesFrom(flowCfg),
		OnOpen:    onOpen,
		OnDeliver: onDeliver,
		PlanExists: func(path string) bool {
			return planFileExists(sp.Path, path)
		},
		ReadPlan: func(path string) (string, error) {
			abs, err := planPath(sp.Path, path)
			if err != nil {
				return "", err
			}
			raw, err := os.ReadFile(abs)
			return string(raw), err
		},
		ApplyRoute: func(r flow.Route) func() {
			previous := prov.Model()
			w.warnUnlistedRoute(sp.Name+"/"+agentName, r.Model)
			prov.SetModel(r.Model)
			return func() { prov.SetModel(previous) }
		},
		Warn: keyedWarn(w.opts.Hooks, sp.Name+"/"+agentName),
		// Admin tools change the catalog and the spaces, not the code, so the
		// flow does not gate them.
		Exempt: []string{"create_space", "assign_agent", "unassign_agent", "create_agent", "import_agent", "install_agent", "delete_space", "edit_space", "delete_agent", "edit_agent", "move_agent", "copy_agent"},
		// Delegation: auto hands a route's agent the task, when it is assigned.
		Delegation:  flow.DelegationMode(flowCfg.Delegation),
		Manager:     flowCfg.Manager,
		CanDelegate: func(agent string) bool { return slices.Contains(sp.Agents, agent) },
	})
}

func routesFrom(c config.FlowConfig) flow.Routes {
	route := func(r config.FlowRoute) flow.Route { return flow.Route{Model: r.Model, Agent: r.Agent} }
	return flow.Routes{
		Small:  route(c.Routes.Small),
		Medium: route(c.Routes.Medium),
		Large:  route(c.Routes.Large),
	}
}

// planFileExists reports whether rel is a regular file inside root.
func planFileExists(root, rel string) bool {
	_, err := planPath(root, rel)
	return err == nil
}

// planPath returns the absolute path of rel, a regular file inside root. A path
// that leaves the space, or names a directory or a missing file, is an error.
func planPath(root, rel string) (string, error) {
	clean := filepath.Clean(rel)
	if filepath.IsAbs(clean) || clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("plan path %q leaves the space", rel)
	}
	abs := filepath.Join(root, clean)
	info, err := os.Stat(abs)
	if err != nil {
		return "", fmt.Errorf("plan file %q not found in the space", rel)
	}
	if !info.Mode().IsRegular() {
		return "", fmt.Errorf("plan path %q is not a file", rel)
	}
	return abs, nil
}

// recordingTool wraps bash so that every run is kept as command evidence of
// the open task. The flow decides whether the run counts; the model does not
// report it.
type recordingTool struct {
	tool.Tool
	ctl *flow.Controller
}

func (r recordingTool) Execute(ctx context.Context, input string) (string, bool) {
	out, isErr := r.Tool.Execute(ctx, input)
	if err := r.ctl.RecordBash(ctx, input, out, isErr); err != nil {
		r.ctl.Warn(fmt.Sprintf("command not recorded as evidence: %v", err))
	}
	return out, isErr
}

// SandboxName forwards the sandbox of the wrapped tool, so the audit log still sees it.
func (r recordingTool) SandboxName() string {
	if s, ok := r.Tool.(interface{ SandboxName() string }); ok {
		return s.SandboxName()
	}
	return ""
}

// CredentialSets forwards the credential sets of the wrapped tool, so the gate
// and the audit still see them.
func (r recordingTool) CredentialSets() []string {
	if c, ok := r.Tool.(tool.Credentialed); ok {
		return c.CredentialSets()
	}
	return nil
}

// Task reports the open task of this conversation, if there is one.
func (c *Conversation) Task() (flow.Task, bool) {
	if c.ctl == nil {
		return flow.Task{}, false
	}
	return c.ctl.Active()
}

// Observed reports the phase the tool activity of the open task shows, if any.
// It is for the human; the model never sees it.
func (c *Conversation) Observed() (flow.Phase, bool) {
	if c.ctl == nil {
		return "", false
	}
	return c.ctl.Observed()
}

// observedTool passes the read-only fact of each tool call to the flow.
func observedTool(ctl *flow.Controller) func(string, bool) {
	return func(name string, readOnly bool) {
		if ctl != nil {
			ctl.Observe(name, readOnly)
		}
	}
}

// TaskStatus describes the classification and the open task.
func (c *Conversation) TaskStatus(ctx context.Context) string {
	if c.ctl == nil {
		return "no task flow in this conversation"
	}
	return c.ctl.Status(ctx)
}

// FailTask closes the open task with a reason, as the user asked.
func (c *Conversation) FailTask(ctx context.Context, reason string) (string, error) {
	if c.ctl == nil {
		return "", fmt.Errorf("no task flow in this conversation")
	}
	t, err := c.ctl.Fail(ctx, reason)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("task #%d failed: %s", t.ID, strings.TrimSpace(reason)), nil
}
