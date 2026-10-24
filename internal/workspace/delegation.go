package workspace

import (
	"context"
	"errors"
	"fmt"
	"slices"

	"github.com/tinchoram/groot-cli/internal/flow"
	"github.com/tinchoram/groot-cli/internal/inbox"
	"github.com/tinchoram/groot-cli/internal/space"
)

// delegateRule is appended to the system prompt of every agent. It says what a
// handover asks of the delegate.
const delegateRule = `Delegation: a message with the subject "task:#N handover" hands you a task from another agent. Classify it, open your own task with the same goal, scope and done-when, then implement, verify and deliver as usual. Your deliver note goes back to the origin on its own as "task:#N delivered". If you cannot finish it, fail your task with a reason and send_message to the origin with the subject "task:#N returned <reason>". Never drop a handover without a return.`

// managerRule is appended to the system prompt of the manager of a space.
const managerRule = `Manager: you track the task handovers of this space. Tracking messages have the subject "task:#N tracking"; reports arrive as "task:#N delivered" or "task:#N returned". You do not implement handed-over tasks. When asked for status, answer from read_inbox and list_tasks. Do not reply to tracking messages unless the user asks.`

// errNotThisTask means a message names a task that does not belong to the
// recipient, or no longer waits on the sender. Such a message is skipped.
var errNotThisTask = errors.New("not a handed-over task of this agent")

// SetSpaceDelegation sets the delegation mode and the manager of a space. An
// empty mode or manager keeps the current value; manager "none" removes the
// manager. The manager must be assigned to the space. Conversations opened from
// now on use the new values.
func (w *Workspace) SetSpaceDelegation(spaceName, mode, manager string) error {
	sp, ok := w.opts.Spaces.Get(spaceName)
	if !ok {
		return fmt.Errorf("no space named %q", spaceName)
	}
	if mode == "" {
		mode = sp.Flow.Delegation
	} else if _, err := flow.ParseDelegation(mode); err != nil {
		return err
	}
	switch {
	case manager == "":
		manager = sp.Flow.Manager
	case manager == "none":
	case !slices.Contains(sp.Agents, manager):
		return fmt.Errorf("agent %q is not assigned to space %q: assign it first, or use manager none", manager, spaceName)
	}
	return w.opts.Spaces.SetDelegation(spaceName, mode, manager)
}

// SetSpaceDelegation reaches the same method through the admin manager.
func (a adminManager) SetSpaceDelegation(_ context.Context, spaceName, mode, manager string) error {
	return a.w.SetSpaceDelegation(spaceName, mode, manager)
}

// resolveManager keeps a manager only when it is assigned to the space. "none"
// and an unassigned manager both mean no manager.
func resolveManager(sp space.Space, m Setting) Setting {
	if m.Value == "none" || !slices.Contains(sp.Agents, m.Value) {
		m.Value = ""
	}
	return m
}

// managerNoteLocked says what a manager lacks to track handovers. Callers hold w.mu.
func (w *Workspace) managerNoteLocked(manager string) string {
	if manager == "" {
		return ""
	}
	d, ok := w.def(manager)
	if !ok || slices.Contains(d.Tools, "list_tasks") {
		return ""
	}
	return fmt.Sprintf("manager %s lacks list_tasks: add it to its tools: line to answer status questions", manager)
}

// onTaskMessage runs the delegation side of a message that was just sent: the
// origin's transcript shows a handover, and a report is applied to its task.
// It runs before the recipient's turn starts, so the turn sees the new state.
func (w *Workspace) onTaskMessage(ctx context.Context, fromSpace, fromAgent, toSpace, toAgent, subject, body string) {
	sig, ok := flow.ParseSubject(subject)
	if !ok {
		return
	}
	switch sig.Signal {
	case flow.SignalHandover:
		w.notifyKey(fromSpace+"/"+fromAgent, fmt.Sprintf("⇢ task #%d handed over to %s", sig.Task, toAgent))
	case flow.SignalDelivered, flow.SignalReturned:
		w.receiveReport(ctx, toSpace, toAgent, fromSpace, fromAgent, subject, sig, body)
	}
}

