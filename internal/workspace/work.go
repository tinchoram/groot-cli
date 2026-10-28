package workspace

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/tinchoram/groot-cli/internal/gate"
	"github.com/tinchoram/groot-cli/internal/provider"
	"github.com/tinchoram/groot-cli/internal/space"
)

// workStateFor returns the work mode of a conversation, created with the limits
// of the configuration on first use.
func (w *Workspace) workStateFor(key, root string, prov provider.Provider) (*gate.WorkState, error) {
	if s, ok := w.work[key]; ok {
		return s, nil
	}
	cfg := w.opts.Config.Work
	s, err := gate.NewWorkState(gate.WorkConfig{
		Space:    root,
		Ask:      cfg.AlwaysAsk,
		MaxTurns: cfg.MaxTurns,
		MaxUSD:   cfg.MaxUSD,
		Cost: func() float64 {
			if r, ok := prov.(provider.UsageReporter); ok {
				return r.EstimatedCostUSD()
			}
			return 0
		},
		Warn: func(msg string) { w.warnKey(key, msg) },
	})
	if err != nil {
		return nil, err
	}
	w.work[key] = s
	return s, nil
}

func (w *Workspace) warnKey(key, msg string) {
	if h := w.opts.Hooks.OnWarning; h != nil {
		h(key, msg)
	}
}

// announce tells the transcript of a conversation that its work mode changed.
func (w *Workspace) announce(key, root string, on bool) {
	if !on {
		w.warnKey(key, WorkOffLine)
		return
	}
	turns, usd := w.WorkLimits()
	w.warnKey(key, WorkOnLine(root, turns, usd))
}

// WorkLimits reports the turn and spend limits of work mode.
func (w *Workspace) WorkLimits() (int, float64) {
	return w.opts.Config.Work.MaxTurns, w.opts.Config.Work.MaxUSD
}

// WorkOnLine is the transcript line that announces work mode, with its limits.
func WorkOnLine(root string, turns int, usd float64) string {
	return fmt.Sprintf("work mode on · writes inside %s auto-approved · limits %d turns / $%.2f", root, turns, usd)
}

// WorkOffLine is the transcript line that announces work mode is off.
const WorkOffLine = "work mode off"

// WorkOffDeliveredLine is the warning that work mode turned itself off because
// the task was delivered for review. It names the reason and how to turn it back on.
const WorkOffDeliveredLine = "work mode off · task delivered for review · /work on turns it back on"

// WorkModes lists the conversations in work mode, by "space/agent".
func (w *Workspace) WorkModes() map[string]bool {
	w.mu.Lock()
	defer w.mu.Unlock()
	out := map[string]bool{}
	for k, s := range w.work {
		if s.On() {
			out[k] = true
		}
	}
	return out
}

// SetWorkMode turns work mode on or off for one agent in a space. The
// conversation is opened if needed, so the limits apply from the first call.
func (w *Workspace) SetWorkMode(spaceName, agent string, on bool) error {
	sp, ok := w.opts.Spaces.Get(spaceName)
	if !ok {
		return fmt.Errorf("no space named %q", spaceName)
	}
	if !slices.Contains(sp.Agents, agent) {
		return fmt.Errorf("agent %q is not assigned to space %q", agent, spaceName)
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	key := spaceName + "/" + agent
	if err := w.openLocked(sp, agent); err != nil {
		return err
	}
	w.work[key].Set(on)
	w.announce(key, sp.Path, on)
	return nil
}

// SetSpaceWorkMode turns work mode on or off for every agent assigned to a
// space, and sets what the conversations opened later in it inherit. On opens
// the agents that are not open yet. Off changes only the conversations that
// exist. Each conversation keeps its own state from then on. The shown
// conversation reports the change itself, so nothing is announced here.
func (w *Workspace) SetSpaceWorkMode(spaceName string, on bool) error {
	sp, ok := w.opts.Spaces.Get(spaceName)
	if !ok {
		return fmt.Errorf("no space named %q", spaceName)
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	if on {
		w.spaceWork[spaceName] = true
	} else {
		delete(w.spaceWork, spaceName)
	}
	for _, agent := range sp.Agents {
		if on {
			if err := w.openLocked(sp, agent); err != nil {
				return err
			}
		}
		if s, ok := w.work[spaceName+"/"+agent]; ok {
			s.Set(on)
		}
	}
	return nil
}

// openLocked opens the conversation of an agent in a space when it is not open
// yet. The caller holds mu.
func (w *Workspace) openLocked(sp space.Space, agent string) error {
	key := sp.Name + "/" + agent
	if _, ok := w.convs[key]; ok {
		return nil
	}
	c, err := w.open(sp, agent)
	if err != nil {
		return err
	}
	w.convs[key] = c
	return nil
}

// renameSpaceWork moves the space-wide work mode of a renamed space to its new
// name. An empty to drops it, for a deleted space.
func (w *Workspace) renameSpaceWork(from, to string) {
	w.mu.Lock()
	defer w.mu.Unlock()
	on := w.spaceWork[from]
	delete(w.spaceWork, from)
	if on && to != "" {
		w.spaceWork[to] = true
	}
}

// WorkMode reports whether work mode is on for this conversation.
func (c *Conversation) WorkMode() bool {
	return c.work != nil && c.work.On()
}

// SetWorkMode turns work mode on or off for this conversation.
func (c *Conversation) SetWorkMode(on bool) {
	if c.work != nil {
		c.work.Set(on)
	}
}

// TakeReview returns the review of the last delivery in work mode, once.
func (c *Conversation) TakeReview() string {
	if c.review == nil {
		return ""
	}
	return c.review.take()
}

// TaskAccept closes the delivered task.
func (c *Conversation) TaskAccept(ctx context.Context) (string, error) {
	if c.ctl == nil {
		return "", errors.New("no task flow in this conversation")
	}
	t, err := c.ctl.Advance(ctx, "done", "")
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("task #%d accepted and closed", t.ID), nil
}

// TaskReject sends the delivered task back to implement, with the reason as
// evidence.
func (c *Conversation) TaskReject(ctx context.Context, reason string) (string, error) {
	if c.ctl == nil {
		return "", errors.New("no task flow in this conversation")
	}
	reason = strings.TrimSpace(reason)
	t, err := c.ctl.Advance(ctx, "implement", reason)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("[review] task #%d sent back to implement: %s", t.ID, reason), nil
}
