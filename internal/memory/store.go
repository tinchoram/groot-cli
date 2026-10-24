// Package memory is the persistence port for sessions, messages and long-term
// memories. SQLite is the default adapter; NoMemory disables persistence.
package memory

import (
	"context"
	"time"

	"github.com/tinchoram/groot-cli/internal/api"
)

// Kind classifies a memory entry.
type Kind string

const (
	KindFact           Kind = "fact"
	KindDecision       Kind = "decision"
	KindRunbook        Kind = "runbook"
	KindSessionSummary Kind = "session_summary"
)

// Valid reports whether k is one of the known kinds.
func (k Kind) Valid() bool {
	switch k {
	case KindFact, KindDecision, KindRunbook, KindSessionSummary:
		return true
	}
	return false
}

// Scope says who a memory applies to.
type Scope string

const (
	ScopeGlobal  Scope = "global"
	ScopeProject Scope = "project"
	ScopeAgent   Scope = "agent"
)

// Valid reports whether s is one of the known scopes.
func (s Scope) Valid() bool {
	switch s {
	case ScopeGlobal, ScopeProject, ScopeAgent:
		return true
	}
	return false
}

// Session is one agent conversation.
type Session struct {
	ID        string
	ParentID  string
	Agent     string
	Model     string
	StartedAt time.Time
	// The fields below are filled when a session is read back (LatestSession).
	EndedAt  time.Time // zero while the session has not ended
	Cleared  bool      // the user cleared it with /clear; it is never resumed
	Messages int       // stored messages
	Turns    int       // highest turn number stored
}

// Entry is one long-term memory.
type Entry struct {
	ID        int64
	Kind      Kind
	Scope     Scope
	Agent     string
	Text      string
	Source    string
	CreatedAt time.Time
}

// Store is the port the agent and tools use to persist state.
type Store interface {
	StartSession(ctx context.Context, s Session) error
	EndSession(ctx context.Context, id string, costUSD float64) error
	AppendMessage(ctx context.Context, sessionID string, turn int, m api.Message, u api.Usage) error
	Remember(ctx context.Context, e Entry) error
	// Recall searches the entries visible to agent (see Visible).
	Recall(ctx context.Context, query, agent string, limit int) ([]Entry, error)
	// Preamble returns the most recent visible entries to agent, newest first,
	// as a text block of at most limit entries.
	Preamble(ctx context.Context, agent string, limit int) (string, error)
	// LatestSession returns the most recently started session of agent,
	// cleared or not. ok is false when the agent has no session.
	LatestSession(ctx context.Context, agent string) (s Session, ok bool, err error)
	// SessionMessages returns the stored messages of a session in order.
	SessionMessages(ctx context.Context, id string) ([]api.Message, error)
	// SessionTimes returns the write time of each message, in SessionMessages order.
	SessionTimes(ctx context.Context, id string) ([]time.Time, error)
	// MarkCleared records that the user cleared a session.
	MarkCleared(ctx context.Context, id string) error
}

// NoMemory is a Store that persists nothing.
type NoMemory struct{}

func (NoMemory) StartSession(context.Context, Session) error { return nil }

func (NoMemory) EndSession(context.Context, string, float64) error { return nil }

func (NoMemory) AppendMessage(context.Context, string, int, api.Message, api.Usage) error {
	return nil
}

func (NoMemory) Remember(context.Context, Entry) error { return nil }

func (NoMemory) Recall(context.Context, string, string, int) ([]Entry, error) { return nil, nil }

func (NoMemory) Preamble(context.Context, string, int) (string, error) { return "", nil }

func (NoMemory) LatestSession(context.Context, string) (Session, bool, error) {
	return Session{}, false, nil
}

func (NoMemory) SessionMessages(context.Context, string) ([]api.Message, error) { return nil, nil }

func (NoMemory) SessionTimes(context.Context, string) ([]time.Time, error) { return nil, nil }

func (NoMemory) MarkCleared(context.Context, string) error { return nil }

// Visible is the SQL condition that selects the entries an agent may read:
// global entries, project entries written by that agent, and project rows
// without an agent (written before per-agent memory existed). Project entries
// of other agents stay private. Parameters: agent.
const Visible = "(m.scope = 'global' OR (m.scope = 'project' AND (m.agent = ? OR m.agent = '')))"
