package flow

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// DelegationMode says what a route that names another agent does when a task
// opens. The empty value is hint, which is what groot did before delegation.
type DelegationMode string

const (
	DelegationAuto DelegationMode = "auto" // hand the task over through the inbox
	DelegationHint DelegationMode = "hint" // only name the agent in the result
	DelegationOff  DelegationMode = "off"  // neither hint nor handover
)

// ParseDelegation accepts auto, hint or off.
func ParseDelegation(s string) (DelegationMode, error) {
	switch DelegationMode(strings.TrimSpace(s)) {
	case DelegationAuto, DelegationHint, DelegationOff:
		return DelegationMode(strings.TrimSpace(s)), nil
	}
	return "", fmt.Errorf("delegation %q is invalid (use auto, hint or off)", s)
}

// DelegationStatus is where a handed-over task stands, on the origin's side.
type DelegationStatus string

const (
	DelegationHandedOver DelegationStatus = "handed_over"
	DelegationAccepted   DelegationStatus = "accepted" // the delegate opened its own task
	DelegationDelivered  DelegationStatus = "delivered"
	DelegationReturned   DelegationStatus = "returned"
)

// Delegation is the record a handover leaves on the origin task.
type Delegation struct {
	Delegate string
	Manager  string // the tracking agent of the space; empty when none
	Status   DelegationStatus
	HandedAt time.Time
	StatusAt time.Time
	// Assigned is set when the handover was placed by an assignment (task_assign
	// or /task assign), not by a route. An assignment in flight is released from
	// the owner's open slot: see Released.
	Assigned bool
}

// Set reports whether the task was ever handed over.
func (d Delegation) Set() bool { return d.Status != "" }

// Active reports whether the task is still with the delegate: the origin must
// not move it forward by itself, and its gate does not apply to it.
func (d Delegation) Active() bool {
	return d.Status == DelegationHandedOver || d.Status == DelegationAccepted
}

// Released reports whether the task is an assignment in flight. Its owner does
// not hold it as the open task, so the owner can open the next one.
func (d Delegation) Released() bool { return d.Assigned && d.Active() }

// Parent is the origin of a task that another agent took over. Task is the
// origin's task id in the origin's space.
type Parent struct {
	Space string
	Agent string
	Task  int64
}

// Set reports whether the task has an origin.
func (p Parent) Set() bool { return p.Task != 0 }

// Handover is the decision that a task goes to another agent. The caller
// sends the messages.
type Handover struct {
	Delegate string
	Manager  string
	// Note is what the origin adds for the delegate, when the handover is an
	// assignment. It is part of the handover body.
	Note string
}

// Opening is what Open decided beyond the task itself: a hint for the agent
// when the route names one, or a handover the caller must send.
type Opening struct {
	Hint     string
	Handover *Handover
}

// Signal is the kind of an inter-agent message about a task.
type Signal string

const (
	SignalHandover  Signal = "handover"  // to the delegate: take task #N
	SignalDelivered Signal = "delivered" // to the origin: task #N is done
	SignalReturned  Signal = "returned"  // to the origin: task #N came back
	SignalTracking  Signal = "tracking"  // to the manager: a handover happened
)

// Subject is a parsed task message subject: "task:#<id> <signal> [rest]".
type Subject struct {
	Signal Signal
	Task   int64
	Rest   string // the reason of a return
}

var subjectRe = regexp.MustCompile(`(?i)^(?:re:\s*)*task:#(\d+)\s+(handover|delivered|returned|tracking)\b\s*(.*)$`)

// ParseSubject reads a task subject. Leading "Re:" and surrounding spaces are
// ignored; the text after the signal is kept as Rest. A subject without the
// prefix is not a task message.
func ParseSubject(subject string) (Subject, bool) {
	m := subjectRe.FindStringSubmatch(strings.TrimSpace(subject))
	if m == nil {
		return Subject{}, false
	}
	id, err := strconv.ParseInt(m[1], 10, 64)
	if err != nil || id <= 0 {
		return Subject{}, false
	}
	return Subject{Signal: Signal(strings.ToLower(m[2])), Task: id, Rest: strings.TrimSpace(m[3])}, true
}

