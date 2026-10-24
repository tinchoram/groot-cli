// Package agent runs the conversation loop. It knows only the ports: the
// provider, the tool registry, the gate, the compactor, the memory store and
// the audit log.
package agent

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"slices"
	"sync"
	"time"

	"github.com/tinchoram/groot-cli/internal/api"
	"github.com/tinchoram/groot-cli/internal/audit"
	"github.com/tinchoram/groot-cli/internal/compact"
	"github.com/tinchoram/groot-cli/internal/gate"
	"github.com/tinchoram/groot-cli/internal/memory"
	"github.com/tinchoram/groot-cli/internal/provider"
	"github.com/tinchoram/groot-cli/internal/tool"
)

// ErrMaxTurns is returned when the model keeps requesting tools past MaxTurns.
var ErrMaxTurns = errors.New("agent reached max turns without a final answer")

const (
	defaultMaxTurns = 20
	deniedMessage   = "denied by user"
)

// Agent is one conversation with a model, with tools and persistence.
//
// The message history, the session id and the turn counter are guarded by an
// internal mutex, so Transcript, SessionID, Compact, Reset and End may run while
// Send is in flight (for example from the UI or a signal handler). Send itself
// must not run twice at once; the UI serializes prompts. The lock is never held
// across a provider call, so readers are not blocked by a slow model.
type Agent struct {
	Name      string
	Provider  provider.Provider
	Tools     *tool.Registry
	Gate      gate.Gate
	Compactor compact.Strategy
	Memory    memory.Store
	Audit     *audit.Log
	System    string
	MaxTurns  int

	// OnText receives streamed model text as it arrives. May be nil.
	OnText func(delta string)
	// OnToolCall is called before a tool's gate decision. input is the
	// tool's human-readable preview, not the raw JSON. May be nil.
	OnToolCall func(name, input string)
	// OnWarning receives non-fatal persistence or compaction problems. May be nil.
	OnWarning func(msg string)
	// Limit is checked before each model call of a turn. A non-nil error ends
	// the turn without calling the model. May be nil.
	Limit func(ctx context.Context) error

	mu        sync.Mutex
	history   []api.Message
	sessionID string
	turn      int
	// synthetic are results added by Resume that the next Send stores.
	synthetic []api.Message
	// costBase makes the provider's running total start at the session's own
	// spend: the total at its start, negated, or the stored spend of a resumed
	// session minus the total then. See SessionCost.
	costBase float64
}

// SessionID returns the current session id, or "" before Start or the first Send.
func (a *Agent) SessionID() string {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.sessionID
}

// Start opens the persisted session if it is not open yet. It is idempotent.
func (a *Agent) Start(ctx context.Context) {
	a.ensureSession(ctx)
}

// Transcript renders the current in-memory history as plain text. It works on
// a snapshot taken under the lock, so it is safe during Send.
func (a *Agent) Transcript() string {
	return api.RenderTranscript(a.snapshot())
}

// Send appends prompt to the conversation, runs the model and any requested
// tools until the model stops asking for tools, and returns the final text.
func (a *Agent) Send(ctx context.Context, prompt string) (string, error) {
	a.ensureSession(ctx)
	a.persistRepairs(ctx)
	a.appendMessage(ctx, api.Message{
		Role:    api.RoleUser,
		Content: []api.Block{{Type: api.BlockText, Text: prompt}},
	}, api.Usage{})

	defs := a.registry().Definitions()
	for step := 0; step < a.maxTurns(); step++ {
		a.autoCompact(ctx)
		if a.Limit != nil {
			if err := a.Limit(ctx); err != nil {
				return "", err
			}
		}

		turn, msgs := a.nextTurn()
		resp, err := a.Provider.Send(ctx, provider.Request{
			System:   a.System,
			Messages: msgs,
			Tools:    defs,
			OnText:   a.OnText,
		})
		if err != nil {
			return "", err
		}
		a.appendWithTurn(ctx, turn, api.Message{Role: api.RoleAssistant, Content: resp.Content}, resp.Usage)

		uses := resp.ToolUses()
		if resp.StopReason != api.StopToolUse || len(uses) == 0 {
			return resp.Text(), nil
		}

		results := a.runTools(ctx, uses)
		// The results are stored even when the turn was cancelled, so the store
		// never keeps a call without its answer.
		a.appendMessage(context.WithoutCancel(ctx), api.Message{Role: api.RoleUser, Content: results}, api.Usage{})
	}
	return "", ErrMaxTurns
}

