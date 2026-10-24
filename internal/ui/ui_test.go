package ui

import (
	"context"
	"errors"
	"fmt"
	"github.com/tinchoram/groot-cli/internal/feedback"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/tinchoram/groot-cli/internal/api"
	"github.com/tinchoram/groot-cli/internal/flow"
	"github.com/tinchoram/groot-cli/internal/gate"
	"github.com/tinchoram/groot-cli/internal/inbox"
	"github.com/tinchoram/groot-cli/internal/provider"
	"github.com/tinchoram/groot-cli/internal/tool"
	"github.com/tinchoram/groot-cli/internal/workspace"
)

type fakeSession struct {
	model     string
	sends     []string
	reply     string
	sendErr   error
	messages  int
	cleared   bool
	setModels []string
	usage     api.Usage
	cost      float64
	busyReply bool
	resumed   *workspace.Resumed
	task      *flow.Task
	failed    []string
	workOn    bool
	review    string
	accepted  []string
	rejected  []string
}

func (f *fakeSession) Task() (flow.Task, bool) {
	if f.task == nil {
		return flow.Task{}, false
	}
	return *f.task, true
}

func (f *fakeSession) TaskStatus(context.Context) string {
	if f.task == nil {
		return "no open task; nothing classified in this turn"
	}
	return fmt.Sprintf("task #%d · %s · %s\ngoal: %s", f.task.ID, f.task.Size, f.task.Phase, f.task.Goal)
}

func (f *fakeSession) FailTask(_ context.Context, reason string) (string, error) {
	if f.task == nil {
		return "", errors.New("no open task")
	}
	f.failed = append(f.failed, reason)
	return fmt.Sprintf("task #%d failed: %s", f.task.ID, reason), nil
}

func (f *fakeSession) Send(_ context.Context, prompt string) (string, error) {
	f.sends = append(f.sends, prompt)
	return f.reply, f.sendErr
}
func (f *fakeSession) Model() string { return f.model }
func (f *fakeSession) SetModel(name string) error {
	f.setModels = append(f.setModels, name)
	f.model = name
	return nil
}
func (f *fakeSession) Compact(context.Context) error { f.messages = 4; return nil }
func (f *fakeSession) MessageCount() int             { return f.messages }
func (f *fakeSession) Usage() api.Usage              { return f.usage }
func (f *fakeSession) CostUSD() float64              { return f.cost }
func (f *fakeSession) Reset()                        { f.cleared = true }

func (f *fakeSession) Resumed() (workspace.Resumed, bool) {
	if f.resumed == nil {
		return workspace.Resumed{}, false
	}
	return *f.resumed, true
}

func (f *fakeSession) WorkMode() bool      { return f.workOn }
func (f *fakeSession) SetWorkMode(on bool) { f.workOn = on }
func (f *fakeSession) TakeReview() string {
	r := f.review
	f.review = ""
	return r
}
func (f *fakeSession) TaskAccept(context.Context) (string, error) {
	f.accepted = append(f.accepted, "yes")
	return "task accepted", nil
}
func (f *fakeSession) TaskReject(_ context.Context, reason string) (string, error) {
	f.rejected = append(f.rejected, reason)
	return "task sent back: " + reason, nil
}

func (f *fakeSession) Clear() func(context.Context) error {
	f.cleared = true
	return func(context.Context) error { return nil }
}
func (f *fakeSession) SessionID() string { return "s-test" }

// fakeWorkspace is an in-memory Workspace with two agents and two spaces.
type fakeWorkspace struct {
	order       []string
	spaces      map[string]workspace.SpaceInfo
	catalog     []workspace.AgentInfo
	active      string
	agent       string
	chat        *fakeSession
	closed      bool
	unread      map[string]int
	workModes   map[string]bool
	blockClose  bool
	backlog     []feedback.Item
	lastContext []string
	fastClosed  bool
	grants      map[string][]string // session approvals by conversation key
	models      []provider.ModelInfo
	modelErr    error
	modelWarn   string
	checkErr    error
	// defaultModel is the configured default; defaultSet records --default.
	defaultModel string
	defaultSet   string
	savedModels  map[string]string
	agentTools   map[string][]string
	toolEdits    []string
	// spend is what Spend answers; budgetSet records SetSpaceBudget and
	// SetAgentBudget as "space:<name>" and "agent:<name>".
	spend     workspace.SpendView
	budgetSet map[string]float64
	budgetErr error
	inbox     []inbox.Message
	handSent  []string
	// flow is what SpaceFlow reports; flowErr makes it fail.
	flow    workspace.FlowView
	flowErr error
}

func newFakeWorkspace(chat *fakeSession) *fakeWorkspace {
	return &fakeWorkspace{
		order: []string{"default"},
		spaces: map[string]workspace.SpaceInfo{
			"default": {Name: "default", Path: "/work/default", Agents: []string{"devjunior"}},
		},
		catalog: []workspace.AgentInfo{
			{Name: "devjunior", Description: "Junior developer", Model: "claude-sonnet-5-5", Tools: []string{"read_file", "bash"}},
			{Name: "reviewer", Description: "Reviews diffs", Model: "claude-sonnet-5-5", Tools: []string{"read_file"}},
		},
		active: "default",
		agent:  "devjunior",
		chat:   chat,
	}
}

func (f *fakeWorkspace) assigned(space string) []string { return f.spaces[space].Agents }

