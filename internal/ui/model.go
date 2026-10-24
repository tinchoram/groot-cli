// Package ui is the Bubble Tea terminal interface. It renders the sidebar and
// the chat and forwards user intent to a Workspace. No agent logic lives here.
package ui

import (
	"context"
	"fmt"
	"github.com/tinchoram/groot-cli/internal/feedback"
	"strconv"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/textarea"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/tinchoram/groot-cli/internal/clipboard"
	"github.com/tinchoram/groot-cli/internal/gate"
	"github.com/tinchoram/groot-cli/internal/inbox"
	"github.com/tinchoram/groot-cli/internal/provider"
	"github.com/tinchoram/groot-cli/internal/term"
	"github.com/tinchoram/groot-cli/internal/workspace"
)

// Session is one running conversation, as the chat sees it.
type Session = workspace.Chat

// Workspace is what the UI needs from the spaces-and-agents runtime.
// *workspace.Workspace implements it.
type Workspace interface {
	Spaces() []workspace.SpaceInfo
	ActiveSpace() (workspace.SpaceInfo, bool)
	Agents() []workspace.AgentInfo
	CatalogAgents() []workspace.AgentInfo
	ActiveAgent() string
	ConversationKey() string
	Chat() Session
	UseSpace(name string) error
	SelectAgent(name string) error
	NewSpace(name, path string) error
	AssignAgent(name string) error
	UnassignAgent(name string) error
	NewAgent(name, description string) (string, error)
	ImportAgent(ctx context.Context, source string, overwrite bool) (workspace.ImportResult, error)
	LibraryAgents() ([]workspace.LibraryInfo, error)
	LibraryAgent(name string) (workspace.LibraryDetail, error)
	InstallLibraryAgent(name, space string, overwrite bool) error
	Agent(name string) (workspace.AgentInfo, error)
	UnreadCounts(ctx context.Context) (map[string]int, error)
	Inbox(ctx context.Context, limit int) ([]inbox.Message, error)
	SendByHand(ctx context.Context, to, subject, body string) (string, error)
	UpgradeAgents(name string) ([]string, error)
	SendFeedback(spaceName, agent, kind, text string, lines []string) (int64, string, error)
	Backlog(status string) ([]feedback.Item, error)
	WorkModes() map[string]bool
	WorkLimits() (int, float64)
	GrantSession(key, tool string) error
	SessionGrants(key string) []string
	ClearSessionGrants(key string)
	ListModels(ctx context.Context) ([]provider.ModelInfo, error)
	CheckModel(ctx context.Context, id string, force bool) (string, error)
	SaveAgentModel(agent, model string) error
	DefaultModel() string
	SetDefaultModel(ctx context.Context, model string, force bool) (string, error)
	ConfigFile() string
	AgentTools(name string) ([]string, error)
	EditAgentTools(name string, add, remove []string) error
	Close(ctx context.Context) error
	CloseFast() error
}

// Options wires a Model.
type Options struct {
	Workspace Workspace
	Bridge    *Bridge
	Version   string
	// History is the prompt history. Nil means an empty in-memory history.
	History *History
	// HistoryPath is the file the history is saved to. Empty disables saving.
	HistoryPath string
	// ShowWelcome adds the first-run welcome to the admin agent's chat.
	ShowWelcome bool
	// NewTerminal makes the shell for the terminal column. Nil uses a real
	// pseudo-terminal running $SHELL.
	NewTerminal func() term.Terminal
	// Clipboard receives copied text. Nil uses the system clipboard.
	Clipboard clipboard.Clipboard
	// LayoutPath is the file the columns are saved to. Empty disables saving.
	LayoutPath string
	// Layout is the saved layout to restore at start. Nil uses the defaults.
	Layout *Layout
}

