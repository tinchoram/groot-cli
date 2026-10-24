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
	// Delegation is what a route that names another agent does (see
	// DelegationMode). Manager is the tracking agent of the space; empty is none.
	Delegation DelegationMode
	Manager    string
	// CanDelegate reports whether an agent can take a handover, which means it is
	// assigned to the space. Nil means no agent can.
	CanDelegate func(agent string) bool
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
	// pending is the origin of the handover this conversation received; the next
	// task it opens is linked to it.
	pending *Parent
}

// Address is the conversation as an inbox address, "<space>/<agent>".
func (c *Controller) Address() string {
	return c.opts.Space + "/" + c.opts.Agent
}

// Expect records the origin of a handover this conversation received. The next
// task it opens is linked to that origin.
func (c *Controller) Expect(p Parent) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.pending = &p
}

// Refresh reloads the open task from the store. Callers use it after another
// conversation changed the task, such as a delivery report.
func (c *Controller) Refresh(ctx context.Context) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	tk, ok, err := c.opts.Store.Active(ctx, c.opts.Space, c.opts.Agent)
	if err != nil {
		return fmt.Errorf("reload open task: %w", err)
	}
	c.active = nil
	if ok {
		c.active = &tk
	}
	return nil
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

// Open opens a task for the current classification. A route that names another
// agent is handed over when delegation is auto and the agent can take it: the
// task is then recorded as handed over, the route model is not applied, and
// the caller sends the handover. Otherwise the route model applies, and the
// opening carries a hint when the route names an agent (unless delegation is
// off).
func (c *Controller) Open(ctx context.Context, goal, scope, doneWhen string) (Task, Opening, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.classified == "" {
		return Task{}, Opening{}, errors.New("classify first (task_classify)")
	}
	if c.active != nil {
		return Task{}, Opening{}, fmt.Errorf("task #%d is still open (%s): finish it with task_advance or fail it with /task fail", c.active.ID, c.active.Phase)
	}
	now := c.opts.Now()
	t, err := NewTask(c.classified, goal, scope, doneWhen, now)
	if err != nil {
		return Task{}, Opening{}, err
	}
	t.Space, t.Agent = c.opts.Space, c.opts.Agent
	route := c.opts.Routes.For(c.classified)
	handover := c.handsOver(route)
	if c.pending != nil {
		t.Parent = *c.pending
	}
	if handover {
		t.Delegation = Delegation{
			Delegate: route.Agent, Manager: c.managerFor(route.Agent),
			Status: DelegationHandedOver, HandedAt: now, StatusAt: now,
		}
	}
	saved, err := c.opts.Store.Open(ctx, t)
	if err != nil {
		return Task{}, Opening{}, fmt.Errorf("save task: %w", err)
	}
	c.pending = nil
	c.active = &saved
	if c.opts.OnOpen != nil {
		c.opts.OnOpen(saved)
	}
	if handover {
		ev := Evidence{Phase: saved.Phase, Kind: KindNote, Text: "handed over to " + route.Agent, At: now}
		if err := c.record(ctx, ev); err != nil {
			return saved, Opening{}, err
		}
		return saved, Opening{Handover: &Handover{Delegate: route.Agent, Manager: saved.Delegation.Manager}}, nil
	}
	if route.Model != "" && c.opts.ApplyRoute != nil {
		c.restore = c.opts.ApplyRoute(route)
	}
	op := Opening{}
	if route.Agent != "" && c.hintsRoute(route) {
		op.Hint = fmt.Sprintf("route for %s is agent %s: hand over with send_message", c.classified, route.Agent)
		if err := c.record(ctx, Evidence{Phase: saved.Phase, Kind: KindNote, Text: op.Hint, At: now}); err != nil {
			return saved, op, err
		}
	}
	return saved, op, nil
}

// handsOver reports whether a route hands the task over: delegation is auto,
// the route names another agent that is assigned to the space, and the task
// does not go back to the agent it came from.
func (c *Controller) handsOver(route Route) bool {
	if c.opts.Delegation != DelegationAuto || route.Agent == "" || route.Agent == c.opts.Agent {
		return false
	}
	if c.pending != nil && c.pending.Agent == route.Agent {
		return false
	}
	return c.opts.CanDelegate != nil && c.opts.CanDelegate(route.Agent)
}

// hintsRoute reports whether an opening names the route's agent. Off never
// does. Auto names it only when it cannot hand over: an agent working on its
// own route has nothing to be told.
func (c *Controller) hintsRoute(route Route) bool {
	switch c.opts.Delegation {
	case DelegationOff:
		return false
	case DelegationAuto:
		return route.Agent != c.opts.Agent
	}
	return true
}

// managerFor is the manager that tracks a handover to delegate. A manager that
// is the origin or the delegate tracks nothing.
func (c *Controller) managerFor(delegate string) string {
	m := c.opts.Manager
	if m == "" || m == c.opts.Agent || m == delegate {
		return ""
	}
	return m
}

// Advance moves the open task to phase to. The text is the evidence of the move:
// the plan path, the failure, the delivery note or the failure reason.
func (c *Controller) Advance(ctx context.Context, to Phase, text string) (Task, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.active == nil {
		return Task{}, errors.New("no open task: open one with task_open")
	}
	if c.active.Delegation.Active() && to != PhaseFailed {
		return Task{}, fmt.Errorf("task #%d is handed over to %s: wait for the delivery report, or fail it with a reason", c.active.ID, c.active.Delegation.Delegate)
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
		t.ID, t.Size, t.Phase, t.Goal, t.Scope, t.DoneWhen, len(t.Evidence)) + statusSuffix(*t)
}

// ContextBlock is the line prepended to the system prompt on every turn.
func (c *Controller) ContextBlock(context.Context) string {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.active != nil {
		t := c.active
		return fmt.Sprintf("[task #%d · %s · %s] goal: %s · done when: %s · evidence so far: %d%s",
			t.ID, t.Size, t.Phase, t.Goal, t.DoneWhen, len(t.Evidence), delegatedSuffix(*t))
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
	// A handed-over task is with its delegate: the origin must not implement
	// it itself. Reads, task tools and messaging passed above, so the origin
	// can still answer the user and wait for the report.
	if c.active != nil && c.active.Delegation.Active() {
		reason = fmt.Sprintf("task #%d is handed over to %s: wait for its report (task:#%d delivered) or fail the task",
			c.active.ID, c.active.Delegation.Delegate, c.active.ID)
		if c.opts.Mode == ModeAdvisory {
			return false, "", reason
		}
		return true, reason, ""
	}
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