func (f *fakeWorkspace) Spaces() []workspace.SpaceInfo {
	var out []workspace.SpaceInfo
	for _, n := range f.order {
		sp := f.spaces[n]
		sp.Active = n == f.active
		out = append(out, sp)
	}
	return out
}
func (f *fakeWorkspace) ActiveSpace() (workspace.SpaceInfo, bool) {
	sp, ok := f.spaces[f.active]
	sp.Active = true
	return sp, ok
}
func (f *fakeWorkspace) Agents() []workspace.AgentInfo {
	var out []workspace.AgentInfo
	for _, n := range f.assigned(f.active) {
		if a, err := f.Agent(n); err == nil {
			out = append(out, a)
		}
	}
	return out
}
func (f *fakeWorkspace) CatalogAgents() []workspace.AgentInfo {
	var out []workspace.AgentInfo
	for _, a := range f.catalog {
		a.Assigned = contains(f.assigned(f.active), a.Name)
		out = append(out, a)
	}
	return out
}
func (f *fakeWorkspace) ActiveAgent() string { return f.agent }
func (f *fakeWorkspace) ConversationKey() string {
	if f.active == "" || f.agent == "" {
		return ""
	}
	return f.active + "/" + f.agent
}
func (f *fakeWorkspace) Chat() Session { return f.chat }
func (f *fakeWorkspace) UseSpace(name string) error {
	if _, ok := f.spaces[name]; !ok {
		return fmt.Errorf("no space named %q", name)
	}
	f.active = name
	f.agent = ""
	if a := f.assigned(name); len(a) > 0 {
		f.agent = a[0]
	}
	return nil
}
func (f *fakeWorkspace) SelectAgent(name string) error {
	if !contains(f.assigned(f.active), name) {
		return fmt.Errorf("agent %q is not assigned to space %q", name, f.active)
	}
	f.agent = name
	return nil
}
func (f *fakeWorkspace) NewSpace(name, path string) error {
	if _, ok := f.spaces[name]; ok {
		return fmt.Errorf("space %q already exists", name)
	}
	f.order = append(f.order, name)
	f.spaces[name] = workspace.SpaceInfo{Name: name, Path: path}
	return nil
}
func (f *fakeWorkspace) AssignAgent(name string) error {
	if _, err := f.Agent(name); err != nil {
		return err
	}
	sp := f.spaces[f.active]
	sp.Agents = append(sp.Agents, name)
	f.spaces[f.active] = sp
	if f.agent == "" {
		f.agent = name
	}
	return nil
}
func (f *fakeWorkspace) UnassignAgent(name string) error {
	sp := f.spaces[f.active]
	sp.Agents = removeString(sp.Agents, name)
	f.spaces[f.active] = sp
	if f.agent == name {
		f.agent = ""
		if a := sp.Agents; len(a) > 0 {
			f.agent = a[0]
		}
	}
	return nil
}
func (f *fakeWorkspace) NewAgent(name, description string) (string, error) {
	if _, err := f.Agent(name); err == nil {
		return "", fmt.Errorf("agent %q already exists", name)
	}
	f.catalog = append(f.catalog, workspace.AgentInfo{Name: name, Description: description, Model: "claude-sonnet-5-5", Tools: []string{"read_file"}})
	return "/agents/" + name, nil
}
func (f *fakeWorkspace) Agent(name string) (workspace.AgentInfo, error) {
	for _, a := range f.catalog {
		if a.Name == name {
			a.Assigned = contains(f.assigned(f.active), name)
			a.Active = a.Assigned && name == f.agent
			return a, nil
		}
	}
	return workspace.AgentInfo{}, fmt.Errorf("no agent named %q in the catalog", name)
}
func (f *fakeWorkspace) Close(ctx context.Context) error {
	if f.blockClose {
		<-ctx.Done()
	}
	f.closed = true
	return nil
}

func (f *fakeWorkspace) CloseFast() error {
	f.fastClosed = true
	return nil
}

func (f *fakeWorkspace) UnreadCounts(context.Context) (map[string]int, error) { return f.unread, nil }

func (f *fakeWorkspace) WorkModes() map[string]bool { return f.workModes }

func (f *fakeWorkspace) SendFeedback(spaceName, agent, kind, text string, context []string) (int64, string, error) {
	f.lastContext = context
	return 7, "root/debug", nil
}

func (f *fakeWorkspace) Backlog(status string) ([]feedback.Item, error) {
	return f.backlog, nil
}

func (f *fakeWorkspace) WorkLimits() (int, float64) { return 50, 5.0 }

func (f *fakeWorkspace) UpgradeAgents(name string) ([]string, error) {
	if name == "" {
		return []string{"devjunior: up to date", "reviewer: up to date"}, nil
	}
	return []string{name + ": up to date"}, nil
}

func (f *fakeWorkspace) Inbox(context.Context, int) ([]inbox.Message, error) { return f.inbox, nil }

func (f *fakeWorkspace) SendByHand(_ context.Context, to, subject, body string) (string, error) {
	f.handSent = append(f.handSent, to+"|"+subject+"|"+body)
	return "default/" + to, nil
}

func (f *fakeWorkspace) LibraryAgents() ([]workspace.LibraryInfo, error) {
	return []workspace.LibraryInfo{
		{Name: "developer", Description: "Expert engineer", Tools: []string{"bash"}, Source: "builtin", Collection: "@tinchoram", Author: "@tinchoram"},
		{Name: "devops-engineer", Description: "Pipelines and containers", Tools: []string{"bash"}, Source: "public",
			Collection: "VoltAgent awesome-claude-code-subagents", Author: "VoltAgent/awesome-claude-code-subagents", License: "MIT", ValidatedBy: "@tinchoram",
			URL: "https://example.com/devops-engineer.md"},
	}, nil
}