// Compact applies the configured strategy to the history now, instead of
// waiting for the next Send. It is a no-op when no strategy is set. Strategies
// that call the model (summarize) run under the lock, so Transcript waits for
// them to finish.
func (a *Agent) Compact(ctx context.Context) error {
	if a.Compactor == nil {
		return nil
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	compacted, err := a.Compactor.Compact(ctx, a.history)
	if err != nil {
		return fmt.Errorf("compact history: %w", err)
	}
	a.history = compacted
	return nil
}

// Reset clears the in-memory history. The session id and persisted rows are kept.
func (a *Agent) Reset() {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.history = nil
}

// Resume continues an existing session: the agent keeps its id, starts from the
// given history and continues numbering turns after turns. No session is opened.
// costUSD is the spend already stored for the session.
func (a *Agent) Resume(id string, history []api.Message, turns int, costUSD float64) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.sessionID = id
	a.costBase = costUSD - a.CostUSD()
	// A dangling tool_use in the stored history gets its cancelled result now;
	// the next Send stores it, ahead of the new prompt.
	repaired, inserted, _ := api.Repair(history)
	a.history = repaired
	for _, i := range inserted {
		if i == len(repaired)-1 {
			a.synthetic = append(a.synthetic, repaired[i])
		}
	}
	a.turn = turns
}

// Detach ends the current conversation in memory, for /clear. It returns the
// old session id, its transcript and its message count, and leaves the agent
// without a session, so the next Send opens a fresh one. The stored rows are
// untouched.
func (a *Agent) Detach() (id, transcript string, messages int) {
	a.mu.Lock()
	defer a.mu.Unlock()
	id, transcript, messages = a.sessionID, api.RenderTranscript(a.history), len(a.history)
	a.sessionID, a.history, a.turn, a.costBase = "", nil, 0, 0
	return id, transcript, messages
}

// Usage returns accumulated token usage when the provider reports it.
func (a *Agent) Usage() api.Usage {
	if r, ok := a.Provider.(provider.UsageReporter); ok {
		return r.Usage()
	}
	return api.Usage{}
}

// CostUSD returns the estimated session cost when the provider reports it.
func (a *Agent) CostUSD() float64 {
	if r, ok := a.Provider.(provider.UsageReporter); ok {
		return r.EstimatedCostUSD()
	}
	return 0
}

// SessionCost returns the spend of the current session: what it had already
// cost when it was resumed, plus what the provider has spent since.
func (a *Agent) SessionCost() float64 {
	a.mu.Lock()
	base := a.costBase
	a.mu.Unlock()
	return base + a.CostUSD()
}

// End marks the session finished in memory, recording its cost.
func (a *Agent) End(ctx context.Context) error {
	id := a.SessionID()
	if id == "" {
		return nil
	}
	return a.store().EndSession(ctx, id, a.SessionCost())
}

// persistCost stores the spend of the current session. It runs after every
// model answer, so a crash loses at most the answer in flight.
func (a *Agent) persistCost(ctx context.Context) {
	a.mu.Lock()
	id := a.sessionID
	a.mu.Unlock()
	if id == "" {
		return
	}
	a.warn(a.store().SetSessionCost(context.WithoutCancel(ctx), id, a.SessionCost()))
}

