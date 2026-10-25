// Package memory is the persistence port for sessions, messages and long-term
// memories. SQLite is the default adapter; NoMemory disables persistence.
package memory

import (
	"context"
	"errors"
	"fmt"
	"strings"
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
	// ScopeAgent is visible only to the agent that wrote it, in its space database.
	ScopeAgent Scope = "agent"
	// ScopeSpace is visible to every agent of the space database.
	ScopeSpace Scope = "space"
	// ScopeGlobal lives in the root space database. It is visible only to a
	// reader that may see global entries (Reader.Global).
	ScopeGlobal Scope = "global"
)

// Valid reports whether s is one of the known scopes.
func (s Scope) Valid() bool {
	switch s {
	case ScopeAgent, ScopeSpace, ScopeGlobal:
		return true
	}
	return false
}

// Reader says who reads memory: the agent, and whether it may see global entries.
type Reader struct {
	// Agent is the reading agent. An empty name matches no agent-scoped entry.
	Agent string
	// Global lets the reader see global entries.
	Global bool
}

// Entry states, computed when an entry is read.
const (
	StateActive      = "active"
	StateNeedsReview = "needs_review"
)

// Outcome says what Remember did with an entry.
type Outcome string

const (
	OutcomeCreated   Outcome = "created"   // a new row
	OutcomeUpdated   Outcome = "updated"   // the row with the same topic key was revised
	OutcomeDuplicate Outcome = "duplicate" // the same content already exists; nothing was inserted
)

// Result is what Remember reports: the row that holds the entry and what happened to it.
type Result struct {
	ID      int64
	Outcome Outcome
}

// Budget bounds the Preamble. Zero fields take the defaults.
//
// A line costs its rune count divided by four tokens. That is a rough
// heuristic, close enough for prose, and it avoids shipping a tokenizer.
type Budget struct {
	Tokens       int // total estimated tokens; default DefaultBudgetTokens
	MaxSummaries int // session summaries kept, newest first; default DefaultBudgetSummaries
	EntryRunes   int // runes kept of each entry text; default DefaultBudgetEntryRunes
}

// Default budget values.
const (
	DefaultBudgetTokens     = 1500
	DefaultBudgetSummaries  = 3
	DefaultBudgetEntryRunes = 400
)

func (b Budget) withDefaults() Budget {
	if b.Tokens <= 0 {
		b.Tokens = DefaultBudgetTokens
	}
	if b.MaxSummaries <= 0 {
		b.MaxSummaries = DefaultBudgetSummaries
	}
	if b.EntryRunes <= 0 {
		b.EntryRunes = DefaultBudgetEntryRunes
	}
	return b
}

// Patch changes the given fields of an entry. Nil fields are left as they are.
type Patch struct {
	Title    *string
	Text     *string
	Pinned   *bool
	TopicKey *string
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
	CostUSD  float64   // spend stored for the session so far
	// ContextTokens is the context size of the last model answer: the prompt
	// tokens the model read, cached or not. Zero before the first answer.
	ContextTokens int64
}

// Overview counts the entries a reader can read.
type Overview struct {
	Visible     int
	Summaries   int
	Pinned      int
	NeedsReview int
}

// Entry is one long-term memory.
type Entry struct {
	ID             int64
	Kind           Kind
	Scope          Scope
	Agent          string
	Title          string
	Text           string
	TopicKey       string
	Source         string
	Pinned         bool
	Tainted        bool
	ReviewAfter    time.Time // zero when the kind has no review interval; on Remember, overrides the default
	RevisionCount  int
	DuplicateCount int
	State          string // StateActive or StateNeedsReview, computed on read
	CreatedAt      time.Time
	UpdatedAt      time.Time
	Truncated      bool // Preamble cut Text to its budget
}

// ErrNotFound is returned when an entry does not exist or the reader cannot see it.
var ErrNotFound = errors.New("memory entry not found")