// welcomeLines is the static first-run block shown in the admin agent's chat.
// It makes no model call.
var welcomeLines = []string{
	"Hi, I'm groot, the administrator of this CLI.",
	"I can create spaces (one per project or directory) and agents (one per job),",
	"and explain how things work. Ask me, for example: create a space for my project.",
	"Keys: tab moves between the sidebar and the input · ctrl-b shows or hides the sidebar",
	"      · ctrl-e shows or hides the chat · /help lists the slash commands.",
}

// Internal messages.
type (
	streamMsg   struct{ key, delta string }
	toolCallMsg struct{ key, name, preview string }
	warnMsg     struct{ key, text string }
	confirmMsg  struct {
		key   string
		req   gate.Request
		reply chan bool
	}
	answerMsg struct {
		key  string
		text string
		err  error
	}
	noteMsg struct {
		text string
		err  error
	}
	shineTickMsg struct{}
	closedMsg    struct{ err error }
	autoMsg      struct{ key, name, preview string }
	badgeMsg     struct{}
)

type confirmState struct {
	key    string // conversation the approval belongs to
	prompt string
	req    gate.Request
	reply  chan bool
}

// Vertical budget of the chat column: header, status line, input box (3), hint.
const chromeRows = 6

// closeBudget bounds the model summaries written when the session closes.
const closeBudget = 5 * time.Second

// sidebarWidth is the outer width of the sidebar when it sits beside the chat.
const sidebarWidth = 28

// autoSidebarMinWidth is the narrowest terminal that starts with the sidebar open.
const autoSidebarMinWidth = 90

// sidebarSection is the part of the sidebar that has the keyboard cursor.
type sidebarSection int

const (
	sectionSpaces sidebarSection = iota
	sectionAgents
)

// Model is the Bubble Tea model.
type Model struct {
	ws      Workspace
	session Session
	bridge  *Bridge
	version string

	// key identifies the active conversation. transcripts keeps the lines of
	// every other conversation, so switching agents restores its chat.
	key         string
	transcripts map[string][]string
	// runs holds the state of the conversations that are off screen: whether they
	// work, wait for an approval, and their streamed text. cancels ends the
	// running turns at shutdown.
	runs    map[string]*run
	cancels map[string]context.CancelFunc
	// restored marks the conversations whose stored transcript was drawn this run.
	restored map[string]bool

	hist        *History
	historyPath string

	input textarea.Model
	spin  spinner.Model
	vp    viewport.Model // chat transcript

	width, height int
	lines         []string // active transcript, already styled
	partial       string   // assistant text still streaming
	busy          bool
	pending       *confirmState
	endErr        error
	closing       bool               // the session is closing: summaries are running
	closeCancel   context.CancelFunc // skips the summaries still running
	unread        map[string]int
	ticking       bool      // the spinner tick chain runs
	lastPoll      time.Time // when the badges were last re-read     // unread counts per "space/agent", for the sidebar badges

	newTerm   func() term.Terminal
	tm        term.Terminal // the shell, nil until ctrl-t first opens it
	termOpen  bool          // the terminal column is wanted on screen
	focusTerm bool

	pastes   []pastedText // long pastes in the input, by placeholder
	pasteSeq int          // numbers the pastes of the message being typed

	sidebarOpen  bool
	chatOpen     bool
	focusSidebar bool
	sized        bool
	section      sidebarSection
	cursor       int

	md           []string // raw Markdown of finished assistant messages
	clip         clipboard.Clipboard
	layoutPath   string
	sidebarW     int      // stored sidebar width, 0 for the default
	termW        int      // stored terminal width, 0 for the default share
	contentPlain []string // the chat content as plain rows, for selection

	sel          selection
	selArea      area
	selecting    bool
	drag         dragKind // divider being dragged
	hover        dragKind // divider under the pointer
	forwardMouse bool     // a press in the terminal went to the shell's program

	clickCount             int
	lastClickAt            time.Time
	lastClickX, lastClickY int

	note      string
	noteUntil time.Time

	shineOn   bool
	shineDone bool
	shineTick int
	shineCol  int
}

