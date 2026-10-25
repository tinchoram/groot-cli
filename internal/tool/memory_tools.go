package tool

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/tinchoram/groot-cli/internal/api"
	"github.com/tinchoram/groot-cli/internal/memory"
)

// recallLineRunes caps the text of each line of a recall result. memory_get
// returns the rest.
const recallLineRunes = 160

// MemoryAccess is what the memory tools of one conversation work with.
type MemoryAccess struct {
	// Store is the memory the tools read and write.
	Store memory.Store
	// Reader says who reads: the agent, and whether it may see global entries.
	Reader memory.Reader
	// CanWriteGlobal lets the agent write global entries. Only groot has it.
	CanWriteGlobal bool
	// Tainted reports whether the current turn read untrusted tool output. Entries
	// saved in such a turn are stored as tainted. Nil means never.
	Tainted func() bool
	// Changed runs after a successful change, so the conversation can rebuild
	// its prompt. Nil means nothing to do.
	Changed func()
}

func (a MemoryAccess) tainted() bool { return a.Tainted != nil && a.Tainted() }

func (a MemoryAccess) changed() {
	if a.Changed != nil {
		a.Changed()
	}
}

// Remember stores a fact, decision or runbook through the memory port.
type Remember struct {
	acc MemoryAccess
}

// NewRemember returns the remember tool.
func NewRemember(acc MemoryAccess) *Remember {
	return &Remember{acc: acc}
}

type rememberInput struct {
	Kind     string `json:"kind"`
	Text     string `json:"text"`
	Title    string `json:"title"`
	TopicKey string `json:"topic_key"`
	Scope    string `json:"scope"`
}

func (*Remember) Definition() api.ToolDef {
	return api.ToolDef{
		Name:        "remember",
		Description: "Save a durable fact, decision or runbook step to long-term memory for future sessions. Saving the same topic_key revises that entry; saving the same content again is counted, not stored twice. Scope: agent (only you), space (every agent of this space; the default), or global (groot only).",
		InputSchema: json.RawMessage(`{"type":"object","properties":{"kind":{"type":"string","enum":["fact","decision","runbook"]},"text":{"type":"string"},"title":{"type":"string"},"topic_key":{"type":"string"},"scope":{"type":"string","enum":["agent","space","global"]}},"required":["kind","text"]}`),
	}
}

// Risk is RiskRead: the tool only writes the local memory database that the
// user already configured, and it cannot touch the infrastructure.
func (*Remember) Risk() Risk { return RiskRead }

func (r *Remember) RiskFor(string) Risk { return r.Risk() }

func (*Remember) Preview(input string) string {
	var in rememberInput
	if err := json.Unmarshal([]byte(input), &in); err != nil {
		return "remember: invalid input"
	}
	return Redact(fmt.Sprintf("remember [%s] %s", in.Kind, in.Text))
}

func (r *Remember) Execute(ctx context.Context, input string) (string, bool) {
	out, isErr := r.execute(ctx, input)
	return Redact(out), isErr
}

func (r *Remember) execute(ctx context.Context, input string) (string, bool) {
	var in rememberInput
	if err := json.Unmarshal([]byte(input), &in); err != nil {
		return "invalid input: " + err.Error(), true
	}
	kind := memory.Kind(in.Kind)
	if !kind.Valid() || kind == memory.KindSessionSummary {
		return fmt.Sprintf("kind %q is not allowed; use fact, decision or runbook", in.Kind), true
	}
	scope, ok := parseScope(in.Scope)
	if !ok {
		return fmt.Sprintf("scope %q is not valid; use agent, space or global", in.Scope), true
	}
	if scope == memory.ScopeGlobal && !r.acc.CanWriteGlobal {
		return "only groot can write global memory; save with scope space or agent instead", true
	}
	if strings.TrimSpace(in.Text) == "" {
		return "text is required", true
	}
	res, err := r.acc.Store.Remember(ctx, memory.Entry{
		Kind: kind, Scope: scope, Agent: r.acc.Reader.Agent, Title: in.Title,
		TopicKey: in.TopicKey, Text: in.Text, Source: "tool:remember", Tainted: r.acc.tainted(),
	})
	if err != nil {
		return err.Error(), true
	}
	r.acc.changed()
	switch res.Outcome {
	case memory.OutcomeDuplicate:
		return fmt.Sprintf("duplicate of #%d", res.ID), false
	case memory.OutcomeUpdated:
		revision := 0
		if e, err := r.acc.Store.Get(ctx, res.ID, r.acc.Reader); err == nil {
			revision = e.RevisionCount
		}
		return fmt.Sprintf("updated #%d (revision %d)", res.ID, revision), false
	}
	return fmt.Sprintf("remembered #%d (created)", res.ID), false
}

