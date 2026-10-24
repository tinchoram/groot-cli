// Package flow is the ODD task engine: a size classification, an open task per
// agent in a space, and the phase transitions that a task must follow. It is
// pure domain code. Storage is a port (Store) and the controller that enforces
// the rules for one conversation lives beside it.
package flow

import (
	"errors"
	"fmt"
	"strings"
	"time"
)

// Size is the classification of a piece of work.
type Size string

const (
	SizeSmall  Size = "small"
	SizeMedium Size = "medium"
	SizeLarge  Size = "large"
)

// Valid reports whether s is a known size.
func (s Size) Valid() bool {
	switch s {
	case SizeSmall, SizeMedium, SizeLarge:
		return true
	}
	return false
}

// Phase is where a task stands.
type Phase string

const (
	PhaseClassified Phase = "classified" // opened; a large task writes its plan here
	PhasePlanned    Phase = "planned"    // large only: the plan file exists
	PhaseImplement  Phase = "implement"
	PhaseVerify     Phase = "verify"
	PhaseDeliver    Phase = "deliver"
	PhaseDone       Phase = "done"
	PhaseFailed     Phase = "failed"
)

// EvidenceKind says what a piece of evidence is.
type EvidenceKind string

const (
	KindCommand EvidenceKind = "command" // a bash run during implement or verify
	KindNote    EvidenceKind = "note"    // a phase note: failures, the delivery summary
	KindPlan    EvidenceKind = "plan"    // the plan file path of a large task
)

// Evidence is one record kept on a task.
type Evidence struct {
	Phase Phase
	Kind  EvidenceKind
	Text  string
	At    time.Time
}

// Task is one unit of work an agent is accountable for.
type Task struct {
	ID       int64
	Space    string
	Agent    string
	Size     Size
	Phase    Phase
	Goal     string
	Scope    string
	DoneWhen string
	PlanPath string
	// Delegation is set on the origin of a handover; Parent on the task that
	// another agent opened to take a handed-over task.
	Delegation Delegation
	Parent     Parent
	Evidence   []Evidence
	OpenedAt   time.Time
	// PhaseAt is when the current phase began. Entering implement starts a
	// cycle; verify only accepts commands run since then.
	PhaseAt   time.Time
	UpdatedAt time.Time
	ClosedAt  time.Time
}

// Closed reports whether the task is finished, done or failed.
func (t Task) Closed() bool { return t.Phase == PhaseDone || t.Phase == PhaseFailed }

// ErrSmallNoTask is returned when someone asks a small change to open a task.
var ErrSmallNoTask = errors.New("small work does not open a task; write directly")

// NewTask opens a medium or large task. The goal, scope and done-when are all
// required, because they are what the verification is checked against.
func NewTask(size Size, goal, scope, doneWhen string, now time.Time) (Task, error) {
	if !size.Valid() {
		return Task{}, fmt.Errorf("size %q is not valid (small, medium or large)", size)
	}
	if size == SizeSmall {
		return Task{}, ErrSmallNoTask
	}
	if strings.TrimSpace(goal) == "" {
		return Task{}, errors.New("goal is required")
	}
	if strings.TrimSpace(doneWhen) == "" {
		return Task{}, errors.New("done_when is required: say how you will know it is finished")
	}
	return Task{
		Size: size, Phase: openPhase(size),
		Goal: strings.TrimSpace(goal), Scope: strings.TrimSpace(scope), DoneWhen: strings.TrimSpace(doneWhen),
		OpenedAt: now, UpdatedAt: now, PhaseAt: now,
	}, nil
}

// openPhase is the phase a new task starts in. A medium task starts in
// implement, so task_open leads straight to the work; a large task starts in
// classified, where its plan is written and recorded first.
func openPhase(size Size) Phase {
	if size == SizeMedium {
		return PhaseImplement
	}
	return PhaseClassified
}

// Transition moves t to phase to and returns the updated task plus the evidence
// the move records (zero when it records none). It enforces the rules:
//
//	medium: implement -> verify -> deliver -> done (opened in implement)
//	large:  classified -> planned -> implement -> verify -> deliver -> done
//	verify -> implement on a failure, with the reason as evidence
//	any open phase -> failed, with the reason as evidence
//
// verify needs a command run during the current implement cycle, and deliver
// needs a note with the changes and the risks.
func Transition(t Task, to Phase, text string, now time.Time) (Task, Evidence, error) {
	if t.Closed() {
		return t, Evidence{}, fmt.Errorf("task #%d is closed (%s)", t.ID, t.Phase)
	}
	text = strings.TrimSpace(text)
	next := t
	next.UpdatedAt = now

	if to == PhaseFailed {
		if text == "" {
			return t, Evidence{}, errors.New("failing a task needs a reason")
		}
		next.Phase, next.ClosedAt, next.PhaseAt = PhaseFailed, now, now
		return next, Evidence{Phase: PhaseFailed, Kind: KindNote, Text: text, At: now}, nil
	}

	var ev Evidence
	switch {
	case t.Phase == PhaseClassified && to == PhaseImplement && t.Size == SizeMedium:
	case t.Phase == PhaseClassified && to == PhasePlanned && t.Size == SizeLarge:
		if text == "" {
			return t, Evidence{}, errors.New("plan needs the plan file path, for example docs/tasks/<id>.md")
		}
		next.PlanPath = text
		ev = Evidence{Phase: PhasePlanned, Kind: KindPlan, Text: text, At: now}
	case t.Phase == PhasePlanned && to == PhaseImplement && t.Size == SizeLarge:
	case t.Phase == PhaseImplement && to == PhaseVerify:
		if !hasCommandSince(t.Evidence, t.PhaseAt) {
			return t, Evidence{}, errors.New("verify needs a command run during implement: run the checks with bash first")
		}
	case t.Phase == PhaseVerify && to == PhaseImplement:
		if text == "" {
			return t, Evidence{}, errors.New("going back to implement needs the evidence of what failed")
		}
		ev = Evidence{Phase: PhaseImplement, Kind: KindNote, Text: text, At: now}
	case t.Phase == PhaseVerify && to == PhaseDeliver:
		if text == "" {
			return t, Evidence{}, errors.New("deliver needs a note with the changes and the risks")
		}
		ev = Evidence{Phase: PhaseDeliver, Kind: KindNote, Text: text, At: now}
	case t.Phase == PhaseDeliver && to == PhaseImplement:
		// A rejected delivery goes back to implement with the reason as evidence.
		if text == "" {
			return t, Evidence{}, errors.New("rejecting a delivery needs the reason")
		}
		ev = Evidence{Phase: PhaseImplement, Kind: KindNote, Text: text, At: now}
	case t.Phase == PhaseDeliver && to == PhaseDone:
		next.ClosedAt = now
	default:
		return t, Evidence{}, fmt.Errorf("cannot move a %s task from %s to %s", t.Size, t.Phase, to)
	}

	next.Phase, next.PhaseAt = to, now
	if ev.Kind != "" {
		ev.Phase = to
	}
	return next, ev, nil
}

// hasCommandSince reports whether a command was recorded at or after since.
func hasCommandSince(evidence []Evidence, since time.Time) bool {
	for _, e := range evidence {
		if e.Kind == KindCommand && !e.At.Before(since) {
			return true
		}
	}
	return false
}
