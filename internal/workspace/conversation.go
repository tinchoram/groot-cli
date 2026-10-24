package workspace

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/tinchoram/groot-cli/internal/admintool"
	"github.com/tinchoram/groot-cli/internal/agent"
	"github.com/tinchoram/groot-cli/internal/api"
	"github.com/tinchoram/groot-cli/internal/flow"
	"github.com/tinchoram/groot-cli/internal/gate"
	"github.com/tinchoram/groot-cli/internal/inbox"
	"github.com/tinchoram/groot-cli/internal/memory"
	"github.com/tinchoram/groot-cli/internal/provider"
	"github.com/tinchoram/groot-cli/internal/tool"
	"sync/atomic"
)

const (
	summaryTimeout  = 30 * time.Second
	summaryMaxRunes = 100_000
	summarySystem   = "You write terse session summaries for an SRE audit trail."
	summaryPrompt   = "Summarize this session in exactly three lines: what was asked, what was found or changed, and what is still open. No preamble.\n\n"
)

// Conversation is one agent running inside one space. It wraps the agent loop
// with the model handle and the space's memory store.
type Conversation struct {
	*agent.Agent
	prov  provider.Provider
	store memory.Store
	name  string
	space string
	inbox *inbox.Store
	// resumed is set when the conversation continued a stored session.
	resumed *Resumed
	// ctl enforces the ODD flow; baseSystem is the prompt before the flow block.
	ctl        *flow.Controller
	work       *gate.WorkState
	files      *fileLog
	review     *reviewBox
	busy       atomic.Bool  // a turn is running: UI-driven or triggered by a message
	autoTurns  atomic.Int64 // turns triggered by messages in this session
	baseSystem string
	// startModel is the model the conversation started with; prevModel is the
	// one before the last change. A model the provider does not know reverts to
	// them. warn reports the revert.
	startModel string
	prevModel  string
	revertWarn func(string)
}

// Model returns the model used for the next call.
func (c *Conversation) Model() string { return c.prov.Model() }

// Send runs one turn. When the agent has unread messages, the prompt is
// prefixed with a notice so the model reads them with read_inbox when they
// matter. Nothing is marked read here.
func (c *Conversation) Send(ctx context.Context, prompt string) (string, error) {
	if !c.busy.CompareAndSwap(false, true) {
		return "", errAgentBusy
	}
	defer c.busy.Store(false)
	return c.send(ctx, prompt)
}

// send runs one turn. The caller holds the busy flag.
func (c *Conversation) send(ctx context.Context, prompt string) (string, error) {
	if c.ctl != nil {
		c.ctl.StartTurn()
		c.Agent.System = withBlock(c.ctl.ContextBlock(ctx), c.baseSystem)
	}
	prompt = c.injectInbox(ctx, prompt)
	if prompt == "" {
		return "", nil
	}
	resp, err := c.Agent.Send(ctx, prompt)
	if err != nil && provider.IsModelNotFound(err) && c.revertModel() {
		// The failed attempt left the prompt in the history; the retry answers it.
		return c.Agent.Send(ctx, prompt)
	}
	return resp, err
}

// SetModel changes the model for later calls.
func (c *Conversation) SetModel(name string) error {
	if strings.TrimSpace(name) == "" {
		return errors.New("model name is empty")
	}
	if current := c.prov.Model(); current != name {
		c.prevModel = current
	}
	c.prov.SetModel(name)
	return nil
}

// revertModel switches back to the model before the last change, or to the
// model the conversation started with, after the provider reports the current
// one as not found. It reports whether it switched.
func (c *Conversation) revertModel() bool {
	bad := c.prov.Model()
	target := c.prevModel
	if target == "" || target == bad {
		target = c.startModel
	}
	if target == "" || target == bad {
		return false
	}
	c.prov.SetModel(target)
	c.prevModel = ""
	if c.revertWarn != nil {
		c.revertWarn(fmt.Sprintf("model %s not found · reverted to %s", bad, target))
	}
	return true
}

// End writes a three-line summary of a non-empty conversation to the space's
// memory, then ends the session.
func (c *Conversation) End(ctx context.Context) error {
	var summaryErr error
	if c.SessionID() != "" && c.MessageCount() > 0 {
		summaryErr = c.saveSummary(ctx)
	}
	if err := c.Agent.End(ctx); err != nil {
		return err
	}
	return summaryErr
}