// Recall searches the long-term memory an agent may read.
type Recall struct {
	acc MemoryAccess
}

// NewRecall returns the recall tool.
func NewRecall(acc MemoryAccess) *Recall {
	return &Recall{acc: acc}
}

type recallInput struct {
	Query string `json:"query"`
	Limit int    `json:"limit"`
}

func (*Recall) Definition() api.ToolDef {
	return api.ToolDef{
		Name:        "recall",
		Description: "Search long-term memory for facts, decisions and runbooks saved earlier. Returns short lines with ids; use memory_get for the full text of one.",
		InputSchema: json.RawMessage(`{"type":"object","properties":{"query":{"type":"string"},"limit":{"type":"integer","minimum":1,"maximum":50}},"required":["query"]}`),
	}
}

func (*Recall) Risk() Risk { return RiskRead }

// Untrusted: remembered entries may have been saved from external content.
func (*Recall) Untrusted() bool { return true }

func (r *Recall) RiskFor(string) Risk { return r.Risk() }

func (*Recall) Preview(input string) string {
	var in recallInput
	if err := json.Unmarshal([]byte(input), &in); err != nil {
		return "recall: invalid input"
	}
	return Redact("recall " + in.Query)
}

func (r *Recall) Execute(ctx context.Context, input string) (string, bool) {
	out, isErr := r.execute(ctx, input)
	return Redact(out), isErr
}

func (r *Recall) execute(ctx context.Context, input string) (string, bool) {
	var in recallInput
	if err := json.Unmarshal([]byte(input), &in); err != nil {
		return "invalid input: " + err.Error(), true
	}
	if in.Limit <= 0 || in.Limit > 50 {
		in.Limit = 10
	}
	entries, err := r.acc.Store.Recall(ctx, r.acc.Reader, in.Query, in.Limit)
	if err != nil {
		return err.Error(), true
	}
	if len(entries) == 0 {
		return "no memories match", false
	}
	lines := make([]string, 0, len(entries)+1)
	for _, e := range entries {
		lines = append(lines, memoryLine(e))
	}
	lines = append(lines, "use memory_get for the full text")
	return strings.Join(lines, "\n"), false
}

// memoryLine is "#<id> [kind|scope] title — text", with the whole line cut to
// recallLineRunes runes.
func memoryLine(e memory.Entry) string {
	body := e.Text
	if e.Title != "" {
		body = e.Title + " — " + e.Text
	}
	body = strings.Join(strings.Fields(body), " ")
	if n := utf8.RuneCountInString(body); n > recallLineRunes {
		body = string([]rune(body)[:recallLineRunes]) + "…"
	}
	return fmt.Sprintf("#%d [%s|%s] %s", e.ID, e.Kind, e.Scope, body)
}

// MemoryGet returns one memory entry with its full text.
type MemoryGet struct {
	acc MemoryAccess
}

// NewMemoryGet returns the memory_get tool.
func NewMemoryGet(acc MemoryAccess) *MemoryGet {
	return &MemoryGet{acc: acc}
}

func (*MemoryGet) Definition() api.ToolDef {
	return api.ToolDef{
		Name:        "memory_get",
		Description: "Read one long-term memory entry in full, by the id that recall or the memory list showed.",
		InputSchema: json.RawMessage(`{"type":"object","properties":{"id":{"type":"integer","minimum":1}},"required":["id"]}`),
	}
}

func (*MemoryGet) Risk() Risk { return RiskRead }

func (m *MemoryGet) RiskFor(string) Risk { return m.Risk() }

// Untrusted: the full text is remembered text, as recall's lines are.
func (*MemoryGet) Untrusted() bool { return true }

func (*MemoryGet) Preview(input string) string {
	var in idInput
	if err := json.Unmarshal([]byte(input), &in); err != nil {
		return "memory_get: invalid input"
	}
	return fmt.Sprintf("memory_get #%d", in.ID)
}

func (m *MemoryGet) Execute(ctx context.Context, input string) (string, bool) {
	var in idInput
	if err := json.Unmarshal([]byte(input), &in); err != nil {
		return "invalid input: " + err.Error(), true
	}
	e, err := m.acc.Store.Get(ctx, in.ID, m.acc.Reader)
	if errors.Is(err, memory.ErrNotFound) {
		return fmt.Sprintf("memory #%d not found", in.ID), true
	}
	if err != nil {
		return Redact(err.Error()), true
	}
	head := fmt.Sprintf("#%d [%s|%s]", e.ID, e.Kind, e.Scope)
	if e.Title != "" {
		head += " " + e.Title
	}
	return Redact(head + "\n" + e.Text), false
}