func (f *fakeWorkspace) LibraryAgent(name string) (workspace.LibraryDetail, error) {
	if name != "VoltAgent awesome-claude-code-subagents/devops-engineer" {
		return workspace.LibraryDetail{}, fmt.Errorf("no library agent named %q", name)
	}
	return workspace.LibraryDetail{
		LibraryInfo: workspace.LibraryInfo{Name: "devops-engineer", Description: "Pipelines and containers", Source: "public"},
		Prompt:      "You build pipelines.\nKeep them small.",
	}, nil
}

func (f *fakeWorkspace) InstallLibraryAgent(name, space string, overwrite bool) error {
	if _, err := f.Agent(name); err == nil && !overwrite {
		return fmt.Errorf("agent %q already exists; pass overwrite to replace it", name)
	}
	if name == "groot" {
		return errors.New("agent \"groot\" is managed")
	}
	f.catalog = append(f.catalog, workspace.AgentInfo{Name: name, Description: "from library", Model: "claude-sonnet-5-5", Tools: []string{"read_file"}})
	return nil
}

func (f *fakeWorkspace) ImportAgent(_ context.Context, source string, overwrite bool) (workspace.ImportResult, error) {
	if source == "/bad/agent.md" {
		return workspace.ImportResult{}, errors.New("read /bad/agent.md: no such file")
	}
	if _, err := f.Agent("scout"); err == nil && !overwrite {
		return workspace.ImportResult{}, errors.New(`agent "scout" already exists; import again with --force to replace it`)
	}
	f.catalog = append(f.catalog, workspace.AgentInfo{Name: "scout", Description: "finds code", Model: "claude-sonnet-5-5", Tools: []string{"read_file"}})
	return workspace.ImportResult{Name: "scout", Path: "/agents/scout.md", Warnings: []string{"Grep has no equivalent yet and was dropped"}}, nil
}

func newTestModel(t *testing.T, chat *fakeSession) (Model, *fakeWorkspace) {
	t.Helper()
	chat.messages = 10
	ws := newFakeWorkspace(chat)
	m := NewModel(Options{Workspace: ws, Version: "test"}) // no bridge: the turn answers through its command
	m.width, m.height = 120, 40
	m.resize()
	return m, ws
}

// findMsg runs cmd, unwraps any tea.Batch it returns, and yields the first
// message of type T. Spinner ticks in the batch are harmless to run.
func findMsg[T any](t *testing.T, cmd tea.Cmd) T {
	t.Helper()
	var zero T
	if cmd == nil {
		t.Fatal("expected a command")
	}
	msg := cmd()
	batch, ok := msg.(tea.BatchMsg)
	if !ok {
		if v, ok := msg.(T); ok {
			return v
		}
		t.Fatalf("command produced %T, want %T", msg, zero)
	}
	for _, c := range batch {
		if c == nil {
			continue
		}
		if v, ok := c().(T); ok {
			return v
		}
	}
	t.Fatalf("no %T in command output", zero)
	return zero
}

func typeText(m Model, s string) Model {
	for _, r := range s {
		next, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
		m = next.(Model)
	}
	return m
}

func press(m Model, k tea.KeyType) (Model, tea.Cmd) {
	next, cmd := m.Update(tea.KeyMsg{Type: k})
	return next.(Model), cmd
}

func pressRune(m Model, r rune) (Model, tea.Cmd) {
	next, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
	return next.(Model), cmd
}

func sendLine(t *testing.T, m Model, line string) Model {
	t.Helper()
	m = typeText(m, line)
	m, cmd := press(m, tea.KeyEnter)
	if cmd != nil {
		if msg := findOptional(cmd); msg != nil {
			next, _ := m.Update(msg)
			m = next.(Model)
		}
	}
	return m
}

// findOptional runs a command and returns its answer or note message if one
// is produced; other messages are returned unchanged.
func findOptional(cmd tea.Cmd) tea.Msg {
	msg := cmd()
	if batch, ok := msg.(tea.BatchMsg); ok {
		for _, c := range batch {
			if c == nil {
				continue
			}
			switch v := c().(type) {
			case answerMsg, noteMsg:
				return v
			}
		}
		return nil
	}
	return msg
}

func contains(list []string, v string) bool {
	for _, x := range list {
		if x == v {
			return true
		}
	}
	return false
}

func removeString(list []string, v string) []string {
	var out []string
	for _, x := range list {
		if x != v {
			out = append(out, x)
		}
	}
	return out
}

func TestParseCommand(t *testing.T) {
	cases := []struct {
		in    string
		isCmd bool
		name  string
		arg   string
		known bool
	}{
		{"/model claude-haiku-5-5", true, "model", "claude-haiku-5-5", true},
		{"  /tokens  ", true, "tokens", "", true},
		{"/space use payments", true, "space", "use payments", true},
		{"/nonsense x", true, "nonsense", "x", false},
		{"list pods in prod", false, "", "", false},
	}
	for _, c := range cases {
		cmd, ok := ParseCommand(c.in)
		if ok != c.isCmd {
			t.Errorf("%q: isCommand=%v want %v", c.in, ok, c.isCmd)
			continue
		}
		if ok && (cmd.Name != c.name || cmd.Arg != c.arg || cmd.Known() != c.known) {
			t.Errorf("%q: got %+v known=%v", c.in, cmd, cmd.Known())
		}
	}
}

