package workspace

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	changelog "github.com/tinchoram/groot-cli"
	"github.com/tinchoram/groot-cli/internal/admintool"
	"github.com/tinchoram/groot-cli/internal/agent"
	"github.com/tinchoram/groot-cli/internal/api"
	"github.com/tinchoram/groot-cli/internal/clock"
	"github.com/tinchoram/groot-cli/internal/flow"
	"github.com/tinchoram/groot-cli/internal/gate"
	"github.com/tinchoram/groot-cli/internal/inbox"
	"github.com/tinchoram/groot-cli/internal/memory"
	"github.com/tinchoram/groot-cli/internal/provider"
	"github.com/tinchoram/groot-cli/internal/tool"
	"sync/atomic"
)

const (
	// recallEntries is how many matches /memory <query> shows.
	recallEntries   = 10
	summaryTimeout  = 30 * time.Second
	summaryMaxRunes = 100_000
	summarySystem   = "You write terse session summaries for an SRE audit trail."
	// summaryPrompt asks for the fixed shape of a session summary; the local
	// summary of exit.go uses the same headings.
	summaryPrompt = "Summarize this session in at most 12 lines, in exactly this shape. No preamble.\n" +
		"Goal: one line.\n" +
		"Done: up to 3 bullets (\"- item\").\n" +
		"Decisions: up to 2 bullets (\"- item\"), or \"none\".\n" +
		"Next: up to 3 bullets (\"- item\").\n" +
		"Files: up to 3 paths, or \"none\".\n\n"
)

// Conversation is one agent running inside one space. It wraps the agent loop
// with the model handle and the space's memory store.
type Conversation struct {
	*agent.Agent
	// mem is the memory view: the store, the agent that reads it, and its global
	// access. The memory tools and the prompt are built from it.
	mem   *memoryState
	prov  provider.Provider
	store memory.Store
	name  string
	space string
	inbox *inbox.Store
	// resumed is set when the conversation continued a stored session.
	resumed *Resumed
	// ctl enforces the ODD flow; baseSystem is the prompt before the flow block.
	ctl       *flow.Controller
	work      *gate.WorkState
	files     *fileLog
	review    *reviewBox
	busy      atomic.Bool  // a turn is running: UI-driven or triggered by a message
	autoTurns atomic.Int64 // turns triggered by messages in this session
	// sysMu guards baseSystem: /clear refreshes it while a turn may be reading it.
	sysMu      sync.Mutex
	baseSystem string
	// rebuildSystem recomputes the prompt before the flow block from the memory
	// stored now, with the agent prompt and project context of the open.
	rebuildSystem func(ctx context.Context) (string, error)
	// startModel is the model the conversation started with; prevModel is the
	// one before the last change. A model the provider does not know reverts to
	// them. warn reports the revert.
	startModel string
	prevModel  string
	revertWarn func(string)
	// budgets reads the budgets of this agent and space with their spend.
	// Nil means no budget checks. budgetNotice gets the one-time warning that
	// the next turn may pass a limit; budgetWarned records that it was given.
	budgets      func(ctx context.Context) ([]Budget, error)
	budgetNotice func(string)
	budgetWarned bool
	// lastTurnUSD is what the last turn cost; it estimates the next one.
	lastTurnUSD float64
	// clock reads the time for the time line of each turn. Nil reads time.Now.
	clock func() time.Time
	// rebuildTools builds the registry of this conversation for a tool list,
	// with the same flow, files, messenger, memory and guard it was opened with.
	// Used when the agent's tools change while the conversation is open.
	rebuildTools func(names []string) (*tool.Registry, error)
}

// Model returns the model used for the next call.
func (c *Conversation) Model() string { return c.prov.Model() }

// turnBlock is the context put in front of the base prompt on every turn: the
// current time, then the flow line when there is a flow. It is rebuilt each
// turn; the base prompt stays as it was cached.
func (c *Conversation) turnBlock(ctx context.Context) string {
	now := time.Now
	if c.clock != nil {
		now = c.clock
	}
	lines := []string{clock.PromptLine(now(), time.Local)}
	if c.ctl != nil {
		c.ctl.StartTurn()
		if flowLine := c.ctl.ContextBlock(ctx); flowLine != "" {
			lines = append(lines, flowLine)
		}
	}
	return strings.Join(lines, "\n")
}

// base returns the prompt before the flow block.
func (c *Conversation) base() string {
	c.sysMu.Lock()
	defer c.sysMu.Unlock()
	return c.baseSystem
}

// refreshSystem recomputes the prompt from the memory stored now. On failure the
// previous prompt stays in place.
func (c *Conversation) refreshSystem(ctx context.Context) error {
	if c.rebuildSystem == nil {
		return nil
	}
	sys, err := c.rebuildSystem(ctx)
	if err != nil {
		return err
	}
	c.sysMu.Lock()
	defer c.sysMu.Unlock()
	c.baseSystem = sys
	return nil
}

// PromptMemory returns the entries of the prompt preamble of this agent, within
// the default budget, and how many entries the agent can read in all.
func (c *Conversation) PromptMemory(ctx context.Context) ([]memory.Entry, memory.Overview, error) {
	entries, err := c.store.Preamble(ctx, c.mem.reader(), c.mem.budget)
	if err != nil {
		return nil, memory.Overview{}, err
	}
	ov, err := c.store.Overview(ctx, c.mem.reader())
	if err != nil {
		return nil, memory.Overview{}, err
	}
	return entries, ov, nil
}