// NewModel builds the initial model for the workspace's active conversation.
func NewModel(o Options) Model {
	ti := newInputArea()

	sp := spinner.New()
	sp.Spinner = spinner.MiniDot
	sp.Style = styleAccentBold

	vp := viewport.New(80, 20)
	vp.MouseWheelEnabled = true

	hist := o.History
	if hist == nil {
		hist = NewHistory(nil, HistoryCap)
	}
	newTerm := o.NewTerminal
	if newTerm == nil {
		newTerm = term.NewPTY
	}
	m := Model{
		newTerm:     newTerm,
		hist:        hist,
		historyPath: o.HistoryPath,
		ws:          o.Workspace,
		bridge:      o.Bridge,
		version:     o.Version,
		transcripts: map[string][]string{},
		restored:    map[string]bool{},
		runs:        map[string]*run{},
		cancels:     map[string]context.CancelFunc{},
		input:       ti,
		spin:        sp,
		vp:          vp,
		width:       80,
		height:      24,
		sidebarOpen: true,
		chatOpen:    true,
		shineCol:    -shineEdgePad,
	}
	m.clip = o.Clipboard
	if m.clip == nil {
		m.clip = clipboard.Default()
	}
	m.layoutPath = o.LayoutPath
	if o.Layout != nil {
		m.sidebarW = o.Layout.SidebarWidth
		m.termW = o.Layout.TerminalWidth
		m.sidebarOpen = o.Layout.SidebarVisible
		m.sized = true
	}
	m.syncConversation()
	if o.ShowWelcome && m.ws.ActiveAgent() == managedAgent && len(m.lines) == 0 {
		for _, l := range welcomeLines {
			m.lines = append(m.lines, styleDim.Render(l))
		}
	}
	m.resize()
	if o.Layout != nil && o.Layout.TerminalVisible {
		m.toggleTerminal()
		m.focusTerm = false
	}
	m.refreshUnread()
	return m
}

// managedAgent is the administrator agent's name in the catalog.
const managedAgent = "groot"

// Init starts the cursor blink.
func (m Model) Init() tea.Cmd {
	if m.tm != nil && m.termOpen {
		return tea.Batch(textarea.Blink, waitTerminalOutput(m.tm))
	}
	return textarea.Blink
}