func TestHelpListsSpaceAndAgentCommands(t *testing.T) {
	m, _ := newTestModel(t, &fakeSession{model: "m"})
	m = sendLine(t, m, "/help")
	out := m.transcriptText()
	for _, want := range []string{"/space list", "/space new", "/agent new", "/agent show"} {
		if !strings.Contains(out, want) {
			t.Errorf("help missing %q", want)
		}
	}
}

func TestSendRunsSessionAndShowsReply(t *testing.T) {
	chat := &fakeSession{model: "m", reply: "pong"}
	m, _ := newTestModel(t, chat)
	m = typeText(m, "ping")
	m, cmd := press(m, tea.KeyEnter)
	if !m.busy {
		t.Fatal("model should be busy while the agent works")
	}
	next, _ := m.Update(findMsg[answerMsg](t, cmd))
	m = next.(Model)
	if m.busy || len(chat.sends) != 1 || chat.sends[0] != "ping" {
		t.Fatalf("busy=%v sends=%v", m.busy, chat.sends)
	}
	if !strings.Contains(m.transcriptText(), "pong") {
		t.Fatalf("reply not shown: %s", m.transcriptText())
	}
}

func TestPromptEchoHasTurnHeaderAndPrefix(t *testing.T) {
	m, _ := newTestModel(t, &fakeSession{model: "m", reply: "ok"})
	m = typeText(m, "list pods")
	m, _ = press(m, tea.KeyEnter)
	out := m.transcriptText()
	if !strings.Contains(out, "── ") || !strings.Contains(out, "❯ list pods") {
		t.Fatalf("turn header or prompt echo missing:\n%s", out)
	}
}

func TestErrorFromSessionIsShown(t *testing.T) {
	m, _ := newTestModel(t, &fakeSession{model: "m", sendErr: errors.New("rate limited")})
	m = typeText(m, "x")
	m, cmd := press(m, tea.KeyEnter)
	next, _ := m.Update(findMsg[answerMsg](t, cmd))
	m = next.(Model)
	if !strings.Contains(m.transcriptText(), "error: rate limited") {
		t.Fatalf("error not shown: %s", m.transcriptText())
	}
}

func TestStreamingDeltasAppearInTranscript(t *testing.T) {
	m, _ := newTestModel(t, &fakeSession{model: "m"})
	m.busy = true
	for _, d := range []string{"Hel", "lo"} {
		next, _ := m.Update(streamMsg{delta: d})
		m = next.(Model)
	}
	if !strings.Contains(m.transcriptText(), "Hello") {
		t.Fatalf("stream not rendered: %q", m.transcriptText())
	}
}

func TestToolCallRendersToolLine(t *testing.T) {
	m, _ := newTestModel(t, &fakeSession{model: "m"})
	m.busy = true
	next, _ := m.Update(toolCallMsg{name: "kubectl", preview: "kubectl get pods -n prod"})
	m = next.(Model)
	if !strings.Contains(m.transcriptText(), "[tool] kubectl get pods -n prod") {
		t.Fatalf("tool line missing: %s", m.transcriptText())
	}
}

func TestInlineConfirmShowsBoxAndAnswers(t *testing.T) {
	m, _ := newTestModel(t, &fakeSession{model: "m"})
	reply := make(chan bool, 1)
	req := gate.Request{Tool: "bash", Risk: tool.RiskExec, Detail: "rm -rf /tmp/x"}
	next, _ := m.Update(confirmMsg{req: req, reply: reply})
	m = next.(Model)
	if !strings.Contains(m.View(), "(y/n/a)") || !strings.Contains(m.View(), "y: yes") {
		t.Fatalf("inline confirm box missing:\n%s", m.View())
	}
	m = typeText(m, "y")
	if !<-reply {
		t.Fatal("y must approve")
	}
	if m.pending != nil {
		t.Fatal("pending must clear after a decision")
	}
}

func TestWriteApprovalIsOneInlineLine(t *testing.T) {
	m, _ := newTestModel(t, &fakeSession{model: "m"})
	reply := make(chan bool, 1)
	req := gate.Request{Tool: "write_file", Risk: tool.RiskWrite, Input: `{"path":"notes/a.md"}`,
		Detail: "--- a\n+++ b\n-old\n+new"}
	next, _ := m.Update(confirmMsg{req: req, reply: reply})
	m = next.(Model)
	view := stripANSI(m.View())
	if !strings.Contains(view, "allow write_file: notes/a.md (+1 −1)? (y/n/a)") {
		t.Fatalf("inline approval line missing:\n%s", view)
	}
	if strings.Contains(view, "+new") {
		t.Fatal("the diff is not drawn until /diff or d asks for it")
	}
	m, _ = pressRune(m, 'd')
	if !strings.Contains(stripANSI(m.transcriptText()), "+new") {
		t.Fatalf("d must print the diff:\n%s", m.transcriptText())
	}
	m, _ = pressRune(m, 'x')
	select {
	case v := <-reply:
		t.Fatalf("a non-decision key must not answer, got %v", v)
	default:
	}
	m = typeText(m, "y")
	if v := <-reply; !v {
		t.Fatal("y must approve")
	}
}

func TestConfirmationDenyWithN(t *testing.T) {
	m, _ := newTestModel(t, &fakeSession{model: "m"})
	reply := make(chan bool, 1)
	next, _ := m.Update(confirmMsg{req: gate.Request{Tool: "bash", Risk: tool.RiskExec, Detail: "rm x"}, reply: reply})
	m = typeText(next.(Model), "n")
	if v := <-reply; v {
		t.Fatal("n must deny")
	}
}