// RecallMemory searches the entries this agent can read, newest matches first.
func (c *Conversation) RecallMemory(ctx context.Context, query string) ([]memory.Entry, error) {
	return c.store.Recall(ctx, c.mem.reader(), query, recallEntries)
}

// MemoryEntry returns one entry this agent can read, with its full text.
func (c *Conversation) MemoryEntry(ctx context.Context, id int64) (memory.Entry, error) {
	return c.store.Get(ctx, id, c.mem.reader())
}

// MemoryPin pins or unpins an entry. The prompt is rebuilt, so the change is in
// the next turn.
func (c *Conversation) MemoryPin(ctx context.Context, id int64, pinned bool) error {
	if err := c.store.SetPinned(ctx, id, c.mem.reader(), pinned); err != nil {
		return err
	}
	return c.refreshSystem(ctx)
}

// MemoryForget removes an entry this agent can change, and rebuilds the prompt.
func (c *Conversation) MemoryForget(ctx context.Context, id int64) error {
	if err := c.store.Forget(ctx, id, c.mem.reader()); err != nil {
		return err
	}
	return c.refreshSystem(ctx)
}

// MemoryReview lists the entries whose review date has passed, oldest first.
func (c *Conversation) MemoryReview(ctx context.Context) ([]memory.Entry, error) {
	return c.store.NeedsReview(ctx, c.mem.reader(), recallEntries)
}

// MemoryReviewed moves the review date of an entry one interval ahead, and
// rebuilds the prompt, which marks the entry as reviewed.
func (c *Conversation) MemoryReviewed(ctx context.Context, id int64) error {
	if err := c.store.MarkReviewed(ctx, id, c.mem.reader()); err != nil {
		return err
	}
	return c.refreshSystem(ctx)
}

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
	if err := c.checkBudget(ctx, c.lastTurnUSD); err != nil {
		return "", err
	}
	c.Agent.System = withBlock(c.turnBlock(ctx), c.base())
	prompt = c.injectInbox(ctx, prompt)
	if prompt == "" {
		return "", nil
	}
	before := c.Agent.SessionCost()
	resp, err := c.Agent.Send(ctx, prompt)
	if err != nil && provider.IsModelNotFound(err) && c.revertModel() {
		// The failed attempt left the prompt in the history; the retry answers it.
		resp, err = c.Agent.Send(ctx, prompt)
	}
	c.lastTurnUSD = c.Agent.SessionCost() - before
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
	return c.storeSummary(sctx, id, text)
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
func (w *Workspace) buildTools(root string, names []string, mem *memoryState, msg tool.Messenger, ctl *flow.Controller, files *fileLog, agentName string) (*tool.Registry, error) {
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

func (w *Workspace) newTool(name, root string, mem *memoryState, msg tool.Messenger, ctl *flow.Controller, files *fileLog, agentName string) (tool.Tool, error) {
	switch name {
	case "task_classify", "task_open", "task_advance", "task_status":
		if ctl == nil {
			return nil, errors.New("the task flow is not available")
		}
		switch name {
		case "task_classify":
			return tool.NewTaskClassify(ctl), nil
		case "task_open":
			return tool.NewTaskOpen(ctl, msg), nil
		case "task_advance":
			return tool.NewTaskAdvance(ctl, msg), nil
		}
		return tool.NewTaskStatus(ctl), nil
	case "feedback_report":
		return tool.NewFeedbackReport(w.feedback, w.spaceNameFor(root), agentName, w.opts.Version), nil
	case "feedback_list":
		return tool.NewFeedbackList(w.feedback), nil
	case "read_changelog":
		return tool.NewReadChangelog(changelog.Text), nil
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
	case "current_time":
		return tool.NewCurrentTime(w.opts.Now), nil
	case "read_file":
		return tool.NewReadFile(root), nil
	case "list_dir":
		return tool.NewListDir(root), nil
	case "glob":
		return tool.NewGlob(root), nil
	case "write_file":
		if files == nil {
			return tool.NewWriteFile(root), nil
		}
		return trackedTool{Tool: tool.NewWriteFile(root), log: files}, nil
	case "bash":
		b := tool.NewBash(root)
		b.Runner = w.opts.Sandbox
		b.Network = w.opts.Config.Bash.Sandbox.Network
		b.ExtraWritable = w.opts.Config.Bash.Sandbox.ExtraWritable
		if ctl == nil {
			return b, nil
		}
		return recordingTool{Tool: b, ctl: ctl}, nil
	case "kubectl":
		return tool.NewKubectl("kubectl", w.opts.Config.Kubectl.Namespaces), nil
	case "remember":
		return tool.NewRemember(mem.access()), nil
	case "recall":
		return tool.NewRecall(mem.access()), nil
	case "memory_get":
		return tool.NewMemoryGet(mem.access()), nil
	case "forget":
		return tool.NewForget(mem.access()), nil
	case "update_memory":
		return tool.NewUpdateMemory(mem.access()), nil
	}
	for _, at := range admintool.All(w.Manager(), w.opts.UserHome) {
		if at.Definition().Name == name {
			return at, nil
		}
	}
	return nil, fmt.Errorf("unknown tool %q", name)
}