// Update applies one message.
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	// An event of a conversation that is off screen is applied to that
	// conversation's state: it is swapped in, handled as if it were on screen,
	// and swapped back out.
	if key := msgKey(msg); key != "" && key != m.key {
		var cmd tea.Cmd
		m.onKey(key, func() {
			var next tea.Model
			next, cmd = m.Update(msg)
			m = next.(Model)
		})
		return m, cmd
	}
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		if !m.sized {
			m.sized = true
			m.sidebarOpen = m.width >= autoSidebarMinWidth
		}
		m.resize()
		if !m.shineOn && !m.shineDone && m.width >= bannerMinWidth {
			m.shineOn = true
			return m, shineTickCmd()
		}
		return m, nil

	case autoMsg:
		m.flushPartial()
		m.append(styleTool.Render(autoLine(msg.name, msg.preview)))
		return m, nil

	case badgeMsg:
		m.refreshUnread()
		return m, m.startTicks()

	case closedMsg:
		m.endErr = msg.err
		return m, tea.Quit

	case shineTickMsg:
		return m.advanceShine()

	case spinner.TickMsg:
		m.poll()
		if !m.anyBusy() {
			m.ticking = false
			return m, nil
		}
		var cmd tea.Cmd
		m.spin, cmd = m.spin.Update(msg)
		return m, cmd

	case streamMsg:
		if !m.busy {
			return m, nil
		}
		m.partial += msg.delta
		m.refresh()
		return m, nil

	case toolCallMsg:
		m.flushPartial()
		if !m.busy {
			return m, nil
		}
		line := msg.preview
		if i := strings.IndexByte(line, '\n'); i >= 0 {
			line = line[:i]
		}
		m.append(styleTool.Render("[tool] " + line))
		return m, nil

	case warnMsg:
		m.append(styleDim.Render(warningLine(msg.text)))
		if strings.HasPrefix(msg.text, "⟶") {
			m.busy = true
			return m, m.startTicks()
		}
		return m, nil

	case confirmMsg:
		m.flushPartial()
		m.pending = &confirmState{key: msg.key, prompt: approvalPrompt(msg.req), req: msg.req, reply: msg.reply}
		m.resize()
		return m, nil

	case answerMsg:
		m.busy = false
		// The final answer is authoritative: it replaces the streamed text.
		if msg.text != "" {
			m.partial = ""
			m.addMarkdown(msg.text)
		} else {
			m.flushPartial()
		}
		m.refresh()
		if msg.err != nil {
			m.append(styleError.Render("error: " + msg.err.Error()))
		}
		// The turn may have created or changed spaces and agents, so the
		// selection and the sidebar are re-read from the workspace.
		m.finishTurn(msg.key)
		m.refreshUnread()
		return m, nil

	case noteMsg:
		m.busy = false
		if msg.err != nil {
			m.append(styleError.Render("error: " + msg.err.Error()))
		} else {
			m.append(msg.text)
		}
		return m, nil

	case termOutputMsg:
		return m, m.handleTerminalOutput()

	case tea.MouseMsg:
		return m.handleMouse(msg)

	case clearNoteMsg:
		if !time.Now().Before(m.noteUntil) {
			m.note = ""
		}
		return m, nil

	case tea.KeyMsg:
		if m.closing {
			if msg.Type == tea.KeyEsc && m.closeCancel != nil {
				m.closeCancel()
			}
			return m, nil
		}
		return m.handleKeyFitted(msg)
	}
	return m, nil
}

func (m Model) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if m.pending != nil {
		return m.handleConfirmKey(msg)
	}
	if msg.Paste {
		return m.handlePaste(msg)
	}
	m.clearSelection()
	switch msg.Type {
	case tea.KeyCtrlT:
		return m, m.toggleTerminal()
	case tea.KeyCtrlB:
		m.toggleSidebar()
		return m, nil
	case tea.KeyCtrlE:
		m.toggleChat()
		return m, nil
	case tea.KeyShiftTab:
		m.cycleFocus(true)
		return m, nil
	case tea.KeyTab:
		if !m.focusTerm {
			m.cycleFocus(false)
			return m, nil
		}
	}
	if m.focusTerm {
		return m.handleTerminalKey(msg)
	}
	if msg.Type == tea.KeyBackspace {
		if next, ok := m.removePasteAtCursor(); ok {
			return next, nil
		}
	}
	switch msg.Type {
	case tea.KeyCtrlC:
		return m.quitFast()
	case tea.KeyCtrlD:
		return m.quit()
	}
	if m.focusSidebar {
		return m.handleSidebarKey(msg)
	}
	if isNewlineKey(msg) {
		m.input.InsertString("\n")
		return m, nil
	}
	switch msg.Type {
	case tea.KeyEnter:
		return m.submit()
	case tea.KeyUp:
		// Up leaves the history only from the first line of the input.
		if m.onFirstInputLine() {
			return m.recall(true)
		}
	case tea.KeyDown:
		// Down leaves the history only from the last line of the input.
		if m.onLastInputLine() {
			return m.recall(false)
		}
	case tea.KeyEsc:
		if m.hist.Browsing() {
			m.input.SetValue(m.hist.Reset())
		}
		return m, nil
	case tea.KeyPgUp, tea.KeyPgDown, tea.KeyHome, tea.KeyEnd:
		var cmd tea.Cmd
		m.vp, cmd = m.vp.Update(msg)
		return m, cmd
	}
	var cmd tea.Cmd
	m.input, cmd = m.input.Update(msg)
	return m, cmd
}

