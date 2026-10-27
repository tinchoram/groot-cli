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
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/tinchoram/groot-cli/internal/api"
	"github.com/tinchoram/groot-cli/internal/audit"
	"github.com/tinchoram/groot-cli/internal/compact"
	"github.com/tinchoram/groot-cli/internal/gate"
	"github.com/tinchoram/groot-cli/internal/guard"
	"github.com/tinchoram/groot-cli/internal/memory"
	"github.com/tinchoram/groot-cli/internal/provider"
	"github.com/tinchoram/groot-cli/internal/tool"
)

// ErrMaxTurns is returned when the model keeps requesting tools past MaxTurns
// and the final answer requested afterwards fails.
var ErrMaxTurns = errors.New("agent reached max turns without a final answer")

const (
	defaultMaxTurns = 40
	deniedMessage   = "denied by user"
	// defaultEmergencyKeep matches the default compaction.keep.
	defaultEmergencyKeep = 40
	// contextWarnPercent is the context fill from which strategy none warns.
	contextWarnPercent = 90
)

// Agent is one conversation with a model, with tools and persistence.
//
// The message history, the session id and the turn counter are guarded by an
// internal mutex, so Transcript, SessionID, Compact, Reset and End may run while
// Send is in flight (for example from the UI or a signal handler). Send itself
// must not run twice at once; the UI serializes prompts. The lock is never held
// across a provider call, so readers are not blocked by a slow model.
type Agent struct {
	Name string
	// Space is the space the agent works in; the audit records carry it.
	Space     string
	Provider  provider.Provider
	Tools     *tool.Registry
	Gate      gate.Gate
	Compactor compact.Strategy
	Memory    memory.Store
	Audit     *audit.Log
	// Guard wraps and scans untrusted tool results and remembers the nonces of
	// its envelopes. Nil, or an off guard, leaves results as they are.
	Guard *guard.Guard
	// Effort is the agent's effort for every model call. Empty leaves the
	// provider's value in place.
	Effort   string
	System   string
	MaxTurns int
	// turnTainted is set when a tool of the current turn returned untrusted
	// output. Send clears it at the start of every turn.
	turnTainted atomic.Bool
	// parentID is the session a /clear detached; the next session names it as
	// its parent, and then it is forgotten. Guarded by mu.
	parentID string

	// OnText receives streamed model text as it arrives. May be nil.
	OnText func(delta string)
	// OnToolCall is called before a tool's gate decision. input is the
	// tool's human-readable preview, not the raw JSON. May be nil.
	OnToolCall func(name, input string)
	// OnToolRisk is called once per tool call that the gate allowed, just before
	// it runs, with whether the call is read-only after the taint check. A denied
	// call is not reported: it is not activity. May be nil.
	OnToolRisk func(name string, readOnly bool)
	// OnWarning receives non-fatal persistence or compaction problems. May be nil.
	OnWarning func(msg string)
	// Limit is checked before each model call of a turn. A non-nil error ends
	// the turn without calling the model. May be nil.
	Limit func(ctx context.Context) error
	// ContextMaxPercent compacts the history before a model call once the last
	// known context fills this share of the window. Zero turns the trigger off.
	ContextMaxPercent int
	// WindowFor returns the context window of a model in tokens. Nil, or a
	// model it answers 0 for, uses the built-in table.
	WindowFor func(model string) int
	// EmergencyKeep is how many messages the overflow fallback keeps when the
	// strategy is none. Zero means defaultEmergencyKeep.
	EmergencyKeep int

	mu sync.Mutex
	// toolsMu guards Tools, which SetTools may replace while a turn runs.
	toolsMu   sync.Mutex
	history   []api.Message
	sessionID string
	turn      int
	// synthetic are results added by Resume that the next Send stores.
	synthetic []api.Message
	// costBase makes the provider's running total start at the session's own
	// spend: the total at its start, negated, or the stored spend of a resumed
	// session minus the total then. See SessionCost.
	costBase float64
	// contextTokens is the context size of the last model answer.
	// contextCompacted is set once the history was compacted for that size and
	// cleared by the next answer, so one answer compacts at most once.
	// pressureWarned records the one-time warning of strategy none.
	contextTokens    int64
	contextCompacted bool
	pressureWarned   bool
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
	a.turnTainted.Store(false)
	a.ensureSession(ctx)
	a.persistRepairs(ctx)
	a.appendMessage(ctx, api.Message{
		Role:    api.RoleUser,
		Content: []api.Block{{Type: api.BlockText, Text: prompt}},
	}, api.Usage{})

	retried := false // the overflow retry is made once per Send
	for step := 0; step < a.maxTurns(); step++ {
		a.autoCompact(ctx)
		a.pressureCompact(ctx)
		if a.Limit != nil {
			if err := a.Limit(ctx); err != nil {
				return "", err
			}
		}

		// The definitions are read every round, so SetTools takes effect on the next one.
		defs := a.registry().Definitions()
		turn, msgs := a.nextTurn()
		resp, err := a.call(ctx, msgs, defs)
		if err != nil && provider.IsContextOverflow(err) && !retried && a.compactForOverflow(ctx) {
			retried = true
			a.notify("context overflow: compacted history and retried")
			resp, err = a.call(ctx, a.snapshot(), defs)
		}
		if err != nil {
			return "", err
		}
		a.appendWithTurn(ctx, turn, api.Message{Role: api.RoleAssistant, Content: resp.Content}, resp.Usage)
		a.recordContext(ctx, resp.Usage)

		uses := resp.ToolUses()
		if resp.StopReason != api.StopToolUse || len(uses) == 0 {
			return resp.Text(), nil
		}

		results := a.runTools(ctx, uses)
		// The results are stored even when the turn was cancelled, so the store
		// never keeps a call without its answer.
		a.appendMessage(context.WithoutCancel(ctx), api.Message{Role: api.RoleUser, Content: results}, api.Usage{})
	}
	return a.wrapUp(ctx, a.maxTurns())
}