func TestSlashModelChangesModel(t *testing.T) {
	chat := &fakeSession{model: "claude-sonnet-5-5"}
	m, _ := newTestModel(t, chat)
	m = sendLine(t, m, "/model claude-opus-5-5")
	if len(chat.setModels) != 1 || chat.setModels[0] != "claude-opus-5-5" {
		t.Fatalf("model not changed: %v", chat.setModels)
	}
	if !strings.Contains(m.View(), "claude-opus-5-5") {
		t.Fatal("status line should show the new model")
	}
}

func TestSlashTokensShowsUsageAndCost(t *testing.T) {
	chat := &fakeSession{model: "m", usage: api.Usage{InputTokens: 1200, OutputTokens: 300}, cost: 0.0054}
	m, _ := newTestModel(t, chat)
	m = sendLine(t, m, "/tokens")
	out := m.transcriptText()
	if !strings.Contains(out, "session usage:") || !strings.Contains(out, "1,200") || !strings.Contains(out, "$0.0054") {
		t.Fatalf("usage not shown: %s", out)
	}
}

func TestIdleStatusLineShowsModelAndUsage(t *testing.T) {
	chat := &fakeSession{model: "claude-sonnet-5-5", usage: api.Usage{InputTokens: 1234, OutputTokens: 56}, cost: 0.0012}
	m, _ := newTestModel(t, chat)
	for _, want := range []string{"claude-sonnet-5-5", "1,234 in", "56 out", "~$0.0012"} {
		if !strings.Contains(m.View(), want) {
			t.Errorf("status line missing %q:\n%s", want, m.View())
		}
	}
}

func TestBusyStatusLineShowsThinking(t *testing.T) {
	m, _ := newTestModel(t, &fakeSession{model: "m"})
	m.busy = true
	if !strings.Contains(m.View(), "thinking...") {
		t.Fatalf("busy indicator missing:\n%s", m.View())
	}
}

func TestSlashCompactReportsCounts(t *testing.T) {
	m, _ := newTestModel(t, &fakeSession{model: "m"})
	m = typeText(m, "/compact")
	m, cmd := press(m, tea.KeyEnter)
	next, _ := m.Update(findMsg[noteMsg](t, cmd))
	m = next.(Model)
	if !strings.Contains(m.transcriptText(), "compacted: 10 → 4 messages") {
		t.Fatalf("compaction notice missing: %s", m.transcriptText())
	}
}

func TestCtrlCClosesWorkspaceAndQuits(t *testing.T) {
	m, ws := newTestModel(t, &fakeSession{model: "m"})
	_, cmd := press(m, tea.KeyCtrlC)
	if !ws.fastClosed {
		t.Fatal("Ctrl+C must close the workspace (ending conversations)")
	}
	if cmd == nil {
		t.Fatal("Ctrl+C must quit")
	}
	if _, ok := cmd().(tea.QuitMsg); !ok {
		t.Fatal("expected QuitMsg")
	}
}

func TestWideTerminalShowsSidebarAndHeader(t *testing.T) {
	m, _ := newTestModel(t, &fakeSession{model: "claude-sonnet-5-5"})
	view := m.View()
	for _, want := range []string{"SPACES", "AGENTS", "default", "devjunior", "Junior developer", "default ▸ devjunior · claude-sonnet-5-5"} {
		if !strings.Contains(view, want) {
			t.Errorf("view missing %q:\n%s", want, view)
		}
	}
}

func TestNarrowTerminalStartsWithSidebarHidden(t *testing.T) {
	m, _ := newTestModel(t, &fakeSession{model: "m"})
	next, _ := m.Update(tea.WindowSizeMsg{Width: 80, Height: 30})
	m = next.(Model)
	if m.sidebarOpen {
		t.Fatal("terminals under 90 columns must start with the sidebar hidden")
	}
	if strings.Contains(m.View(), "SPACES") {
		t.Fatal("sidebar should not render when hidden")
	}
}

func TestBannerShownInTranscript(t *testing.T) {
	m, _ := newTestModel(t, &fakeSession{model: "m"})
	m.sidebarOpen = false
	m.resize()
	if !strings.Contains(m.View(), bannerArt[0]) {
		t.Fatalf("wordmark missing:\n%s", m.View())
	}
}

func TestTabMovesFocusToSidebarAndBack(t *testing.T) {
	m, _ := newTestModel(t, &fakeSession{model: "m"})
	m, _ = press(m, tea.KeyTab)
	if !m.focusSidebar {
		t.Fatal("tab should focus the sidebar")
	}
	m, _ = press(m, tea.KeyEsc)
	if m.focusSidebar {
		t.Fatal("esc should return focus to the input")
	}
}