func (a *Agent) runTool(ctx context.Context, use api.Block) api.Block {
	input := string(use.Input)
	t, ok := a.registry().Get(use.Name)
	if !ok {
		return api.Block{
			Type:      api.BlockToolResult,
			ToolUseID: use.ID,
			Content:   fmt.Sprintf("unknown tool %q", use.Name),
			IsError:   true,
		}
	}

	risk := t.RiskFor(input)
	detail := t.Preview(input)
	if a.OnToolCall != nil {
		a.OnToolCall(use.Name, detail)
	}

	req := gate.Request{Tool: use.Name, Input: input, Risk: risk, Detail: detail}
	decision := a.gate().Decide(ctx, req)
	if decision == gate.Deny {
		a.audit(use.Name, risk, decision, input, true, 0)
		return api.Block{Type: api.BlockToolResult, ToolUseID: use.ID, Content: a.denial(req), IsError: true}
	}

	start := time.Now()
	out, isErr := t.Execute(ctx, input)
	a.audit(use.Name, risk, decision, input, isErr, time.Since(start))
	return api.Block{Type: api.BlockToolResult, ToolUseID: use.ID, Content: out, IsError: isErr}
}

func (a *Agent) audit(name string, risk tool.Risk, decision gate.Decision, input string, isErr bool, d time.Duration) {
	if a.Audit == nil {
		return
	}
	err := a.Audit.Write(audit.Record{
		Time:         time.Now().UTC(),
		Session:      a.SessionID(),
		Tool:         name,
		Risk:         risk.String(),
		Decision:     decision.String(),
		InputPreview: input,
		IsError:      isErr,
		DurationMS:   d.Milliseconds(),
	})
	a.warn(err)
}

// ensureSession opens the session once. The id is set under the lock; the
// store call happens outside it.
func (a *Agent) ensureSession(ctx context.Context) {
	a.mu.Lock()
	if a.sessionID != "" {
		a.mu.Unlock()
		return
	}
	a.sessionID = newSessionID()
	a.costBase = -a.CostUSD()
	id := a.sessionID
	a.mu.Unlock()

	a.warn(a.store().StartSession(ctx, memory.Session{
		ID:        id,
		Agent:     a.Name,
		Model:     a.Provider.Model(),
		StartedAt: time.Now().UTC(),
	}))
}

// persistRepairs repairs the history in memory and stores the synthetic results
// that belong at its end: those queued by Resume and any the repair adds there.
// They are stored before the prompt of the turn about to start, so the stored
// order matches the history. Repairs in the middle of the history stay in
// memory, because the store only appends.
func (a *Agent) persistRepairs(ctx context.Context) {
	a.mu.Lock()
	before := len(a.history)
	repaired, inserted, changed := api.Repair(a.history)
	if changed {
		a.history = repaired
	}
	pending := a.synthetic
	a.synthetic = nil
	for _, i := range inserted {
		if i == len(repaired)-1 && i >= before {
			pending = append(pending, repaired[i])
		}
	}
	id, turn := a.sessionID, a.turn
	a.mu.Unlock()
	for _, m := range pending {
		a.warn(a.store().AppendMessage(ctx, id, turn, m, api.Usage{}))
	}
}

// runTools runs the calls of one model response in order. Once the context is
// cancelled, the calls that have not started get cancelled results, so every
// tool_use is answered and the history stays consistent.
func (a *Agent) runTools(ctx context.Context, uses []api.Block) []api.Block {
	results := make([]api.Block, 0, len(uses))
	for i, use := range uses {
		if ctx.Err() != nil {
			for _, rest := range uses[i:] {
				results = append(results, api.CancelledResult(rest.ID))
			}
			break
		}
		results = append(results, a.runToolSafe(ctx, use))
	}
	return results
}

