package flow

import (
	"fmt"
	"time"
)

// Assign hands the open task t to agent to, on behalf of caller, and returns the
// task with the assignment recorded. Ownership stays with the opener: t.Agent
// does not change. Only the owner and the space manager may assign; manager is
// "" when the space has none. The caller saves the task and sends the handover.
//
// A task can be assigned while it is classified or planned, or after a return.
// A medium task opens straight in implement, so it is also assignable there
// while no command or verdict is recorded: nothing has been run yet. No assignee may be in
// flight, and the recipient must not be the caller or the owner. Whether the recipient is an agent of the space and holds no open task
// is checked by the caller, which reads the space.
func Assign(t Task, caller, manager, to string, now time.Time) (Task, error) {
	if t.Closed() {
		return Task{}, fmt.Errorf("task #%d is closed (%s)", t.ID, t.Phase)
	}
	if caller != t.Agent && (manager == "" || caller != manager) {
		return Task{}, fmt.Errorf("only the owner %s or the space manager may assign task #%d", t.Agent, t.ID)
	}
	if to == caller {
		return Task{}, fmt.Errorf("agent %s cannot assign task #%d to itself", caller, t.ID)
	}
	if to == t.Agent {
		return Task{}, fmt.Errorf("task #%d already belongs to %s", t.ID, to)
	}
	if t.Delegation.Active() {
		return Task{}, fmt.Errorf("task #%d is with %s (%s): wait for its report", t.ID, t.Delegation.Delegate, statusText(t.Delegation.Status))
	}
	returned := t.Delegation.Status == DelegationReturned
	unstarted := t.Phase == PhaseClassified || t.Phase == PhasePlanned || (t.Phase == PhaseImplement && !ranWork(t.Evidence))
	if !unstarted && !returned {
		return Task{}, fmt.Errorf("task #%d is in %s with work recorded: assign it before work starts, or after its return", t.ID, t.Phase)
	}
	next := t
	next.UpdatedAt = now
	next.Delegation = Delegation{
		Delegate: to, Manager: trackingManager(manager, t.Agent, to),
		Status: DelegationHandedOver, Assigned: true, HandedAt: now, StatusAt: now,
	}
	return next, nil
}

// ranWork reports whether the evidence holds a command run or a verdict. Notes
// such as an assignment do not count: nothing has been run by then.
func ranWork(ev []Evidence) bool {
	for _, e := range ev {
		if e.Kind == KindCommand || e.Kind == KindVerdict {
			return true
		}
	}
	return false
}

// trackingManager is the manager that tracks a handover of an origin to a
// delegate. The origin and the delegate track nothing.
func trackingManager(manager, origin, delegate string) string {
	if manager == "" || manager == origin || manager == delegate {
		return ""
	}
	return manager
}
