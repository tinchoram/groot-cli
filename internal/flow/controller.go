package flow

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"
)

// Mode decides what a rule violation does: strict denies the tool call,
// advisory lets it run and adds a warning.
type Mode string

const (
	ModeStrict   Mode = "strict"
	ModeAdvisory Mode = "advisory"
)

// ParseMode accepts strict or advisory, and treats an empty value as strict.
func ParseMode(s string) (Mode, error) {
	switch Mode(strings.TrimSpace(s)) {
	case "", ModeStrict:
		return ModeStrict, nil
	case ModeAdvisory:
		return ModeAdvisory, nil
	}
	return "", fmt.Errorf("flow mode %q is invalid (use strict or advisory)", s)
}

// Route is the model and agent a size is routed to. Model is applied when a
// task opens; Agent is a hint recorded on the task.
type Route struct {
	Model string
	Agent string
}

// Routes holds the route of each size.
type Routes struct {
	Small, Medium, Large Route
}

// For returns the route of a size.
func (r Routes) For(s Size) Route {
	switch s {
	case SizeSmall:
		return r.Small
	case SizeMedium:
		return r.Medium
	}
	return r.Large
}

// maxCommandOutput is how much of a command's output is kept as evidence.
const maxCommandOutput = 300

// Options wire a controller to its conversation.
type Options struct {
	Space, Agent string
	Store        Store
	Mode         Mode
	Routes       Routes
	// Now is the clock. Nil uses time.Now.
	Now func() time.Time
	// PlanExists reports whether a plan file exists in the space. Nil accepts any path.
	PlanExists func(path string) bool
	// ApplyRoute switches the model for a route and returns the function that
	// restores the previous model. Nil skips the switch.
	ApplyRoute func(Route) (restore func())
	// Warn receives advisory warnings. Nil drops them.
	Warn func(msg string)
	// OnOpen runs when a task opens, and OnDeliver when it reaches deliver. Both
	// run under the controller lock: they must not call the controller.
	OnOpen    func(t Task)
	OnDeliver func(t Task)
	// Exempt names tools the rules never gate, such as admin tools that change
	// the catalog and not the code.
	Exempt []string
}

// Controller enforces the flow for one conversation. It keeps the current
// classification and a cache of the open task, so that the UI and the system
// prompt can read them without touching the database. All methods are safe for
// concurrent use.
type Controller struct {
	opts Options

	mu         sync.Mutex
	classified Size
	released   bool // a task reached deliver; writes are allowed until the next classification
	active     *Task
	restore    func()
}

// NewController builds a controller and loads the open task of the agent, if any.
func NewController(ctx context.Context, opts Options) (*Controller, error) {
	if opts.Store == nil {
		return nil, errors.New("flow: a task store is required")
	}
	if opts.Mode == "" {
		opts.Mode = ModeStrict
	}
	if opts.Now == nil {
		opts.Now = time.Now
	}
	c := &Controller{opts: opts}
	tk, ok, err := opts.Store.Active(ctx, opts.Space, opts.Agent)
	if err != nil {
		return nil, fmt.Errorf("load open task: %w", err)
	}
	if ok {
		c.active = &tk
	}
	return c, nil
}

// Warn passes a warning to the conversation's warning sink, if it has one.
func (c *Controller) Warn(msg string) {
	if c.opts.Warn != nil {
		c.opts.Warn(msg)
	}
}

// Active returns the open task, if there is one.
func (c *Controller) Active() (Task, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.active == nil {
		return Task{}, false
	}
	return *c.active, true
}

// Classified returns the classification of the current turn series, or "".
func (c *Controller) Classified() Size {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.classified
}

// StartTurn begins a new turn series. Without an open task the classification
// is forgotten, so the next write needs a new one.
func (c *Controller) StartTurn() {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.active == nil {
		c.classified = ""
		c.released = false
	}
}