func TestSidebarSwitchesAgentAndRestoresTranscript(t *testing.T) {
	chat := &fakeSession{model: "m", reply: "pong"}
	m, ws := newTestModel(t, chat)
	m = sendLine(t, m, "hello")
	m.lines = append(m.lines, "pong-from-devjunior")

	// Add reviewer to the space, then switch to it from the sidebar.
	if err := ws.AssignAgent("reviewer"); err != nil {
		t.Fatal(err)
	}
	m, _ = press(m, tea.KeyTab)
	m, _ = pressRune(m, 'a')
	m, _ = press(m, tea.KeyDown)
	m, _ = press(m, tea.KeyEnter)
	if ws.agent != "reviewer" {
		t.Fatalf("agent = %q, want reviewer", ws.agent)
	}
	if strings.Contains(m.transcriptText(), "pong-from-devjunior") {
		t.Fatal("reviewer's chat must not show devjunior's transcript")
	}

	m, _ = press(m, tea.KeyEsc)
	m, _ = press(m, tea.KeyTab)
	m, _ = pressRune(m, 'a')
	m, _ = press(m, tea.KeyUp)
	m, _ = press(m, tea.KeyEnter)
	if ws.agent != "devjunior" || !strings.Contains(m.transcriptText(), "pong-from-devjunior") {
		t.Fatalf("switching back must restore the transcript: agent=%q", ws.agent)
	}
}

func TestSidebarSpaceSwitchSelectsFirstAgent(t *testing.T) {
	m, ws := newTestModel(t, &fakeSession{model: "m"})
	_ = ws.NewSpace("payments", "/work/pay")
	m, _ = press(m, tea.KeyTab)
	m, _ = pressRune(m, 's')
	m, _ = press(m, tea.KeyDown)
	m, _ = press(m, tea.KeyEnter)
	if ws.active != "payments" {
		t.Fatalf("active space = %q", ws.active)
	}
	if ws.agent != "" {
		t.Fatalf("an empty space should select no agent, got %q", ws.agent)
	}
}

func TestSwitchingWhileBusyIsAllowed(t *testing.T) {
	m, ws := newTestModel(t, &fakeSession{model: "m"})
	_ = ws.AssignAgent("reviewer")
	m.busy = true
	m, _ = press(m, tea.KeyTab)
	m, _ = pressRune(m, 'a')
	m, _ = press(m, tea.KeyDown)
	m, _ = press(m, tea.KeyEnter)
	if ws.agent != "reviewer" || m.busy {
		t.Fatalf("switching must be allowed: agent=%s busy=%v", ws.agent, m.busy)
	}
	if busy, _ := m.keyState("default/devjunior"); !busy {
		t.Fatal("the busy conversation keeps working in the background")
	}
}

func TestCtrlBTogglesSidebarAndCtrlEHidesChat(t *testing.T) {
	m, _ := newTestModel(t, &fakeSession{model: "m"})
	m, _ = press(m, tea.KeyCtrlB)
	if m.sidebarOpen {
		t.Fatal("ctrl-b should hide the sidebar")
	}
	m, _ = press(m, tea.KeyCtrlB)
	if !m.sidebarOpen {
		t.Fatal("ctrl-b should show the sidebar again")
	}
	m, _ = press(m, tea.KeyCtrlE)
	if m.chatOpen {
		t.Fatal("ctrl-e should hide the chat")
	}
	view := m.View()
	if !strings.Contains(view, "/work/default") || !strings.Contains(view, "read_file") {
		t.Fatalf("full-width sidebar should show path and tools:\n%s", view)
	}
	m, _ = press(m, tea.KeyCtrlE)
	if !m.chatOpen {
		t.Fatal("ctrl-e should show the chat again")
	}
}

func TestSpaceCommands(t *testing.T) {
	chat := &fakeSession{model: "m"}
	m, ws := newTestModel(t, chat)

	m = sendLine(t, m, "/space list")
	if !strings.Contains(m.transcriptText(), "default") {
		t.Fatalf("space list missing default: %s", m.transcriptText())
	}

	m = sendLine(t, m, "/space new payments /work/pay")
	if _, ok := ws.spaces["payments"]; !ok {
		t.Fatal("/space new did not create the space")
	}

	m = sendLine(t, m, "/space use payments")
	if ws.active != "payments" {
		t.Fatal("/space use did not switch")
	}

	m = sendLine(t, m, "/space use payments")
	_ = ws.UseSpace("default")
	m = sendLine(t, m, "/space assign reviewer")
	if !contains(ws.assigned("default"), "reviewer") {
		t.Fatal("/space assign did not assign")
	}
	m = sendLine(t, m, "/space unassign reviewer")
	if contains(ws.assigned("default"), "reviewer") {
		t.Fatal("/space unassign did not unassign")
	}

	m = sendLine(t, m, "/space bogus")
	if !strings.Contains(m.transcriptText(), "usage: /space") {
		t.Fatalf("bad subcommand should print usage: %s", m.transcriptText())
	}
}

func TestSpaceNewDuplicateShowsError(t *testing.T) {
	m, _ := newTestModel(t, &fakeSession{model: "m"})
	m = sendLine(t, m, "/space new default /work/x")
	if !strings.Contains(m.transcriptText(), "error: space \"default\" already exists") {
		t.Fatalf("error not shown: %s", m.transcriptText())
	}
}

func TestAgentCommands(t *testing.T) {
	m, ws := newTestModel(t, &fakeSession{model: "m"})

	m = sendLine(t, m, "/agent list")
	if !strings.Contains(m.transcriptText(), "reviewer") {
		t.Fatalf("agent list missing reviewer: %s", m.transcriptText())
	}

	m = sendLine(t, m, "/agent new auditor checks changes before merge")
	if _, err := ws.Agent("auditor"); err != nil {
		t.Fatal("/agent new did not create the agent")
	}
	if !strings.Contains(m.transcriptText(), "/agents/auditor") {
		t.Fatalf("/agent new should print the folder: %s", m.transcriptText())
	}

	m = sendLine(t, m, "/agent show reviewer")
	if !strings.Contains(m.transcriptText(), "Reviews diffs") {
		t.Fatalf("/agent show missing description: %s", m.transcriptText())
	}

	m = sendLine(t, m, "/agent show ghost")
	if !strings.Contains(m.transcriptText(), "error:") {
		t.Fatalf("unknown agent should show an error: %s", m.transcriptText())
	}
}