func (c *Conversation) saveSummary(ctx context.Context) error {
	return c.summarize(ctx, c.SessionID(), c.Transcript())
}

// summarize asks the model for a three-line summary of session id and stores it
// in the space memory.
func (c *Conversation) summarize(ctx context.Context, id, transcript string) error {
	sctx, cancel := context.WithTimeout(ctx, summaryTimeout)
	defer cancel()

	transcript = tail(transcript, summaryMaxRunes)
	resp, err := c.prov.Send(sctx, provider.Request{
		System: summarySystem,
		Messages: []api.Message{{
			Role:    api.RoleUser,
			Content: []api.Block{{Type: api.BlockText, Text: summaryPrompt + transcript}},
		}},
	})
	if err != nil {
		return fmt.Errorf("session summary for %s: %w", c.name, err)
	}
	text := strings.TrimSpace(resp.Text())
	if text == "" {
		return nil
	}
	return c.store.Remember(sctx, memory.Entry{
		Kind:   memory.KindSessionSummary,
		Scope:  memory.ScopeProject,
		Agent:  c.name,
		Text:   text,
		Source: "session:" + id,
	})
}

// tail keeps the last n runes so long sessions stay within the summary budget.
func tail(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[len(r)-n:])
}

// buildTools creates the registry an agent may use. root confines file tools
// and sets the working directory for bash, so it is the space path.
func (w *Workspace) buildTools(root string, names []string, mem memory.Store, msg tool.Messenger, ctl *flow.Controller, files *fileLog, agentName string) (*tool.Registry, error) {
	reg := tool.NewRegistry()
	for _, n := range names {
		t, err := w.newTool(n, root, mem, msg, ctl, files, agentName)
		if err != nil {
			return nil, err
		}
		if err := reg.Register(t); err != nil {
			return nil, err
		}
	}
	return reg, nil
}

func (w *Workspace) newTool(name, root string, mem memory.Store, msg tool.Messenger, ctl *flow.Controller, files *fileLog, agentName string) (tool.Tool, error) {
	switch name {
	case "task_classify", "task_open", "task_advance", "task_status":
		if ctl == nil {
			return nil, errors.New("the task flow is not available")
		}
		switch name {
		case "task_classify":
			return tool.NewTaskClassify(ctl), nil
		case "task_open":
			return tool.NewTaskOpen(ctl), nil
		case "task_advance":
			return tool.NewTaskAdvance(ctl), nil
		}
		return tool.NewTaskStatus(ctl), nil
	case "feedback_report":
		return tool.NewFeedbackReport(w.feedback, w.spaceNameFor(root), agentName, w.opts.Version), nil
	case "feedback_list":
		return tool.NewFeedbackList(w.feedback), nil
	case "fetch_url":
		fc := w.opts.Config.Fetch
		return tool.NewFetch(w.opts.Version, tool.FetchPolicy{
			AllowPrivate: fc.AllowPrivate,
			Allow:        fc.Allow,
			Deny:         fc.Deny,
		}), nil
	case "feedback_update":
		return tool.NewFeedbackUpdate(w.feedback), nil
	case "send_message":
		if msg == nil {
			return nil, errors.New("messaging is not available")
		}
		return tool.NewSendMessage(msg), nil
	case "read_inbox":
		if msg == nil {
			return nil, errors.New("messaging is not available")
		}
		return tool.NewReadInbox(msg), nil
	case "read_file":
		return tool.NewReadFile(root), nil
	case "write_file":
		if files == nil {
			return tool.NewWriteFile(root), nil
		}
		return trackedTool{Tool: tool.NewWriteFile(root), log: files}, nil
	case "bash":
		if ctl == nil {
			return tool.NewBash(root), nil
		}
		return recordingTool{Tool: tool.NewBash(root), ctl: ctl}, nil
	case "kubectl":
		return tool.NewKubectl("kubectl", w.opts.Config.Kubectl.Namespaces), nil
	case "remember":
		return tool.NewRemember(mem, agentName), nil
	case "recall":
		return tool.NewRecall(mem, agentName), nil
	}
	for _, at := range admintool.All(w.Manager(), w.opts.UserHome) {
		if at.Definition().Name == name {
			return at, nil
		}
	}
	return nil, fmt.Errorf("unknown tool %q", name)
}