// SubjectFor builds the subject of a task message without a reason.
func SubjectFor(sig Signal, task int64) string {
	return fmt.Sprintf("task:#%d %s", task, sig)
}

// HandoverBody is the body of the handover message to the delegate. It names
// the origin address, which is where the delivery and the return go.
func HandoverBody(t Task, origin string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Task #%d (%s) is handed over to you from %s.\n", t.ID, t.Size, origin)
	fmt.Fprintf(&b, "Goal: %s\n", t.Goal)
	if t.Scope != "" {
		fmt.Fprintf(&b, "Scope: %s\n", t.Scope)
	}
	fmt.Fprintf(&b, "Done when: %s\n", t.DoneWhen)
	if t.PlanPath != "" {
		fmt.Fprintf(&b, "Plan: %s\n", t.PlanPath)
	}
	for _, s := range t.Specs {
		fmt.Fprintf(&b, "Spec S%d: %s\n", s.N, s.Text)
	}
	b.WriteString("\nWork it through your own task flow: task_classify, task_open with this goal, scope and done-when, implement, verify with a command, then task_advance deliver with a note on the changes and the risks.\n")
	fmt.Fprintf(&b, "Your delivery is reported to %s with subject %q and your deliver note as the body.\n", origin, SubjectFor(SignalDelivered, t.ID))
	fmt.Fprintf(&b, "If you cannot finish it, fail your task with a reason and send_message to %s with subject %q.\n", origin, fmt.Sprintf("task:#%d returned <reason>", t.ID))
	return b.String()
}

// delegatedSuffix is the tail of the context block for a task that was handed over.
func delegatedSuffix(t Task) string {
	if !t.Delegation.Set() {
		return ""
	}
	return fmt.Sprintf(" · delegated to %s (%s)", t.Delegation.Delegate, statusText(t.Delegation.Status))
}

// statusSuffix is the delegation and origin lines of /task and task_status.
func statusSuffix(t Task) string {
	var b strings.Builder
	if t.Parent.Set() {
		fmt.Fprintf(&b, "\nfrom %s/%s task #%d", t.Parent.Space, t.Parent.Agent, t.Parent.Task)
	}
	if d := t.Delegation; d.Set() {
		fmt.Fprintf(&b, "\ndelegation: → %s (%s since %s)", d.Delegate, statusText(d.Status), d.StatusAt.Local().Format("15:04"))
		if d.Manager != "" {
			fmt.Fprintf(&b, ", manager %s", d.Manager)
		}
	}
	return b.String()
}

func statusText(s DelegationStatus) string {
	return strings.ReplaceAll(string(s), "_", " ")
}

// ApplyReport moves a handed-over origin task on a report from its delegate:
// delivered moves it to verify with the report as evidence, returned moves it
// back to implement with the reason as evidence. It returns the updated task
// and the evidence to keep.
func ApplyReport(t Task, sub Subject, body string, now time.Time) (Task, Evidence, error) {
	if t.Closed() {
		return t, Evidence{}, fmt.Errorf("task #%d is closed (%s)", t.ID, t.Phase)
	}
	if !t.Delegation.Active() {
		return t, Evidence{}, fmt.Errorf("task #%d is not waiting on a delegate (%s)", t.ID, t.Delegation.Status)
	}
	next := t
	next.UpdatedAt = now
	next.Delegation.StatusAt = now
	body = strings.TrimSpace(body)
	switch sub.Signal {
	case SignalDelivered:
		next.Delegation.Status = DelegationDelivered
		next.Phase, next.PhaseAt = PhaseVerify, now
		return next, Evidence{Phase: PhaseVerify, Kind: KindNote, Text: body, At: now}, nil
	case SignalReturned:
		reason := strings.TrimSpace(sub.Rest)
		if reason == "" {
			reason = body
		}
		if reason == "" {
			return t, Evidence{}, fmt.Errorf("a return needs a reason")
		}
		next.Delegation.Status = DelegationReturned
		next.Phase, next.PhaseAt = PhaseImplement, now
		return next, Evidence{Phase: PhaseImplement, Kind: KindNote, Text: "returned by " + t.Delegation.Delegate + ": " + reason, At: now}, nil
	}
	return t, Evidence{}, fmt.Errorf("message %q is not a report", sub.Signal)
}