// Classify records the size of the work and returns what the engine will require.
func (c *Controller) Classify(size Size, reason string) (string, error) {
	if !size.Valid() {
		return "", fmt.Errorf("size %q is not valid (small, medium or large)", size)
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.classified = size
	c.released = false
	route := c.opts.Routes.For(size)
	var b strings.Builder
	fmt.Fprintf(&b, "classified %s (%s)", size, strings.TrimSpace(reason))
	fmt.Fprintf(&b, "\nroute: %s", describeRoute(route))
	switch size {
	case SizeSmall:
		b.WriteString("\nno task needed: write directly; the route is not applied")
	case SizeMedium:
		b.WriteString("\nnext: task_open, then implement, verify with a command, deliver with a note")
	default:
		b.WriteString("\nnext: task_open, then write the plan (task_advance phase planned with its path), implement, verify with a command, deliver with a note")
	}
	return b.String(), nil
}

func describeRoute(r Route) string {
	parts := []string{"model unchanged"}
	if r.Model != "" {
		parts[0] = "model " + r.Model
	}
	if r.Agent != "" {
		parts = append(parts, "agent "+r.Agent)
	}
	return strings.Join(parts, ", ")
}

// Open opens a task for the current classification. It applies the route's
// model, and returns the route and a hint when the route names an agent.
func (c *Controller) Open(ctx context.Context, goal, scope, doneWhen string) (Task, string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.classified == "" {
		return Task{}, "", errors.New("classify first (task_classify)")
	}
	if c.active != nil {
		return Task{}, "", fmt.Errorf("task #%d is still open (%s): finish it with task_advance or fail it with /task fail", c.active.ID, c.active.Phase)
	}
	t, err := NewTask(c.classified, goal, scope, doneWhen, c.opts.Now())
	if err != nil {
		return Task{}, "", err
	}
	t.Space, t.Agent = c.opts.Space, c.opts.Agent
	saved, err := c.opts.Store.Open(ctx, t)
	if err != nil {
		return Task{}, "", fmt.Errorf("save task: %w", err)
	}
	c.active = &saved
	if c.opts.OnOpen != nil {
		c.opts.OnOpen(saved)
	}

	route := c.opts.Routes.For(c.classified)
	if route.Model != "" && c.opts.ApplyRoute != nil {
		c.restore = c.opts.ApplyRoute(route)
	}
	hint := ""
	if route.Agent != "" {
		hint = fmt.Sprintf("route for %s is agent %s: hand over with send_message", c.classified, route.Agent)
		if err := c.record(ctx, Evidence{Phase: saved.Phase, Kind: KindNote, Text: hint, At: c.opts.Now()}); err != nil {
			return saved, hint, err
		}
	}
	return saved, hint, nil
}

// Advance moves the open task to phase to. The text is the evidence of the move:
// the plan path, the failure, the delivery note or the failure reason.
func (c *Controller) Advance(ctx context.Context, to Phase, text string) (Task, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.active == nil {
		return Task{}, errors.New("no open task: open one with task_open")
	}
	if to == PhasePlanned {
		path := strings.TrimSpace(text)
		if c.opts.PlanExists != nil && path != "" && !c.opts.PlanExists(path) {
			return Task{}, fmt.Errorf("plan file %q not found in the space: write it first, then advance", path)
		}
	}
	next, ev, err := Transition(*c.active, to, text, c.opts.Now())
	if err != nil {
		return Task{}, err
	}
	if err := c.opts.Store.Update(ctx, next); err != nil {
		return Task{}, fmt.Errorf("save task: %w", err)
	}
	if ev.Kind != "" {
		if err := c.record(ctx, ev); err != nil {
			return Task{}, err
		}
	}
	next.Evidence = c.active.Evidence
	if ev.Kind != "" {
		next.Evidence = append(next.Evidence, ev)
	}

	c.active = &next
	if to == PhaseDeliver && c.opts.OnDeliver != nil {
		c.opts.OnDeliver(next)
	}

	switch to {
	case PhaseImplement:
		c.released = false
	case PhaseDeliver, PhaseDone:
		c.released = true
	}
	if next.Closed() {
		c.active = nil
		c.classified = ""
		c.finishRoute()
	}
	return next, nil
}

// Fail closes the open task with a reason.
func (c *Controller) Fail(ctx context.Context, reason string) (Task, error) {
	return c.Advance(ctx, PhaseFailed, reason)
}

// finishRoute restores the model the route replaced. Callers hold c.mu.
func (c *Controller) finishRoute() {
	if c.restore != nil {
		c.restore()
		c.restore = nil
	}
}

// RecordBash records a bash run as command evidence when a task is in implement
// or verify. input is the raw tool input; it must carry a "command" field.
func (c *Controller) RecordBash(ctx context.Context, input, output string, isErr bool) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.active == nil || (c.active.Phase != PhaseImplement && c.active.Phase != PhaseVerify) {
		return nil
	}
	var in struct {
		Command string `json:"command"`
	}
	if err := json.Unmarshal([]byte(input), &in); err != nil || strings.TrimSpace(in.Command) == "" {
		return nil
	}
	status := "0"
	if isErr {
		status = "non-zero"
	}
	text := fmt.Sprintf("$ %s\n%s\nexit: %s", strings.TrimSpace(in.Command), cut(output, maxCommandOutput), status)
	return c.record(ctx, Evidence{Phase: c.active.Phase, Kind: KindCommand, Text: text, At: c.opts.Now()})
}