// Forget soft-deletes a memory entry.
type Forget struct {
	acc MemoryAccess
}

// NewForget returns the forget tool.
func NewForget(acc MemoryAccess) *Forget {
	return &Forget{acc: acc}
}

type idInput struct {
	ID int64 `json:"id"`
}

func (*Forget) Definition() api.ToolDef {
	return api.ToolDef{
		Name:        "forget",
		Description: "Remove one long-term memory entry so it no longer appears in recall or in the prompt.",
		InputSchema: json.RawMessage(`{"type":"object","properties":{"id":{"type":"integer","minimum":1}},"required":["id"]}`),
	}
}

func (*Forget) Risk() Risk { return RiskWrite }

func (f *Forget) RiskFor(string) Risk { return f.Risk() }

func (*Forget) Preview(input string) string {
	var in idInput
	if err := json.Unmarshal([]byte(input), &in); err != nil {
		return "forget: invalid input"
	}
	return fmt.Sprintf("forget memory #%d", in.ID)
}

func (f *Forget) Execute(ctx context.Context, input string) (string, bool) {
	var in idInput
	if err := json.Unmarshal([]byte(input), &in); err != nil {
		return "invalid input: " + err.Error(), true
	}
	if err := f.acc.Store.Forget(ctx, in.ID, f.acc.Reader); err != nil {
		return memoryError(in.ID, err), true
	}
	f.acc.changed()
	return fmt.Sprintf("forgot #%d", in.ID), false
}

// UpdateMemory changes the title, text, topic key or pin of a memory entry.
type UpdateMemory struct {
	acc MemoryAccess
}

// NewUpdateMemory returns the update_memory tool.
func NewUpdateMemory(acc MemoryAccess) *UpdateMemory {
	return &UpdateMemory{acc: acc}
}

type updateMemoryInput struct {
	ID       int64   `json:"id"`
	Title    *string `json:"title"`
	Text     *string `json:"text"`
	TopicKey *string `json:"topic_key"`
	Pinned   *bool   `json:"pinned"`
}

func (*UpdateMemory) Definition() api.ToolDef {
	return api.ToolDef{
		Name:        "update_memory",
		Description: "Change one long-term memory entry. Give only the fields to change: title, text, topic_key or pinned.",
		InputSchema: json.RawMessage(`{"type":"object","properties":{"id":{"type":"integer","minimum":1},"title":{"type":"string"},"text":{"type":"string"},"topic_key":{"type":"string"},"pinned":{"type":"boolean"}},"required":["id"]}`),
	}
}

func (*UpdateMemory) Risk() Risk { return RiskWrite }

func (u *UpdateMemory) RiskFor(string) Risk { return u.Risk() }

func (*UpdateMemory) Preview(input string) string {
	var in updateMemoryInput
	if err := json.Unmarshal([]byte(input), &in); err != nil {
		return "update_memory: invalid input"
	}
	return fmt.Sprintf("update memory #%d", in.ID)
}

func (u *UpdateMemory) Execute(ctx context.Context, input string) (string, bool) {
	var in updateMemoryInput
	if err := json.Unmarshal([]byte(input), &in); err != nil {
		return "invalid input: " + err.Error(), true
	}
	p := memory.Patch{Title: in.Title, Text: in.Text, TopicKey: in.TopicKey, Pinned: in.Pinned}
	if p.Title == nil && p.Text == nil && p.TopicKey == nil && p.Pinned == nil {
		return "nothing to update: give title, text, topic_key or pinned", true
	}
	if err := u.acc.Store.Update(ctx, in.ID, u.acc.Reader, p); err != nil {
		return memoryError(in.ID, err), true
	}
	u.acc.changed()
	return fmt.Sprintf("updated #%d", in.ID), false
}

// memoryError names the entry in a store error; a missing entry is the usual one.
func memoryError(id int64, err error) string {
	if errors.Is(err, memory.ErrNotFound) {
		return fmt.Sprintf("memory #%d not found", id)
	}
	return Redact(err.Error())
}

// parseScope maps the scope named by the model onto the store's scopes. An
// empty scope is space, the shared default.
func parseScope(name string) (memory.Scope, bool) {
	switch name {
	case "":
		return memory.ScopeSpace, true
	case "agent":
		return memory.ScopeAgent, true
	case "space":
		return memory.ScopeSpace, true
	case "global":
		return memory.ScopeGlobal, true
	}
	return "", false
}