func TestBridgeConfirmRoundTrip(t *testing.T) {
	b := NewBridge()
	done := make(chan bool, 1)
	go func() { done <- b.Confirm("default/devjunior", gate.Request{Tool: "bash"}) }()
	msg := <-b.Events()
	cm, ok := msg.(confirmMsg)
	if !ok {
		t.Fatalf("expected confirmMsg, got %T", msg)
	}
	cm.reply <- true
	if !<-done {
		t.Fatal("bridge should return the UI decision")
	}
}

func TestUpRecallsPromptAndDownRestoresDraft(t *testing.T) {
	m, _ := newTestModel(t, &fakeSession{model: "m", reply: "ok"})
	m = sendLine(t, m, "first")
	m = typeText(m, "dra")
	m, _ = press(m, tea.KeyUp)
	if m.input.Value() != "first" {
		t.Fatalf("Up should recall the prompt, got %q", m.input.Value())
	}
	m, _ = press(m, tea.KeyDown)
	if m.input.Value() != "dra" {
		t.Fatalf("Down past the newest must restore the draft, got %q", m.input.Value())
	}
}

func TestEscReturnsRecalledLineToDraft(t *testing.T) {
	m, _ := newTestModel(t, &fakeSession{model: "m", reply: "ok"})
	m = sendLine(t, m, "first")
	m = typeText(m, "half")
	m, _ = press(m, tea.KeyUp)
	m, _ = press(m, tea.KeyEsc)
	if m.input.Value() != "half" || m.hist.Browsing() {
		t.Fatalf("esc should restore the draft, got %q", m.input.Value())
	}
}

func TestSlashCommandsAreRecorded(t *testing.T) {
	m, _ := newTestModel(t, &fakeSession{model: "m"})
	m = sendLine(t, m, "/tokens")
	m, _ = press(m, tea.KeyUp)
	if m.input.Value() != "/tokens" {
		t.Fatalf("slash command should be recalled, got %q", m.input.Value())
	}
}

func TestEditingRecalledLineKeepsStoredEntry(t *testing.T) {
	m, _ := newTestModel(t, &fakeSession{model: "m", reply: "ok"})
	m = sendLine(t, m, "kubectl get pods")
	m, _ = press(m, tea.KeyUp)
	m = typeText(m, " -n prod")
	m, _ = press(m, tea.KeyEsc)
	m, _ = press(m, tea.KeyUp)
	if m.input.Value() != "kubectl get pods" {
		t.Fatalf("stored entry was changed by editing: %q", m.input.Value())
	}
}

func TestArrowsDoNothingWhileSidebarHasFocus(t *testing.T) {
	m, _ := newTestModel(t, &fakeSession{model: "m", reply: "ok"})
	m = sendLine(t, m, "first")
	m, _ = press(m, tea.KeyTab)
	m, _ = press(m, tea.KeyUp)
	if m.input.Value() != "" {
		t.Fatalf("sidebar focus must not recall history into the input: %q", m.input.Value())
	}
}

func TestSubmitPersistsHistoryFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "history")
	m, ws := newTestModel(t, &fakeSession{model: "m", reply: "ok"})
	m.historyPath = path
	_ = ws
	line := `grep C:\tmp\n app.log`
	m = sendLine(t, m, line)
	got, err := LoadHistory(path)
	if err != nil || len(got) != 1 || got[0] != line {
		t.Fatalf("history file = %q (%v)", got, err)
	}
}

func TestCompletedTurnRefreshesConversationFromWorkspace(t *testing.T) {
	m, ws := newTestModel(t, &fakeSession{model: "m"})
	// The admin agent changed the selection while the turn ran.
	ws.agent = "reviewer"
	next, _ := m.Update(answerMsg{text: "done"})
	m = next.(Model)
	if m.key != "default/reviewer" {
		t.Fatalf("key after the turn = %q, want default/reviewer", m.key)
	}
}

func TestFreshHomeShowsGrootWelcome(t *testing.T) {
	chat := &fakeSession{model: "m"}
	ws := newFakeWorkspace(chat)
	ws.active, ws.agent = "root", "groot"
	m := NewModel(Options{Workspace: ws, Bridge: NewBridge(), ShowWelcome: true})
	out := m.transcriptText()
	for _, want := range []string{"administrator", "tab", "ctrl-b", "ctrl-e", "/help"} {
		if !strings.Contains(out, want) {
			t.Errorf("welcome missing %q:\n%s", want, out)
		}
	}
}

func TestWelcomeOnlyForGrootOrNotRequested(t *testing.T) {
	chat := &fakeSession{model: "m"}
	ws := newFakeWorkspace(chat)
	m := NewModel(Options{Workspace: ws, Bridge: NewBridge(), ShowWelcome: true})
	if strings.Contains(m.transcriptText(), "administrator") {
		t.Fatal("welcome must not appear in another agent's conversation")
	}
	ws.active, ws.agent = "root", "groot"
	m = NewModel(Options{Workspace: ws, Bridge: NewBridge()})
	if strings.Contains(m.transcriptText(), "administrator") {
		t.Fatal("welcome must not appear unless the first run asked for it")
	}
}