// wrapUp is the last model call of a Send whose turn budget is spent. It asks
// for a final answer with no tool definitions, so the model cannot request
// tools. A failed call returns ErrMaxTurns wrapping the call error.
func (a *Agent) wrapUp(ctx context.Context, budget int) (string, error) {
	if a.Limit != nil {
		if err := a.Limit(ctx); err != nil {
			return "", fmt.Errorf("%w: final answer refused: %w", ErrMaxTurns, err)
		}
	}
	a.notify(fmt.Sprintf("turn budget reached (%d): asking for a final answer; raise max_turns for this agent if it needs more", budget))
	a.appendMessage(ctx, api.Message{
		Role: api.RoleUser,
		Content: []api.Block{{Type: api.BlockText, Text: fmt.Sprintf(
			"Turn budget reached (%d model calls). Answer now with what you have: the findings so far, what you could not finish, and the next step. Do not request tools.", budget)}},
	}, api.Usage{})

	turn, msgs := a.nextTurn()
	resp, err := a.call(ctx, msgs, nil)
	if err != nil {
		return "", fmt.Errorf("%w: final answer: %w", ErrMaxTurns, err)
	}
	a.appendWithTurn(ctx, turn, api.Message{Role: api.RoleAssistant, Content: resp.Content}, resp.Usage)
	a.recordContext(ctx, resp.Usage)
	return resp.Text(), nil
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
	a.resetContextLocked()
}

// resetContextLocked forgets the context of the history just dropped: the
// gauge reads zero until the next answer, and the one-time flags re-arm.
// The caller holds a.mu.
func (a *Agent) resetContextLocked() {
	a.contextTokens = 0
	a.contextCompacted = false
	a.pressureWarned = false
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

// PrependHistory puts msgs in front of the history of the session. It is for
// the messages a resume left out of the context, summarized, before the first
// Send of the session.
func (a *Agent) PrependHistory(msgs []api.Message) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.history = append(append([]api.Message(nil), msgs...), a.history...)
}

// Detach ends the current conversation in memory, for /clear. It returns the
// old session id, its transcript and its message count, and leaves the agent
// without a session, so the next Send opens a fresh one. The stored rows are
// untouched.
func (a *Agent) Detach() (id, transcript string, messages int) {
	a.mu.Lock()
	defer a.mu.Unlock()
	id, transcript, messages = a.sessionID, api.RenderTranscript(a.history), len(a.history)
	a.parentID = id
	a.sessionID, a.history, a.turn, a.costBase = "", nil, 0, 0
	a.resetContextLocked()
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

// TurnTainted reports whether the current turn has read untrusted tool output,
// such as a fetched page or a remembered entry. Memory saved in such a turn is
// stored as tainted.
func (a *Agent) TurnTainted() bool { return a.turnTainted.Load() }

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
	if u, ok := t.(tool.Untrusted); ok && u.Untrusted() {
		a.turnTainted.Store(true)
	}

	risk := t.RiskFor(input)
	preview := t.Preview(input)
	detail := preview
	// A call that copies a nonce from a recent envelope carries untrusted text.
	// Its preview says so, and a read-only call asks instead of running unasked.
	tainted := a.Guard.Tainted(input)
	if tainted {
		detail = guard.Tag + "\n" + preview
		if risk == tool.RiskRead {
			risk = tool.RiskExec
		}
	}
	if a.OnToolCall != nil {
		a.OnToolCall(use.Name, detail)
	}

	_, external := t.(tool.External)
	destructive, destructiveReason := destructiveOf(t, input)
	req := gate.Request{
		Tool: use.Name, Input: input, Risk: risk, Detail: detail, Tainted: tainted, External: external,
		Destructive: destructive, DestructiveReason: destructiveReason,
	}
	decision := a.gate().Decide(ctx, req)
	if decision == gate.Deny {
		a.audit(use.Name, risk, decision, input, true, 0, "", nil)
		return api.Block{Type: api.BlockToolResult, ToolUseID: use.ID, Content: a.denial(req), IsError: true}
	}
	if a.OnToolRisk != nil {
		a.OnToolRisk(use.Name, risk == tool.RiskRead)
	}

	start := time.Now()
	out, isErr := t.Execute(ctx, input)
	out, kinds, withheld := a.guardOutput(t, use.Name, use.Name+" "+preview, out)
	isErr = isErr || withheld
	a.audit(use.Name, risk, decision, input, isErr, time.Since(start), sandboxOf(t), kinds)
	return api.Block{Type: api.BlockToolResult, ToolUseID: use.ID, Content: out, IsError: isErr}
}