// handleConfirmKey resolves the pending approval. y allows; n, esc, enter and
// ctrl-c deny. d prints the full detail (the diff) into the transcript.
func (m Model) handleConfirmKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch strings.ToLower(msg.String()) {
	case "d":
		m.showDiff()
	case "y":
		m.answer(true)
	case "a":
		m.answerForSession()
	case "n", "esc", "enter":
		m.answer(false)
	case "ctrl+c":
		m.answer(false)
		return m.quitFast()
	case "ctrl+d":
		m.answer(false)
		return m.quit()
	}
	return m, nil
}

func (m *Model) answer(allow bool) {
	if m.pending == nil {
		return
	}
	m.pending.reply <- allow
	verdict := "denied"
	if allow {
		verdict = "approved"
	}
	m.append(styleDim.Render("  ↳ " + verdict))
	m.pending = nil
	m.resize()
}

// recall moves through the prompt history: up goes back, down goes forward
// and past the newest entry restores the draft.
func (m Model) recall(older bool) (tea.Model, tea.Cmd) {
	var line string
	var ok bool
	if older {
		line, ok = m.hist.Prev(m.input.Value())
	} else {
		line, ok = m.hist.Next()
	}
	if ok {
		m.input.SetValue(line)
	}
	return m, nil
}

func (m Model) submit() (tea.Model, tea.Cmd) {
	text := strings.TrimSpace(m.input.Value())
	full := expandPastes(text, m.pastes)
	echoLines := pasteEchoLines(m.pastes)
	m.input.Reset()
	m.pastes, m.pasteSeq = nil, 0
	if text == "" {
		return m, nil
	}
	if m.busy {
		m.append(styleDim.Render("agent is working; wait or switch"))
		return m, nil
	}
	m.hist.Push(full)
	if m.historyPath != "" {
		if err := SaveHistory(m.historyPath, m.hist.Entries()); err != nil {
			m.append(styleDim.Render("warning: could not save prompt history: " + err.Error()))
		}
	}
	if cmd, ok := ParseCommand(full); ok {
		return m.runCommand(cmd)
	}

	m.append("\n" + turnHeader(m.vp.Width))
	// The echo prints the lines as typed: the prompt on the first, an indent after.
	lines := strings.Split(text, "\n")
	for i, l := range lines {
		prefix := "  "
		if i == 0 {
			prefix = stylePrompt.Render("❯ ")
		}
		end := ""
		if i == len(lines)-1 {
			end = "\n"
		}
		m.append(prefix + stylePromptText.Render(l) + end)
	}
	for _, l := range echoLines {
		m.append(l)
	}
	m.busy = true
	m.vp.GotoBottom()
	m.refresh()
	ctx := m.startTurn(m.key)
	return m, tea.Batch(m.sendCmd(ctx, m.key, full), m.startTicks())
}

// sendCmd runs one turn of conversation key. Bubble Tea runs each command in
// its own goroutine, so turns of different conversations run side by side.
func (m Model) sendCmd(ctx context.Context, key, prompt string) tea.Cmd {
	s := m.session
	bridge := m.bridge
	return func() tea.Msg {
		reply, err := s.Send(ctx, prompt)
		if bridge != nil {
			bridge.Done(key, reply, err)
			return nil
		}
		return answerMsg{key: key, text: reply, err: err}
	}
}