func TestAgentImportRunsAsyncAndReports(t *testing.T) {
	m, ws := newTestModel(t, &fakeSession{model: "m"})
	m = typeText(m, "/agent import /work/scout.md")
	m, cmd := press(m, tea.KeyEnter)
	next, _ := m.Update(findMsg[noteMsg](t, cmd))
	m = next.(Model)
	out := m.transcriptText()
	if !strings.Contains(out, "imported agent scout") || !strings.Contains(out, "warning: Grep") {
		t.Fatalf("import result not shown:\n%s", out)
	}
	if _, err := ws.Agent("scout"); err != nil {
		t.Fatal("the agent was not imported")
	}
}

func TestAgentImportRefusesExistingWithoutForce(t *testing.T) {
	m, ws := newTestModel(t, &fakeSession{model: "m"})
	_ = ws.catalog
	ws.catalog = append(ws.catalog, workspace.AgentInfo{Name: "scout"})
	m = typeText(m, "/agent import /work/scout.md")
	m, cmd := press(m, tea.KeyEnter)
	next, _ := m.Update(findMsg[noteMsg](t, cmd))
	m = next.(Model)
	if !strings.Contains(m.transcriptText(), "already exists") {
		t.Fatalf("refusal not shown:\n%s", m.transcriptText())
	}
}

func TestAgentImportForceOverwrites(t *testing.T) {
	m, ws := newTestModel(t, &fakeSession{model: "m"})
	ws.catalog = append(ws.catalog, workspace.AgentInfo{Name: "scout"})
	m = typeText(m, "/agent import /work/scout.md --force")
	m, cmd := press(m, tea.KeyEnter)
	next, _ := m.Update(findMsg[noteMsg](t, cmd))
	m = next.(Model)
	if !strings.Contains(m.transcriptText(), "imported agent scout") {
		t.Fatalf("--force should overwrite:\n%s", m.transcriptText())
	}
}

func TestAgentImportNeedsASource(t *testing.T) {
	m, _ := newTestModel(t, &fakeSession{model: "m"})
	m = sendLine(t, m, "/agent import")
	if !strings.Contains(m.transcriptText(), "usage: /agent import") {
		t.Fatalf("usage not shown: %s", m.transcriptText())
	}
}

func TestAgentLibraryListsEntries(t *testing.T) {
	m, _ := newTestModel(t, &fakeSession{model: "m"})
	m = sendLine(t, m, "/agent library")
	out := m.transcriptText()
	if !strings.Contains(out, "developer") || !strings.Contains(out, "Pipelines and containers") {
		t.Fatalf("library listing missing entries:\n%s", out)
	}
	if !strings.Contains(out, "collection @tinchoram by @tinchoram") {
		t.Fatalf("own collection header missing:\n%s", out)
	}
	if !strings.Contains(out, "collection VoltAgent awesome-claude-code-subagents by VoltAgent/awesome-claude-code-subagents · license MIT · validated by @tinchoram") {
		t.Fatalf("public source header should carry the license and validator:\n%s", out)
	}
	if strings.Index(out, "@tinchoram") > strings.Index(out, "VoltAgent") {
		t.Fatalf("own collection must come first:\n%s", out)
	}
}

func TestAgentLibraryFiltersOneSource(t *testing.T) {
	m, _ := newTestModel(t, &fakeSession{model: "m"})
	m = sendLine(t, m, "/agent library VoltAgent awesome-claude-code-subagents")
	out := m.transcriptText()
	if !strings.Contains(out, "devops-engineer") || strings.Contains(out, "collection @tinchoram") {
		t.Fatalf("filter should keep only that source:\n%s", out)
	}
}

func TestAgentShowQualifiedShowsDescriptionAndPrompt(t *testing.T) {
	m, _ := newTestModel(t, &fakeSession{model: "m"})
	m = sendLine(t, m, "/agent show VoltAgent awesome-claude-code-subagents/devops-engineer")
	out := m.transcriptText()
	if !strings.Contains(out, "Pipelines and containers") || !strings.Contains(out, "You build pipelines.") {
		t.Fatalf("show should print the description and the prompt:\n%s", out)
	}
}

func TestAgentInstallCopiesAndAssigns(t *testing.T) {
	m, ws := newTestModel(t, &fakeSession{model: "m"})
	m = sendLine(t, m, "/agent install sre default")
	if !strings.Contains(m.transcriptText(), "installed sre") {
		t.Fatalf("install not reported:\n%s", m.transcriptText())
	}
	if _, err := ws.Agent("sre"); err != nil {
		t.Fatal("the agent was not installed")
	}
}

func TestAgentInstallRefusesExistingUnlessForced(t *testing.T) {
	m, ws := newTestModel(t, &fakeSession{model: "m"})
	ws.catalog = append(ws.catalog, workspace.AgentInfo{Name: "sre"})
	m = sendLine(t, m, "/agent install sre")
	if !strings.Contains(m.transcriptText(), "already exists") {
		t.Fatalf("refusal not shown:\n%s", m.transcriptText())
	}
	m = sendLine(t, m, "/agent install sre --force")
	if !strings.Contains(m.transcriptText(), "installed sre") {
		t.Fatalf("--force should replace:\n%s", m.transcriptText())
	}
}

func TestAgentInstallNeedsAName(t *testing.T) {
	m, _ := newTestModel(t, &fakeSession{model: "m"})
	m = sendLine(t, m, "/agent install")
	if !strings.Contains(m.transcriptText(), "usage: /agent install") {
		t.Fatalf("usage not shown: %s", m.transcriptText())
	}
}