// receiveReport applies a delivery or a return to the origin task it names. The
// recipient must be the origin and the sender its delegate; any other recipient
// (the manager's copy, for one) is skipped silently. The origin's transcript
// shows the report, and the manager gets a copy.
func (w *Workspace) receiveReport(ctx context.Context, space, agent, fromSpace, fromAgent, subject string, sig flow.Subject, body string) {
	var manager string
	err := w.editTask(ctx, space, sig.Task, true, func(t *flow.Task) ([]flow.Evidence, error) {
		if t.Agent != agent || fromSpace != space || t.Delegation.Delegate != fromAgent {
			return nil, errNotThisTask
		}
		next, ev, err := flow.ApplyReport(*t, sig, body, w.opts.Now())
		if err != nil {
			return nil, err
		}
		*t = next
		manager = next.Delegation.Manager
		return []flow.Evidence{ev}, nil
	})
	key := space + "/" + agent
	switch {
	case errors.Is(err, errNotThisTask):
		return
	case err != nil:
		w.warnKey(key, fmt.Sprintf("report from %s/%s on task #%d not applied: %v", fromSpace, fromAgent, sig.Task, err))
		return
	}
	verb := "delivered"
	if sig.Signal == flow.SignalReturned {
		verb = "returned"
	}
	w.notifyKey(key, fmt.Sprintf("⇠ task #%d %s by %s", sig.Task, verb, fromAgent))
	if manager == "" || manager == agent || manager == fromAgent {
		return
	}
	if _, _, err := w.messengerFor(space, agent).Send(ctx, manager, subject, "copy for the manager. "+body); err != nil {
		w.warnKey(key, fmt.Sprintf("copy of task #%d not sent to %s: %v", sig.Task, manager, err))
	}
}

// acceptDelegation marks the origin of a task as accepted by its delegate, when
// the delegate opens the task. The origin's live controller is not refreshed
// here: while a task is handed over its gate does not change between
// handed_over and accepted, and the next report refreshes it. Refreshing here
// would take the origin's lock while the delegate holds its own.
func (w *Workspace) acceptDelegation(ctx context.Context, t flow.Task) {
	if t.Space == t.Parent.Space && t.Agent == t.Parent.Agent {
		return // a task cannot be its own origin
	}
	_ = w.editTask(ctx, t.Parent.Space, t.Parent.Task, false, func(o *flow.Task) ([]flow.Evidence, error) {
		if o.Agent != t.Parent.Agent || o.Delegation.Status != flow.DelegationHandedOver || o.Delegation.Delegate != t.Agent {
			return nil, errNotThisTask
		}
		now := w.opts.Now()
		o.Delegation.Status, o.Delegation.StatusAt = flow.DelegationAccepted, now
		o.UpdatedAt = now
		return []flow.Evidence{{Phase: o.Phase, Kind: flow.KindNote, Text: "accepted by " + t.Agent, At: now}}, nil
	})
}

// editTask loads task id of a space, lets edit change it, and saves the task
// with the evidence edit returns. When refresh is set, the open controller of
// the task's agent reloads it. A task that cannot be read is errNotThisTask:
// the message concerns some other space.
func (w *Workspace) editTask(ctx context.Context, spaceName string, id int64, refresh bool, edit func(*flow.Task) ([]flow.Evidence, error)) error {
	sp, ok := w.opts.Spaces.Get(spaceName)
	if !ok {
		return errNotThisTask
	}
	w.mu.Lock()
	st, err := w.storesFor(sp.Path)
	w.mu.Unlock()
	if err != nil {
		return err
	}
	store := st.mem.Tasks()
	t, err := store.Get(ctx, id)
	if err != nil {
		return errNotThisTask
	}
	evidence, err := edit(&t)
	if err != nil {
		return err
	}
	if err := store.Update(ctx, t); err != nil {
		return err
	}
	for _, ev := range evidence {
		if err := store.AddEvidence(ctx, t.ID, ev); err != nil {
			return err
		}
	}
	if !refresh {
		return nil
	}
	w.mu.Lock()
	live := w.convs[spaceName+"/"+t.Agent]
	w.mu.Unlock()
	if live != nil && live.ctl != nil {
		return live.ctl.Refresh(ctx)
	}
	return nil
}

// expectHandover links the next task this conversation opens to the origin of
// a handover it reads.
func (c *Conversation) expectHandover(m inbox.Message) {
	sig, ok := flow.ParseSubject(m.Subject)
	if !ok || sig.Signal != flow.SignalHandover || c.ctl == nil {
		return
	}
	c.ctl.Expect(flow.Parent{Space: m.FromSpace, Agent: m.FromAgent, Task: sig.Task})
}
