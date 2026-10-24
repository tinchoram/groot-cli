// Package workspace is the runtime for spaces and agents. It owns the space
// list, the agent catalog, one conversation per (space, agent) pair, and the
// memory and audit handles of each space. The UI talks to it through the Chat
// and Workspace methods and never builds agents itself.
package workspace

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/tinchoram/groot-cli/internal/agent"
	"github.com/tinchoram/groot-cli/internal/agentimport"
	"github.com/tinchoram/groot-cli/internal/api"
	"github.com/tinchoram/groot-cli/internal/audit"
	"github.com/tinchoram/groot-cli/internal/catalog"
	"github.com/tinchoram/groot-cli/internal/compact"
	"github.com/tinchoram/groot-cli/internal/config"
	"github.com/tinchoram/groot-cli/internal/feedback"
	"github.com/tinchoram/groot-cli/internal/flow"
	"github.com/tinchoram/groot-cli/internal/flowgate"
	"github.com/tinchoram/groot-cli/internal/gate"
	"github.com/tinchoram/groot-cli/internal/inbox"
	"github.com/tinchoram/groot-cli/internal/memory"
	"github.com/tinchoram/groot-cli/internal/provider"
	"github.com/tinchoram/groot-cli/internal/space"
)

// Chat is one running conversation as the UI sees it.
type Chat interface {
	Send(ctx context.Context, prompt string) (string, error)
	Model() string
	SetModel(name string) error
	Compact(ctx context.Context) error
	MessageCount() int
	Usage() api.Usage
	CostUSD() float64
	// ContextTokens is the context size of the last model answer; ContextWindow
	// is the window of the model the next call uses.
	ContextTokens() int64
	ContextWindow() int
	Reset()
	SessionID() string
	// Resumed reports the stored session this conversation continued, if any.
	Resumed() (Resumed, bool)
	// Task reports the open task of this conversation, if there is one.
	Task() (flow.Task, bool)
	// TaskStatus describes the classification and the open task.
	TaskStatus(ctx context.Context) string
	// FailTask closes the open task with a reason.
	FailTask(ctx context.Context, reason string) (string, error)
	// WorkMode reports whether work mode is on for this conversation.
	WorkMode() bool
	// SetWorkMode turns work mode on or off for this conversation.
	SetWorkMode(on bool)
	// TaskAccept closes the delivered task.
	TaskAccept(ctx context.Context) (string, error)
	// TaskReject sends the delivered task back to implement with the reason.
	TaskReject(ctx context.Context, reason string) (string, error)
	// TakeReview returns the review of the last work-mode delivery, once.
	TakeReview() string
	// Clear detaches the conversation for /clear; the returned function ends
	// the old session and must run off the UI loop.
	Clear() func(context.Context) error
}

// Hooks receive the output of every conversation, tagged with its key
// ("space/agent"), so that the UI can route it to the right transcript.
// OnConfirm blocks until the user answers; the interactive policy uses it.
type Hooks struct {
	OnText     func(key, delta string)
	OnToolCall func(key, name, input string)
	OnWarning  func(key, msg string)
	OnAuto     func(key, name, input string)
	// OnNotice receives a plain transcript line, such as a message that
	// triggered a turn. When nil, it goes to OnWarning.
	OnNotice func(key, text string)
	// OnAnswer receives the answer of a turn the workspace started itself.
	OnAnswer func(key, text string, err error)
	// OnInbox reports that a message was delivered, so the inbox badges can be re-read.
	OnInbox   func()
	OnConfirm func(key string, req gate.Request) bool
}

// Options configure a Workspace.
type Options struct {
	Home     string
	Version  string // the groot version, written into feedback items
	UserHome string // the user's home directory: the root space and "~" expansion
	Config   config.Config
	// ConfigPath is the configuration file in use; /model --default and
	// set_default_model write provider.model into it. Empty means groot.yaml.
	ConfigPath string
	Spaces     *space.Store
	Gate       gate.Gate
	Hooks      Hooks
	// DisablePublic leaves the public library sources out, so that nothing
	// reaches the network. Tests and offline use set it.
	DisablePublic bool
	NewProvider   func(model string) provider.Provider
	Now           func() time.Time
}

