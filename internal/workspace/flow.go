package workspace

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
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
		ApplyRoute: func(r flow.Route) func() {
			previous := prov.Model()
			w.warnUnlistedRoute(sp.Name+"/"+agentName, r.Model)
			prov.SetModel(r.Model)
			return func() { prov.SetModel(previous) }
		},
		Warn: keyedWarn(w.opts.Hooks, sp.Name+"/"+agentName),
		// Admin tools change the catalog and the spaces, not the code, so the
		// flow does not gate them.
		Exempt: []string{"create_space", "assign_agent", "unassign_agent", "create_agent", "import_agent", "install_agent"},
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

// planFileExists reports whether rel is a file inside root. A path that
// leaves the space is never a plan.
func planFileExists(root, rel string) bool {
	clean := filepath.Clean(rel)
	if filepath.IsAbs(clean) || clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
		return false
	}
	info, err := os.Stat(filepath.Join(root, clean))
	return err == nil && !info.IsDir()
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

// withBlock puts the flow block in front of the system prompt of a turn.
func withBlock(block, base string) string {
	if block == "" {
		return base
	}
	return block + "\n\n" + base
}

// Task reports the open task of this conversation, if there is one.
func (c *Conversation) Task() (flow.Task, bool) {
	if c.ctl == nil {
		return flow.Task{}, false
	}
	return c.ctl.Active()
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