// destructiveOf reports whether a tool call carries a destructive command, and
// names it as "kind: command", for example "git: git push --force".
func destructiveOf(t tool.Tool, input string) (bool, string) {
	d, ok := t.(tool.Destructive)
	if !ok {
		return false, ""
	}
	kind, reason, bad := d.DestructiveFor(input)
	if !bad {
		return false, ""
	}
	return true, string(kind) + ": " + reason
}

// sandboxOf returns the sandbox backend of a tool that runs processes, or "".
func sandboxOf(t tool.Tool) string {
	if s, ok := t.(interface{ SandboxName() string }); ok {
		return s.SandboxName()
	}
	return ""
}

// guardOutput envelopes and scans what an untrusted tool returned. It gives back
// the text for the model, the kinds of any findings, and whether the result was
// withheld. Strict mode withholds fetch_url results that have findings.
func (a *Agent) guardOutput(t tool.Tool, name, source, out string) (string, []string, bool) {
	u, ok := t.(tool.Untrusted)
	if !ok || !u.Untrusted() || !a.Guard.Enabled() {
		return out, nil, false
	}
	clean, found := guard.Sanitize(out)
	found = append(found, guard.Scan(clean)...)
	selfWrapped := false
	if se, ok := t.(tool.SelfEnveloped); ok {
		selfWrapped = se.SelfEnveloped()
	}
	wrap := func(s string) string {
		if selfWrapped {
			return s
		}
		return a.Guard.Wrap(source, s)
	}
	if len(found) == 0 {
		return wrap(clean), nil, false
	}
	kinds := guard.Kinds(found)
	a.notify(fmt.Sprintf("guard: %s returned text that reads like instructions to the model (%s)", name, strings.Join(kinds, ", ")))
	if a.Guard.Mode() == guard.Strict && name == "fetch_url" {
		return fmt.Sprintf("[guard] withheld: %s returned text that reads like instructions to the model (%s). The content was not shown; tell the user what was found and ask how to proceed.", name, strings.Join(kinds, ", ")), kinds, true
	}
	return wrap(clean) + "\n" + guard.Summary(found), kinds, false
}