// ImportResult reports an agent that was imported into the catalog.
type ImportResult struct {
	Name     string
	Path     string
	Warnings []string
}

// SpaceInfo describes a space for display.
type SpaceInfo struct {
	Name   string
	Path   string
	Agents []string
	Active bool
}

// AgentInfo describes an agent definition for display.
type AgentInfo struct {
	Name        string
	Description string
	Model       string
	Tools       []string
	Prompt      string
	Assigned    bool // assigned to the active space
	Active      bool // currently selected in the active space
}

// Workspace is the runtime. Methods are safe for concurrent use.
type Workspace struct {
	opts Options

	mu     sync.Mutex
	defs   []catalog.AgentDef
	active string // selected agent in the active space
	convs  map[string]*Conversation
	stores map[string]*spaceStores
	inbox  *inbox.Store               // messages between agents and groot, under the home directory
	work   map[string]*gate.WorkState // work mode per conversation key
	grants map[string]*gate.Grants    // session approvals per conversation key
	// modelCache is the provider's model list, reused for modelCacheTTL.
	modelCache modelCache
	feedback   *feedback.Store // the backlog of bugs and feature requests about groot
}

type spaceStores struct {
	mem *memory.SQLite
	aud *audit.Log
}

// New builds a workspace over an existing home directory.
func New(opts Options) (*Workspace, error) {
	if opts.Spaces == nil {
		return nil, errors.New("workspace: spaces store is required")
	}
	if opts.Gate == nil {
		opts.Gate = gate.ReadOnly{}
	}
	if opts.NewProvider == nil {
		return nil, errors.New("workspace: a provider factory is required")
	}
	if opts.Now == nil {
		opts.Now = time.Now
	}
	fb, err := feedback.Open(filepath.Join(opts.Home, "feedback"))
	if err != nil {
		return nil, err
	}
	ib, err := openInbox(opts.Home)
	if err != nil {
		return nil, err
	}
	w := &Workspace{
		opts:     opts,
		convs:    map[string]*Conversation{},
		stores:   map[string]*spaceStores{},
		inbox:    ib,
		work:     map[string]*gate.WorkState{},
		grants:   map[string]*gate.Grants{},
		feedback: fb,
	}
	if err := w.reload(); err != nil {
		_ = ib.Close()
		return nil, err
	}
	w.active = w.firstAssigned()
	return w, nil
}

func (w *Workspace) reload() error {
	defs, err := catalog.Load(w.opts.Home)
	if err != nil {
		return err
	}
	w.mu.Lock()
	w.defs = defs
	w.mu.Unlock()
	return nil
}

func (w *Workspace) def(name string) (catalog.AgentDef, bool) {
	for _, d := range w.defs {
		if d.Name == name {
			return d, true
		}
	}
	return catalog.AgentDef{}, false
}

// firstAssigned returns the first agent of the active space that exists in
// the catalog, or "".
func (w *Workspace) firstAssigned() string {
	cur, ok := w.opts.Spaces.Current()
	if !ok {
		return ""
	}
	for _, a := range cur.Agents {
		if _, ok := w.def(a); ok {
			return a
		}
	}
	return ""
}

func toSpaceInfo(sp space.Space, active bool) SpaceInfo {
	return SpaceInfo{Name: sp.Name, Path: sp.Path, Agents: sp.Agents, Active: active}
}

// Spaces lists every space; the active one is flagged.
func (w *Workspace) Spaces() []SpaceInfo {
	cur, _ := w.opts.Spaces.Current()
	var out []SpaceInfo
	for _, sp := range w.opts.Spaces.List() {
		out = append(out, toSpaceInfo(sp, sp.Name == cur.Name))
	}
	return out
}