// Store is the port the agent and tools use to persist state.
type Store interface {
	StartSession(ctx context.Context, s Session) error
	EndSession(ctx context.Context, id string, costUSD float64) error
	// SetSessionCost stores the spend of a session so far. The agent calls it
	// after every model answer, so a crash loses no spend.
	SetSessionCost(ctx context.Context, id string, costUSD float64) error
	// SetContextTokens stores the context size of the last model answer of a
	// session, so a resumed session shows it before its next turn.
	SetContextTokens(ctx context.Context, id string, tokens int64) error
	// SpendUSD is the total spend of agent in this store, over all its sessions.
	SpendUSD(ctx context.Context, agent string) (float64, error)
	// SpendByAgent is the total spend of each agent in this store.
	SpendByAgent(ctx context.Context) (map[string]float64, error)
	AppendMessage(ctx context.Context, sessionID string, turn int, m api.Message, u api.Usage) error
	// Remember stores an entry, or revises or counts an existing one (see Result).
	Remember(ctx context.Context, e Entry) (Result, error)
	// Recall searches the entries visible to r, best match first.
	Recall(ctx context.Context, r Reader, query string, limit int) ([]Entry, error)
	// Preamble returns the entries visible to r that fit the budget, in the
	// order the prompt wants them. FormatPreamble renders them as text.
	Preamble(ctx context.Context, r Reader, b Budget) ([]Entry, error)
	// Overview counts the entries visible to r.
	Overview(ctx context.Context, r Reader) (Overview, error)
	// Get returns one visible entry with its full text.
	Get(ctx context.Context, id int64, r Reader) (Entry, error)
	// Update changes the fields of a visible entry that the patch sets.
	Update(ctx context.Context, id int64, r Reader, p Patch) error
	// SetPinned pins or unpins a visible entry.
	SetPinned(ctx context.Context, id int64, r Reader, pinned bool) error
	// MarkReviewed moves the review date of a visible entry one interval ahead.
	MarkReviewed(ctx context.Context, id int64, r Reader) error
	// Forget soft-deletes a visible entry. It leaves every read path.
	Forget(ctx context.Context, id int64, r Reader) error
	// NeedsReview returns the visible entries whose review date has passed, oldest review date first.
	NeedsReview(ctx context.Context, r Reader, limit int) ([]Entry, error)
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

// FormatPreamble renders entries as the "- [kind] text" lines of the prompt.
// An entry with a title reads "- [kind] title: text".
func FormatPreamble(entries []Entry) string {
	var sb strings.Builder
	for _, e := range entries {
		sb.WriteString(preambleLine(e))
		sb.WriteString("\n")
	}
	return sb.String()
}

func preambleLine(e Entry) string {
	body := e.Text
	if e.Title != "" {
		body = e.Title + ": " + e.Text
	}
	return fmt.Sprintf("- [%s] %s", e.Kind, body)
}

// NoMemory is a Store that persists nothing.
type NoMemory struct{}

func (NoMemory) StartSession(context.Context, Session) error { return nil }

func (NoMemory) EndSession(context.Context, string, float64) error { return nil }

func (NoMemory) SetSessionCost(context.Context, string, float64) error { return nil }

func (NoMemory) SetContextTokens(context.Context, string, int64) error { return nil }

func (NoMemory) SpendUSD(context.Context, string) (float64, error) { return 0, nil }

func (NoMemory) SpendByAgent(context.Context) (map[string]float64, error) { return nil, nil }

func (NoMemory) AppendMessage(context.Context, string, int, api.Message, api.Usage) error {
	return nil
}

func (NoMemory) Remember(context.Context, Entry) (Result, error) { return Result{}, nil }

func (NoMemory) Recall(context.Context, Reader, string, int) ([]Entry, error) { return nil, nil }

func (NoMemory) Preamble(context.Context, Reader, Budget) ([]Entry, error) { return nil, nil }

func (NoMemory) Overview(context.Context, Reader) (Overview, error) { return Overview{}, nil }

func (NoMemory) Get(context.Context, int64, Reader) (Entry, error) { return Entry{}, ErrNotFound }

func (NoMemory) Update(context.Context, int64, Reader, Patch) error { return ErrNotFound }

func (NoMemory) SetPinned(context.Context, int64, Reader, bool) error { return ErrNotFound }

func (NoMemory) MarkReviewed(context.Context, int64, Reader) error { return ErrNotFound }

func (NoMemory) Forget(context.Context, int64, Reader) error { return ErrNotFound }

func (NoMemory) NeedsReview(context.Context, Reader, int) ([]Entry, error) { return nil, nil }

func (NoMemory) LatestSession(context.Context, string) (Session, bool, error) {
	return Session{}, false, nil
}

func (NoMemory) SessionMessages(context.Context, string) ([]api.Message, error) { return nil, nil }

func (NoMemory) SessionTimes(context.Context, string) ([]time.Time, error) { return nil, nil }

func (NoMemory) MarkCleared(context.Context, string) error { return nil }

// VisibleSQL is the condition that selects the live (not soft-deleted) entries
// a reader may see, for a query that names the memories table as m: space
// entries; agent entries of the reader's own agent; and global entries when the
// reader may see them. It returns the condition and its parameters.
func VisibleSQL(r Reader) (string, []any) {
	cond := "(m.deleted_at = '' AND (m.scope = 'space' OR (m.scope = 'agent' AND m.agent <> '' AND m.agent = ?)"
	if r.Global {
		cond += " OR m.scope = 'global'"
	}
	return cond + "))", []any{r.Agent}
}