// record stores one evidence record on the open task. Callers hold c.mu.
func (c *Controller) record(ctx context.Context, ev Evidence) error {
	if c.active == nil {
		return nil
	}
	if err := c.opts.Store.AddEvidence(ctx, c.active.ID, ev); err != nil {
		return fmt.Errorf("save evidence: %w", err)
	}
	c.active.Evidence = append(c.active.Evidence, ev)
	return nil
}

func cut(s string, n int) string {
	r := []rune(strings.TrimRight(s, "\n"))
	if len(r) <= n {
		return string(r)
	}
	return string(r[:n]) + "…"
}

// Status describes the classification and the open task, for /task and task_status.
func (c *Controller) Status(context.Context) string {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.active == nil {
		if c.classified == "" {
			return "no open task; nothing classified in this turn"
		}
		return fmt.Sprintf("no open task; classified %s", c.classified)
	}
	t := c.active
	return fmt.Sprintf("task #%d · %s · %s\ngoal: %s\nscope: %s\ndone when: %s\nevidence: %d",
		t.ID, t.Size, t.Phase, t.Goal, t.Scope, t.DoneWhen, len(t.Evidence))
}

// ContextBlock is the line prepended to the system prompt on every turn.
func (c *Controller) ContextBlock(context.Context) string {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.active != nil {
		t := c.active
		return fmt.Sprintf("[task #%d · %s · %s] goal: %s · done when: %s · evidence so far: %d",
			t.ID, t.Size, t.Phase, t.Goal, t.DoneWhen, len(t.Evidence))
	}
	if c.classified != "" {
		return fmt.Sprintf("[classified %s]", c.classified)
	}
	return ""
}

// Check applies the flow rules to one tool call. risky is true for write and
// exec calls; reads always pass. It returns whether to deny, the reason, and
// an advisory warning when the mode is advisory.
func (c *Controller) Check(ctx context.Context, tool, input string, risky bool) (deny bool, reason, warning string) {
	if !risky || strings.HasPrefix(tool, "task_") || tool == "send_message" || slices.Contains(c.opts.Exempt, tool) {
		return false, "", ""
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.released {
		return false, "", ""
	}
	if c.active != nil {
		if c.active.Phase == PhaseImplement || c.planWrite(tool, input) {
			return false, "", ""
		}
		reason = fmt.Sprintf("task #%d is in %s: writes need the implement phase (task_advance phase implement)", c.active.ID, c.active.Phase)
	} else {
		switch c.classified {
		case "":
			reason = "classify first (task_classify) before writing or running commands"
		case SizeSmall:
			return false, "", ""
		default:
			reason = fmt.Sprintf("open a task first (task_open) — classification is %s", c.classified)
		}
	}
	if c.opts.Mode == ModeAdvisory {
		return false, "", reason
	}
	return true, reason, ""
}

// planWrite reports whether a write is the plan document of an open large
// task that has not reached implement yet: the plan is written before it is
// recorded, so any file under docs/tasks/ counts until a plan path is set.
func (c *Controller) planWrite(tool, input string) bool {
	if tool != "write_file" || c.active.Size != SizeLarge {
		return false
	}
	if c.active.Phase != PhaseClassified && c.active.Phase != PhasePlanned {
		return false
	}
	var in struct {
		Path string `json:"path"`
	}
	if err := json.Unmarshal([]byte(input), &in); err != nil || in.Path == "" {
		return false
	}
	path := filepath.ToSlash(filepath.Clean(in.Path))
	if c.active.PlanPath != "" {
		return path == filepath.ToSlash(filepath.Clean(c.active.PlanPath))
	}
	return strings.HasPrefix(path, "docs/tasks/")
}