// ActiveSpace returns the active space.
func (w *Workspace) ActiveSpace() (SpaceInfo, bool) {
	cur, ok := w.opts.Spaces.Current()
	if !ok {
		return SpaceInfo{}, false
	}
	return toSpaceInfo(cur, true), true
}

// ActiveAgent returns the selected agent's name, or "".
func (w *Workspace) ActiveAgent() string {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.active
}

// ConversationKey identifies the active conversation as "space/agent", or "".
func (w *Workspace) ConversationKey() string {
	cur, ok := w.opts.Spaces.Current()
	w.mu.Lock()
	defer w.mu.Unlock()
	if !ok || w.active == "" {
		return ""
	}
	return cur.Name + "/" + w.active
}

// Agents lists the agents assigned to the active space.
func (w *Workspace) Agents() []AgentInfo {
	cur, ok := w.opts.Spaces.Current()
	if !ok {
		return nil
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	var out []AgentInfo
	for _, name := range cur.Agents {
		if d, ok := w.def(name); ok {
			out = append(out, w.info(d, true))
		}
	}
	return out
}

// CatalogAgents lists every agent definition, flagging the ones assigned to
// the active space.
func (w *Workspace) CatalogAgents() []AgentInfo {
	cur, _ := w.opts.Spaces.Current()
	w.mu.Lock()
	defer w.mu.Unlock()
	var out []AgentInfo
	for _, d := range w.defs {
		out = append(out, w.info(d, slices.Contains(cur.Agents, d.Name)))
	}
	return out
}

// Agent returns one agent definition by name.
func (w *Workspace) Agent(name string) (AgentInfo, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	d, ok := w.def(name)
	if !ok {
		return AgentInfo{}, fmt.Errorf("no agent named %q in the catalog", name)
	}
	cur, _ := w.opts.Spaces.Current()
	return w.info(d, slices.Contains(cur.Agents, name)), nil
}

func (w *Workspace) info(d catalog.AgentDef, assigned bool) AgentInfo {
	cur, _ := w.opts.Spaces.Current()
	model := ResolveModel(d.Model, cur, w.opts.Config.Provider.Model).Value
	return AgentInfo{
		Name:        d.Name,
		Description: d.Description,
		Model:       model,
		Tools:       slices.Clone(d.Tools),
		Prompt:      d.Prompt,
		Assigned:    assigned,
		Active:      assigned && d.Name == w.active,
	}
}

// Chat returns the conversation for the active space and agent. When there is
// none, or it cannot be opened, the returned Chat fails every Send with the
// reason, so callers never receive nil.
func (w *Workspace) Chat() Chat {
	cur, ok := w.opts.Spaces.Current()
	if !ok {
		return idleChat{err: errors.New("no space selected: use /space use <name>")}
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.active == "" {
		return idleChat{err: errors.New("no agent in this space: assign one with /space assign <agent>")}
	}
	key := cur.Name + "/" + w.active
	if c, ok := w.convs[key]; ok {
		return c
	}
	c, err := w.open(cur, w.active)
	if err != nil {
		return idleChat{err: err}
	}
	w.convs[key] = c
	return c
}

// UseSpace switches to the named space and selects its first agent.
func (w *Workspace) UseSpace(name string) error {
	if err := w.opts.Spaces.Use(name); err != nil {
		return err
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	w.active = w.firstAssigned()
	return nil
}

// SelectAgent makes an agent of the active space the current conversation.
func (w *Workspace) SelectAgent(name string) error {
	cur, ok := w.opts.Spaces.Current()
	if !ok {
		return errors.New("no space selected")
	}
	if !slices.Contains(cur.Agents, name) {
		return fmt.Errorf("agent %q is not assigned to space %q", name, cur.Name)
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	w.active = name
	return nil
}

// NewSpace registers a space. It does not switch to it.
func (w *Workspace) NewSpace(name, path string) error {
	return w.opts.Spaces.Add(name, path, w.opts.Now())
}

// AssignAgent adds a catalog agent to the active space.
func (w *Workspace) AssignAgent(name string) error {
	cur, ok := w.opts.Spaces.Current()
	if !ok {
		return errors.New("no space selected")
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	if _, ok := w.def(name); !ok {
		return fmt.Errorf("no agent named %q in the catalog", name)
	}
	if err := w.opts.Spaces.Assign(cur.Name, name); err != nil {
		return err
	}
	if w.active == "" {
		w.active = name
	}
	return nil
}

// UnassignAgent removes an agent from the active space and moves the
// selection if it was on that agent.
func (w *Workspace) UnassignAgent(name string) error {
	cur, ok := w.opts.Spaces.Current()
	if !ok {
		return errors.New("no space selected")
	}
	if err := w.opts.Spaces.Unassign(cur.Name, name); err != nil {
		return err
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.active == name {
		w.active = w.firstAssigned()
	}
	return nil
}

// NewAgent creates a starter agent in the catalog and returns its folder.
func (w *Workspace) NewAgent(name, description string) (string, error) {
	if isManaged(name) {
		return "", fmt.Errorf("agent %q is managed and cannot be created again", managedAgent)
	}
	if catalog.Exists(w.opts.Home, name) {
		return "", fmt.Errorf("agent %q already exists", name)
	}
	if strings.TrimSpace(description) == "" {
		description = "custom agent"
	}
	def := catalog.AgentDef{Name: name, Description: description, Tools: catalog.DefaultTools()}
	if err := catalog.Save(w.opts.Home, def, starterPrompt(name)); err != nil {
		return "", err
	}
	if err := w.reload(); err != nil {
		return "", err
	}
	return catalog.Path(w.opts.Home, name), nil
}

// Close ends every conversation with a model summary, in parallel, and closes
// the stores. The context bounds the summaries: when it ends, each conversation
// keeps a local summary instead. See exit.go.
func (w *Workspace) Close(ctx context.Context) error {
	return w.shutdown(ctx, true)
}

// CloseFast ends every conversation with a local summary and makes no model
// call, then closes the stores.
func (w *Workspace) CloseFast() error {
	return w.shutdown(context.Background(), false)
}

// open builds a conversation. Callers hold w.mu.
func (w *Workspace) open(sp space.Space, agentName string) (*Conversation, error) {
	def, ok := w.def(agentName)
	if !ok {
		return nil, fmt.Errorf("agent %q is not in the catalog", agentName)
	}
	// The model is the agent's own, else the space's, else the global one.
	model := ResolveModel(def.Model, sp, w.opts.Config.Provider.Model).Value
	prov := w.opts.NewProvider(model)
	st, err := w.storesFor(sp.Path)
	if err != nil {
		return nil, err
	}
	key := sp.Name + "/" + agentName
	files := &fileLog{}
	box := &reviewBox{}
	work, err := w.workStateFor(key, sp.Path, prov)
	if err != nil {
		return nil, err
	}
	ctl, err := w.newFlow(context.Background(), sp, agentName, st.mem.Tasks(), prov,
		func(t flow.Task) {
			work.ResetTurns()
			files.reset()
			if t.Parent.Set() {
				w.acceptDelegation(context.Background(), t)
			}
		},
		func(t flow.Task) {
			if work.On() {
				work.Set(false)
				box.set(reviewText(context.Background(), t, sp.Path, files.list()))
			}
		})
	if err != nil {
		return nil, err
	}
	tools, err := w.buildTools(sp.Path, def.Tools, st.mem, w.messengerFor(sp.Name, agentName), ctl, files, agentName)
	if err != nil {
		return nil, err
	}
	ctx := context.Background()
	preamble, err := st.mem.Preamble(ctx, agentName, 10)
	if err != nil {
		return nil, fmt.Errorf("load memory preamble: %w", err)
	}
	projectCtx, err := config.LoadProjectContext(sp.Path)
	if err != nil {
		return nil, fmt.Errorf("read AGENTS.md: %w", err)
	}
	base := def.Prompt
	if strings.TrimSpace(base) == "" {
		base = w.opts.Config.EffectiveSystemPrompt()
	}
	base += "\n\n" + delegateRule
	if ResolveFlow(sp, w.opts.Config.Flow).Manager.Value == agentName {
		base += "\n\n" + managerRule
	}
	if agentName == debugAgent {
		base += "\n\n" + debugRules(w.opts.Config.Feedback.Confirm)
	}
	maxTurns := def.MaxTurns
	if maxTurns == 0 {
		maxTurns = w.opts.Config.MaxTurns
	}
	sess := gate.Session{Inner: confirmingGate(w.opts.Gate, w.opts.Hooks, key), Grants: w.grantsFor(key), Work: work}
	// conv is set below; the limit reads it only once a turn runs.
	var conv *Conversation
	ag := &agent.Agent{
		Name:      agentName,
		Provider:  prov,
		Tools:     tools,
		Gate:      &flowgate.Gate{Inner: gate.Work{Inner: sess, State: work}, Ctl: ctl, Warn: keyedWarn(w.opts.Hooks, key)},
		Compactor: compactorFor(w.opts.Config.Compaction, prov),
		// The token trigger and the overflow fallback read the same settings.
		ContextMaxPercent: w.opts.Config.Compaction.MaxContextPercent,
		EmergencyKeep:     w.opts.Config.Compaction.Keep,
		WindowFor:         w.windowFor,
		Memory:            st.mem,
		Audit:             st.aud,
		System:            config.ComposeSystemPrompt(base, preamble, projectCtx),
		MaxTurns:          maxTurns,
		OnText:            keyedText(w.opts.Hooks, key),
		OnToolCall:        autoAwareTool(w.opts.Hooks, key, work, sess),
		OnWarning:         keyedWarn(w.opts.Hooks, key),
		// Checked before every model call, so a turn that goes over its budget
		// stops between tool rounds.
		Limit: func(ctx context.Context) error { return conv.checkBudget(ctx, 0) },
	}
	resumed := w.startOrResume(ctx, ag, st.mem, agentName)
	conv = &Conversation{Agent: ag, prov: prov, store: st.mem, name: agentName, space: sp.Name, inbox: w.inbox, resumed: resumed, ctl: ctl, baseSystem: ag.System, work: work, files: files, review: box,
		startModel: prov.Model(), revertWarn: keyedWarn(w.opts.Hooks, key),
		budgets:      func(ctx context.Context) ([]Budget, error) { return w.budgetsFor(ctx, st.mem, sp.Name, agentName) },
		budgetNotice: keyedWarn(w.opts.Hooks, key)}
	return conv, nil
}

// memoryPathFor resolves memory.path for a space. A relative path is taken
// from the space directory; an absolute one is used as is.
func memoryPathFor(spacePath, cfgPath string) string {
	return spacePathFor(spacePath, cfgPath, ".groot/groot.db")
}

// auditPathFor resolves audit.path for a space, with the same rules as memoryPathFor.
func auditPathFor(spacePath, cfgPath string) string {
	return spacePathFor(spacePath, cfgPath, ".groot/audit.jsonl")
}

func spacePathFor(spacePath, cfgPath, fallback string) string {
	if cfgPath == "" {
		cfgPath = fallback
	}
	if filepath.IsAbs(cfgPath) {
		return cfgPath
	}
	return filepath.Join(spacePath, cfgPath)
}

// storesFor returns the memory and audit handles of a space, opening them at
// the paths set by memory.path and audit.path on first use. Callers hold w.mu.
func (w *Workspace) storesFor(path string) (*spaceStores, error) {
	if s, ok := w.stores[path]; ok {
		return s, nil
	}
	mem, err := memory.OpenSQLite(memoryPathFor(path, w.opts.Config.Memory.Path))
	if err != nil {
		return nil, fmt.Errorf("open memory for %s: %w", path, err)
	}
	aud, err := audit.Open(auditPathFor(path, w.opts.Config.Audit.Path))
	if err != nil {
		_ = mem.Close()
		return nil, fmt.Errorf("open audit log for %s: %w", path, err)
	}
	s := &spaceStores{mem: mem, aud: aud}
	w.stores[path] = s
	return s, nil
}

func compactorFor(c config.CompactionConfig, p provider.Provider) compact.Strategy {
	switch c.Strategy {
	case "window":
		return compact.SlidingWindow{Keep: c.Keep}
	case "summarize":
		return compact.Summarize{Provider: p, Threshold: c.Threshold}
	}
	return compact.None{}
}

// idleChat stands in when no conversation can run. Every Send returns the
// reason, so the UI can show it without special cases.
type idleChat struct {
	err error
}

func (c idleChat) Send(context.Context, string) (string, error) { return "", c.err }
func (c idleChat) Model() string                                { return "" }
func (c idleChat) SetModel(string) error                        { return c.err }
func (c idleChat) Compact(context.Context) error                { return nil }
func (c idleChat) MessageCount() int                            { return 0 }
func (c idleChat) Usage() api.Usage                             { return api.Usage{} }
func (c idleChat) CostUSD() float64                             { return 0 }
func (c idleChat) ContextTokens() int64                         { return 0 }
func (c idleChat) ContextWindow() int                           { return provider.DefaultContextWindow }
func (c idleChat) Reset()                                       {}
func (c idleChat) Task() (flow.Task, bool)                      { return flow.Task{}, false }
func (c idleChat) TaskStatus(context.Context) string            { return "" }
func (c idleChat) FailTask(context.Context, string) (string, error) {
	return "", c.err
}
func (c idleChat) WorkMode() bool   { return false }
func (c idleChat) SetWorkMode(bool) {}
func (c idleChat) TaskAccept(context.Context) (string, error) {
	return "", c.err
}
func (c idleChat) TaskReject(context.Context, string) (string, error) {
	return "", c.err
}
func (c idleChat) TakeReview() string       { return "" }
func (c idleChat) Resumed() (Resumed, bool) { return Resumed{}, false }
func (c idleChat) Clear() func(context.Context) error {
	return func(context.Context) error { return nil }
}
func (c idleChat) SessionID() string { return "" }

// ImportAgent fetches an agent file (a local path or an http(s) URL), maps its
// tools, and saves it to the catalog. An existing agent is replaced only when
// overwrite is true. groot itself is never replaced.
func (w *Workspace) ImportAgent(ctx context.Context, source string, overwrite bool) (ImportResult, error) {
	plan, err := agentimport.Load(ctx, source)
	if err != nil {
		return ImportResult{}, err
	}
	return w.importPlan(plan, overwrite)
}

// importPlan saves a planned import. Callers hold no lock.
func (w *Workspace) importPlan(plan agentimport.Planned, overwrite bool) (ImportResult, error) {
	name := plan.Def.Name
	if isManaged(name) {
		return ImportResult{}, fmt.Errorf("agent %q is managed and cannot be overwritten", managedAgent)
	}
	if catalog.Exists(w.opts.Home, name) && !overwrite {
		return ImportResult{}, fmt.Errorf("agent %q already exists; import again with --force to replace it", name)
	}
	if err := catalog.Save(w.opts.Home, plan.Def, plan.Def.Prompt); err != nil {
		return ImportResult{}, err
	}
	if err := w.reload(); err != nil {
		return ImportResult{}, err
	}
	return ImportResult{Name: name, Path: catalog.Path(w.opts.Home, name), Warnings: plan.Warnings}, nil
}