func (a *Agent) audit(name string, risk tool.Risk, decision gate.Decision, input string, isErr bool, d time.Duration, sandbox string, kinds []string) {
	if a.Audit == nil {
		return
	}
	err := a.Audit.Write(audit.Record{
		Time:         time.Now().UTC(),
		Space:        a.Space,
		Agent:        a.Name,
		Session:      a.SessionID(),
		Tool:         name,
		Risk:         risk.String(),
		Decision:     decision.String(),
		InputPreview: input,
		IsError:      isErr,
		DurationMS:   d.Milliseconds(),
		Sandbox:      sandbox,
		Guard:        kinds,
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
	id, parent := a.sessionID, a.parentID
	a.parentID = ""
	a.mu.Unlock()

	a.warn(a.store().StartSession(ctx, memory.Session{
		ID:        id,
		ParentID:  parent,
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

// call makes one model call over msgs.
func (a *Agent) call(ctx context.Context, msgs []api.Message, defs []api.ToolDef) (api.Response, error) {
	return a.Provider.Send(ctx, provider.Request{
		System:   a.System,
		Messages: msgs,
		Tools:    defs,
		OnText:   a.OnText,
		Effort:   a.Effort,
	})
}

// ContextTokens is the context size of the last model answer: the prompt tokens
// it read, cached or not. Zero before the first answer.
func (a *Agent) ContextTokens() int64 {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.contextTokens
}

// ContextWindow is the context window of the model the next call uses. An open
// task's route model is the provider's model, so it is the one measured.
func (a *Agent) ContextWindow() int {
	if a.Provider == nil {
		return provider.DefaultContextWindow
	}
	return a.windowOf(a.Provider.Model())
}

func (a *Agent) windowOf(model string) int {
	if a.WindowFor != nil {
		if w := a.WindowFor(model); w > 0 {
			return w
		}
	}
	return provider.ContextWindow(model, nil)
}

// RestoreContext sets the context size of a resumed session, read from the
// store, so the gauge shows it before the next turn.
func (a *Agent) RestoreContext(tokens int64) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.contextTokens = tokens
	a.contextCompacted = false
}

// recordContext keeps the context size of the answer just received. It is
// stored with the session and arms the token trigger for the next call.
func (a *Agent) recordContext(ctx context.Context, u api.Usage) {
	tokens := u.InputTokens + u.CacheCreationTokens + u.CacheReadTokens
	if tokens <= 0 {
		return
	}
	a.mu.Lock()
	a.contextTokens = tokens
	a.contextCompacted = false
	id := a.sessionID
	a.mu.Unlock()
	if id == "" {
		return
	}
	a.warn(a.store().SetContextTokens(context.WithoutCancel(ctx), id, tokens))
}

// compactsHistory reports whether a compaction strategy is configured.
func (a *Agent) compactsHistory() bool {
	if a.Compactor == nil {
		return false
	}
	_, none := a.Compactor.(compact.None)
	return !none
}

// pressureCompact runs the strategy before a model call when the last known
// context fills ContextMaxPercent of the window, even if the message-count rule
// has not fired. Under strategy none it only warns, once, at 90 percent.
func (a *Agent) pressureCompact(ctx context.Context) {
	// The window is read before the lock: WindowFor may take other locks.
	window := a.ContextWindow()
	a.mu.Lock()
	tokens, done, warned := a.contextTokens, a.contextCompacted, a.pressureWarned
	a.mu.Unlock()
	if tokens <= 0 || window <= 0 {
		return
	}
	pct := provider.ContextPercent(tokens, window)

	if !a.compactsHistory() {
		if pct >= contextWarnPercent && !warned {
			a.mu.Lock()
			a.pressureWarned = true
			a.mu.Unlock()
			a.notify(fmt.Sprintf("context at %d%%, set compaction.strategy to window or summarize", pct))
		}
		return
	}
	if a.ContextMaxPercent <= 0 || pct < a.ContextMaxPercent || done {
		return
	}

	a.mu.Lock()
	compacted, err := compact.Shrink(ctx, a.Compactor, a.history)
	if err == nil {
		a.history = compacted
	}
	a.contextCompacted = true
	a.mu.Unlock()
	if err != nil {
		a.warn(fmt.Errorf("history compaction skipped: %w", err))
	}
}

// compactForOverflow shortens the history after the provider refused a prompt
// as too long. It uses the configured strategy, or a sliding window of
// EmergencyKeep messages when none is set. It reports whether the history shrank.
func (a *Agent) compactForOverflow(ctx context.Context) bool {
	a.mu.Lock()
	strategy := a.Compactor
	if !a.compactsHistory() {
		strategy = compact.SlidingWindow{Keep: a.emergencyKeep()}
	}
	before := len(a.history)
	compacted, err := compact.Shrink(ctx, strategy, a.history)
	shrank := err == nil && len(compacted) < before
	if shrank {
		a.history = compacted
		a.contextCompacted = true
	}
	a.mu.Unlock()
	if err != nil {
		a.warn(fmt.Errorf("history compaction skipped: %w", err))
	}
	return shrank
}

func (a *Agent) emergencyKeep() int {
	if a.EmergencyKeep > 0 {
		return a.EmergencyKeep
	}
	return defaultEmergencyKeep
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
	if err != nil {
		a.notify(err.Error())
	}
}

// notify sends a message to the transcript through OnWarning.
func (a *Agent) notify(msg string) {
	if a.OnWarning != nil {
		a.OnWarning(msg)
	}
}

func (a *Agent) maxTurns() int {
	if a.MaxTurns <= 0 {
		return defaultMaxTurns
	}
	return a.MaxTurns
}

// registry returns the current tool registry, creating an empty one on first use.
func (a *Agent) registry() *tool.Registry {
	a.toolsMu.Lock()
	defer a.toolsMu.Unlock()
	if a.Tools == nil {
		a.Tools = tool.NewRegistry()
	}
	return a.Tools
}

// SetTools replaces the tool registry. The next model call offers the new
// definitions and the next tool call is dispatched through it. A call already
// running keeps the registry it started with.
func (a *Agent) SetTools(r *tool.Registry) {
	a.toolsMu.Lock()
	defer a.toolsMu.Unlock()
	a.Tools = r
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