func (m Model) runCommand(cmd Command) (tea.Model, tea.Cmd) {
	if !cmd.Known() {
		m.append(styleError.Render(fmt.Sprintf("unknown command /%s, try /help", cmd.Name)))
		return m, nil
	}
	switch cmd.Name {
	case "help":
		m.append(helpBlock())
	case "space":
		m.runSpaceCommand(cmd.Arg)
	case "agent":
		if c := m.runAgentCommand(cmd.Arg); c != nil {
			m.refresh()
			return m, c
		}
	case "model":
		m.runModelCommand(cmd.Arg)
	case "models":
		m.runModelsCommand()
	case "compact":
		before := m.session.MessageCount()
		m.busy = true
		s := m.session
		m.refresh()
		return m, tea.Batch(m.spin.Tick, func() tea.Msg {
			if err := s.Compact(context.Background()); err != nil {
				return noteMsg{err: err}
			}
			return noteMsg{text: compactionNotice(before, s.MessageCount())}
		})
	case "tokens":
		m.append(usageBlock(m.session.Usage(), m.session.CostUSD()))
	case "clear":
		done := m.session.Clear()
		m.lines = nil
		m.partial = ""
		m.refresh()
		// Ending the old session writes its summary, which calls the model,
		// so it runs off the UI loop.
		return m, func() tea.Msg {
			if err := done(context.Background()); err != nil {
				return noteMsg{err: err}
			}
			return noteMsg{text: styleDim.Render("conversation cleared")}
		}
	case "inbox":
		m.runInboxCommand(cmd.Arg)
	case "work":
		m.runWorkCommand(cmd.Arg)
	case "allow":
		m.runAllowCommand(cmd.Arg)
	case "bug", "feat":
		kind := feedback.TypeBug
		if cmd.Name == "feat" {
			kind = feedback.TypeFeat
		}
		m.reportFeedback(kind, cmd.Arg)
	case "backlog":
		m.showBacklog(cmd.Arg)
	case "diff":
		m.showDiff()
	case "task":
		m.runTaskCommand(cmd.Arg)
	case "exit":
		return m.quit()
	}
	m.refresh()
	return m, nil
}

// quit ends the session for ctrl-d and /exit. Turns are cancelled, approvals
// denied, and every conversation gets a model summary, within closeBudget. The
// status line says that the session is closing, and esc skips the summaries.
func (m Model) quit() (tea.Model, tea.Cmd) {
	m.denyAll()
	if m.pending != nil {
		m.answer(false)
	}
	m.closeTerminal()
	ctx, cancel := context.WithTimeout(context.Background(), closeBudget)
	m.closeCancel = cancel
	m.closing = true
	ws := m.ws
	return m, func() tea.Msg {
		err := ws.Close(ctx)
		cancel()
		return closedMsg{err: err}
	}
}

// quitFast ends the session at once, for ctrl-c. Turns are cancelled, approvals
// denied, every conversation keeps a local summary and no model is called, and
// the shell is hung up when its terminal closes.
func (m Model) quitFast() (tea.Model, tea.Cmd) {
	m.denyAll()
	if m.pending != nil {
		m.answer(false)
	}
	m.closeTerminal()
	m.endErr = m.ws.CloseFast()
	return m, tea.Quit
}

// advanceShine moves the startup sweep one frame, then settles it.
func (m Model) advanceShine() (tea.Model, tea.Cmd) {
	if m.shineDone {
		return m, nil
	}
	m.shineTick++
	if m.shineTick > shineTotalTicks {
		m.shineCol = shineDisabled
		m.shineDone = true
		m.refresh()
		return m, nil
	}
	progress := float64(m.shineTick) / float64(shineTotalTicks)
	m.shineCol = int(easeInOutCubic(progress)*float64(bannerWidthCells+shineEdgePad*2)) - shineEdgePad
	m.refresh()
	return m, shineTickCmd()
}

// bannerWidthCells is the wordmark width, used to size the sweep.
const bannerWidthCells = 49

func shineTickCmd() tea.Cmd {
	return tea.Tick(shineFrameMs*time.Millisecond, func(time.Time) tea.Msg {
		return shineTickMsg{}
	})
}

func (m *Model) append(line string) {
	m.lines = append(m.lines, line)
	m.refresh()
}

func (m *Model) flushPartial() {
	if m.partial == "" {
		return
	}
	m.addMarkdown(m.partial)
	m.partial = ""
}

