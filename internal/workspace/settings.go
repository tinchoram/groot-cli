package workspace

import (
	"context"
	"fmt"

	"github.com/tinchoram/groot-cli/internal/config"
	"github.com/tinchoram/groot-cli/internal/space"
)

// Source names the layer a setting comes from.
type Source string

const (
	SourceAgent   Source = "agent"
	SourceSpace   Source = "space"
	SourceGlobal  Source = "global"
	SourceDefault Source = "default"
)

// Setting is one value and the layer it came from.
type Setting struct {
	Value string
	From  Source
}

// SizeRoute is the route of one task size, each field with its origin.
type SizeRoute struct {
	Size  string
	Model Setting
	Agent Setting
}

// FlowView is the ODD flow a space runs with, each value with its origin.
// Model is the default model of the space's agents that set none.
type FlowView struct {
	Space  string
	Mode   Setting
	Routes []SizeRoute // small, medium, large
	// Delegation is what a route naming another agent does. Manager tracks the
	// handovers of the space; its value is empty when none is assigned.
	Delegation Setting
	Manager    Setting
	// ManagerNote says what the manager lacks, when something is missing.
	ManagerNote string
	Model       Setting
}

// ResolveFlow merges the flow overrides of a space over the global flow and the
// built-in defaults, one field at a time: space, then global, then default. A
// global value equal to its built-in default is reported as default, because
// it has the same effect.
func ResolveFlow(sp space.Space, global config.FlowConfig) FlowView {
	builtin := config.Default().Flow
	v := FlowView{
		Space:      sp.Name,
		Mode:       pick(sp.Flow.Mode, global.Mode, builtin.Mode),
		Delegation: pick(sp.Flow.Delegation, global.Delegation, builtin.Delegation),
		Manager:    resolveManager(sp, pick(sp.Flow.Manager, global.Manager, builtin.Manager)),
	}
	ownRoutes := []space.SpaceRoute{sp.Flow.Routes.Small, sp.Flow.Routes.Medium, sp.Flow.Routes.Large}
	globalRoutes := []config.FlowRoute{global.Routes.Small, global.Routes.Medium, global.Routes.Large}
	builtinRoutes := []config.FlowRoute{builtin.Routes.Small, builtin.Routes.Medium, builtin.Routes.Large}
	for i, size := range space.Sizes {
		v.Routes = append(v.Routes, SizeRoute{
			Size:  size,
			Model: pick(ownRoutes[i].Model, globalRoutes[i].Model, builtinRoutes[i].Model),
			Agent: pick(ownRoutes[i].Agent, globalRoutes[i].Agent, builtinRoutes[i].Agent),
		})
	}
	return v
}

// pick applies the precedence to one value: own (the space), then the global
// value, then the built-in default.
func pick(own, global, def string) Setting {
	switch {
	case own != "":
		return Setting{own, SourceSpace}
	case global != "" && global != def:
		return Setting{global, SourceGlobal}
	}
	return Setting{def, SourceDefault}
}

// FlowConfig is the merged flow as the flow controller reads it.
func (v FlowView) FlowConfig() config.FlowConfig {
	var c config.FlowConfig
	c.Mode = v.Mode.Value
	c.Delegation = v.Delegation.Value
	c.Manager = v.Manager.Value
	for _, r := range v.Routes {
		route := config.FlowRoute{Model: r.Model.Value, Agent: r.Agent.Value}
		switch r.Size {
		case "small":
			c.Routes.Small = route
		case "medium":
			c.Routes.Medium = route
		case "large":
			c.Routes.Large = route
		}
	}
	return c
}

// ResolveModel picks the model of one conversation: the agent's own model, then
// the space's model, then the global provider.model. An empty value means the
// provider's default.
func ResolveModel(agentModel string, sp space.Space, globalModel string) Setting {
	switch {
	case agentModel != "":
		return Setting{agentModel, SourceAgent}
	case sp.Model != "":
		return Setting{sp.Model, SourceSpace}
	case globalModel != "":
		return Setting{globalModel, SourceGlobal}
	}
	return Setting{"", SourceDefault}
}

// SpaceFlow reports the flow and default model a space runs with, merged over
// the global settings.
func (w *Workspace) SpaceFlow(name string) (FlowView, error) {
	sp, ok := w.opts.Spaces.Get(name)
	if !ok {
		return FlowView{}, fmt.Errorf("no space named %q", name)
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	v := ResolveFlow(sp, w.opts.Config.Flow)
	v.Model = ResolveModel("", sp, w.opts.Config.Provider.Model)
	v.ManagerNote = w.managerNoteLocked(v.Manager.Value)
	return v, nil
}

// SetSpaceRoute sets the model and agent of a task size in a space. A non-empty
// model is checked like set_agent_model's; an empty model or agent clears that
// field. The returned warning says when the model could not be checked.
// Conversations opened from now on use the new route.
func (w *Workspace) SetSpaceRoute(ctx context.Context, spaceName, size, model, agent string) (string, error) {
	if !space.ValidSize(size) {
		return "", fmt.Errorf("size %q is invalid (use small, medium or large)", size)
	}
	var warn string
	if model != "" {
		var err error
		if warn, err = w.CheckModel(ctx, model, false); err != nil {
			return "", err
		}
	}
	if err := w.opts.Spaces.SetRoute(spaceName, size, model, agent); err != nil {
		return "", err
	}
	return warn, nil
}

// SetSpaceModel sets the default model of a space's agents. An empty model
// clears it. A non-empty model is checked like set_agent_model's. Conversations
// opened from now on use it; agents with their own model keep theirs.
func (w *Workspace) SetSpaceModel(ctx context.Context, spaceName, model string) (string, error) {
	var warn string
	if model != "" {
		var err error
		if warn, err = w.CheckModel(ctx, model, false); err != nil {
			return "", err
		}
	}
	if err := w.opts.Spaces.SetModel(spaceName, model); err != nil {
		return "", err
	}
	return warn, nil
}

// SetSpaceRoute and SetSpaceModel let the administrator agent reach the space
// overrides through the admin manager.
func (a adminManager) SetSpaceRoute(ctx context.Context, spaceName, size, model, agent string) (string, error) {
	return a.w.SetSpaceRoute(ctx, spaceName, size, model, agent)
}

func (a adminManager) SetSpaceModel(ctx context.Context, spaceName, model string) (string, error) {
	return a.w.SetSpaceModel(ctx, spaceName, model)
}

// inheritedModelLocked is the model of an agent in spaceName that sets none.
// Callers hold w.mu.
func (w *Workspace) inheritedModelLocked(spaceName string) string {
	sp, _ := w.opts.Spaces.Get(spaceName)
	if m := ResolveModel("", sp, w.opts.Config.Provider.Model).Value; m != "" {
		return m
	}
	return w.defaultModelLocked()
}