// runToolSafe turns a panic in a tool into an error result, so one broken tool
// cannot leave its call without an answer.
func (a *Agent) runToolSafe(ctx context.Context, use api.Block) (b api.Block) {
	defer func() {
		if r := recover(); r != nil {
			b = api.Block{Type: api.BlockToolResult, ToolUseID: use.ID,
				Content: fmt.Sprintf("tool %s panicked: %v", use.Name, r), IsError: true}
		}
	}()
	return a.runTool(ctx, use)
}

// appendMessage adds m to the history under the lock, then persists it.
func (a *Agent) appendMessage(ctx context.Context, m api.Message, u api.Usage) {
	a.mu.Lock()
	a.history = append(a.history, m)
	id, turn := a.sessionID, a.turn
	a.mu.Unlock()
	a.warn(a.store().AppendMessage(ctx, id, turn, m, u))
}

// appendWithTurn is appendMessage for a turn number fixed before the model call.
func (a *Agent) appendWithTurn(ctx context.Context, turn int, m api.Message, u api.Usage) {
	a.mu.Lock()
	a.history = append(a.history, m)
	id := a.sessionID
	a.mu.Unlock()
	a.warn(a.store().AppendMessage(ctx, id, turn, m, u))
	a.persistCost(ctx)
}

// nextTurn advances the turn counter and returns it with a copy of the history,
// so the provider never reads a slice that a concurrent append could touch.
func (a *Agent) nextTurn() (int, []api.Message) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.turn++
	// Defensive: a call model needs the history consistent. Storage of any
	// repair happens in persistRepairs, at the start of a Send.
	if repaired, changed := api.RepairHistory(a.history); changed {
		a.history = repaired
	}
	return a.turn, slices.Clone(a.history)
}

// snapshot returns a copy of the history taken under the lock.
func (a *Agent) snapshot() []api.Message {
	a.mu.Lock()
	defer a.mu.Unlock()
	return slices.Clone(a.history)
}

// autoCompact runs the strategy before a model call. A failure is reported as
// a warning and the history is left unchanged.
func (a *Agent) autoCompact(ctx context.Context) {
	if a.Compactor == nil {
		return
	}
	a.mu.Lock()
	compacted, err := a.Compactor.Compact(ctx, a.history)
	if err == nil {
		a.history = compacted
	}
	a.mu.Unlock()
	if err != nil {
		a.warn(fmt.Errorf("history compaction skipped: %w", err))
	}
}

// denial is the tool result for a denied call: the gate's reason when it gives
// one, otherwise the plain refusal of the user.
func (a *Agent) denial(req gate.Request) string {
	if e, ok := a.gate().(gate.Explainer); ok {
		if reason := e.Explain(req); reason != "" {
			return reason
		}
	}
	return deniedMessage
}

func (a *Agent) warn(err error) {
	if err != nil && a.OnWarning != nil {
		a.OnWarning(err.Error())
	}
}

func (a *Agent) maxTurns() int {
	if a.MaxTurns <= 0 {
		return defaultMaxTurns
	}
	return a.MaxTurns
}

func (a *Agent) registry() *tool.Registry {
	if a.Tools == nil {
		a.Tools = tool.NewRegistry()
	}
	return a.Tools
}

// gate fails closed: without a configured policy only reads are allowed.
func (a *Agent) gate() gate.Gate {
	if a.Gate == nil {
		return gate.ReadOnly{}
	}
	return a.Gate
}

func (a *Agent) store() memory.Store {
	if a.Memory == nil {
		return memory.NoMemory{}
	}
	return a.Memory
}

func newSessionID() string {
	b := make([]byte, 6)
	if _, err := rand.Read(b); err != nil {
		return fmt.Sprintf("s-%d", time.Now().UnixNano())
	}
	return "s-" + hex.EncodeToString(b)
}

// MessageCount returns the number of messages in the in-memory history.
func (a *Agent) MessageCount() int {
	a.mu.Lock()
	defer a.mu.Unlock()
	return len(a.history)
}