// mdMarker prefixes a transcript line that stands for a Markdown block. The
// block itself lives in Model.md and is rendered at the current width when the
// screen is drawn, so the text re-wraps on resize.
const mdMarker = "\x00md:"

// addMarkdown appends a finished assistant message to the transcript.
func (m *Model) addMarkdown(raw string) {
	m.md = append(m.md, raw)
	m.lines = append(m.lines, mdMarker+strconv.Itoa(len(m.md)-1))
}

// expandLines replaces the Markdown markers with their rendering at width.
func (m Model) expandLines(width int) []string {
	var out []string
	for _, l := range m.lines {
		if strings.HasPrefix(l, mdMarker) {
			if idx, err := strconv.Atoi(l[len(mdMarker):]); err == nil && idx >= 0 && idx < len(m.md) {
				out = append(out, strings.Split(renderMarkdown(m.md[idx], width), "\n")...)
				continue
			}
		}
		out = append(out, l)
	}
	return out
}

// contentWidth is the width Markdown is rendered at: the chat's inner width.
func (m Model) contentWidth() int {
	if m.vp.Width > 0 {
		return m.vp.Width
	}
	return 80
}

// chatWidth is the width left for the chat column.
func (m Model) chatWidth() int {
	if !m.chatOpen {
		return m.width
	}
	w := m.width - m.termWidth()
	if m.sidebarOpen {
		w -= m.sidebarCols()
	}
	return max(w, 20)
}

// resize recomputes the chat and modal sizes from the window size.
func (m *Model) resize() {
	cw := m.chatWidth()
	m.input.SetWidth(max(cw-6, 10))
	rows := m.inputRows()
	m.input.SetHeight(rows)
	// The input may take more rows than the one chromeRows budgets for.
	vpHeight := max(m.height-chromeRows-(rows-1), 3)
	m.vp.Width = cw
	m.vp.Height = vpHeight

	m.resizeTerminal()
	m.refresh()
}

// refresh rebuilds the transcript viewport. The banner is not wrapped; the
// lines after it are wrapped to the viewport width.
func (m *Model) refresh() {
	body := strings.Join(m.expandLines(m.contentWidth()), "\n")
	if m.partial != "" {
		if body != "" {
			body += "\n"
		}
		body += m.partial
	}
	if m.vp.Width > 0 {
		body = lipgloss.NewStyle().Width(m.vp.Width).Render(body)
	}
	atBottom := m.vp.AtBottom()
	content := m.banner() + body
	m.contentPlain = strings.Split(stripCells(content), "\n")
	if m.sel.active && m.selArea == areaChat {
		rows := highlightRows(strings.Split(content, "\n"), m.contentPlain, m.sel)
		content = strings.Join(rows, "\n")
	}
	m.vp.SetContent(content)
	if atBottom {
		m.vp.GotoBottom()
	}
}

// banner returns the banner for the current width and sweep position.
func (m Model) banner() string {
	shine := m.shineCol
	if !m.shineOn {
		shine = shineDisabled
	}
	return bannerView(m.chatWidth(), shine)
}

// transcriptText returns the plain transcript, used by tests.
func (m Model) transcriptText() string {
	out := m.expandLines(m.contentWidth())
	if m.partial != "" {
		out = append(out, m.partial)
	}
	return strings.Join(out, "\n")
}

// turnHeader is the dim "── 15:04:05 ───" divider printed before each prompt.
func turnHeader(width int) string {
	return turnHeaderAt(width, time.Now())
}

// turnHeaderAt is the divider of a turn written at the given time.
func turnHeaderAt(width int, at time.Time) string {
	if width < 24 {
		width = 80
	}
	left := "── "
	label := " " + at.Format("15:04:05") + " "
	tail := width - lipgloss.Width(left) - lipgloss.Width(label)
	if tail < 3 {
		tail = 3
	}
	return styleDivider.Render(left) + styleTimestamp.Render(label) + styleDivider.Render(strings.Repeat("─", tail))
}
