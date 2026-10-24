package ui

import (
	"errors"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/tinchoram/groot-cli/internal/term"
)

// termModel builds a wide model whose terminal factory hands out the given
// fakes in order. Each ctrl-t that starts a shell takes the next fake.
func termModel(t *testing.T, fakes ...*term.Fake) (Model, *fakeWorkspace) {
	t.Helper()
	chat := &fakeSession{model: "m"}
	chat.messages = 10
	ws := newFakeWorkspace(chat)
	next := 0
	factory := func() term.Terminal {
		f := fakes[next%len(fakes)]
		next++
		return f
	}
	m := NewModel(Options{Workspace: ws, Bridge: NewBridge(), NewTerminal: factory})
	m.width, m.height = 120, 40
	m.resize()
	return m, ws
}

func TestCtrlTOpensShellInActiveSpaceDirectory(t *testing.T) {
	fake := term.NewFake("user@host ~ %")
	m, _ := termModel(t, fake)
	m, cmd := press(m, tea.KeyCtrlT)
	if cmd == nil {
		t.Fatal("opening the terminal must wait for its output")
	}
	if !fake.Started || fake.Dir != "/work/default" {
		t.Fatalf("shell started=%v dir=%q", fake.Started, fake.Dir)
	}
	if !m.termOpen || !m.focusTerm {
		t.Fatal("ctrl-t should show the column and focus it")
	}
	if !strings.Contains(m.View(), "terminal · default") {
		t.Fatalf("title missing:\n%s", m.View())
	}
}

func TestTerminalColumnTakesFortyFivePercent(t *testing.T) {
	m, _ := termModel(t, term.NewFake())
	m, _ = press(m, tea.KeyCtrlT)
	// 45% of 120 is 54, but the chat keeps at least 40 columns, so the
	// terminal stops at 120-28-40 = 52.
	if got := m.termWidth(); got != 52 {
		t.Fatalf("terminal width = %d, want 52", got)
	}
	if got := m.chatWidth(); got != 40 {
		t.Fatalf("chat width = %d, want the 40-column minimum", got)
	}
}

func TestCtrlTAgainHidesButKeepsShellRunning(t *testing.T) {
	fake := term.NewFake()
	m, _ := termModel(t, fake)
	m, _ = press(m, tea.KeyCtrlT)
	m, _ = press(m, tea.KeyCtrlT)
	if m.termOpen || m.termWidth() != 0 {
		t.Fatal("second ctrl-t should hide the column")
	}
	if fake.Closed {
		t.Fatal("hiding must keep the shell running")
	}
	m, _ = press(m, tea.KeyCtrlT)
	if !m.termOpen {
		t.Fatal("third ctrl-t should show the column again")
	}
}

func TestTabCyclesForwardOutsideTheTerminal(t *testing.T) {
	m, _ := termModel(t, term.NewFake())
	m, _ = press(m, tea.KeyCtrlT)    // terminal
	m, _ = press(m, tea.KeyShiftTab) // -> input
	steps := []struct {
		key  tea.KeyType
		want string
	}{
		{tea.KeyTab, "terminal"},
		{tea.KeyShiftTab, "input"},
		{tea.KeyShiftTab, "agents"},
		{tea.KeyTab, "input"},
	}
	for _, s := range steps {
		m, _ = press(m, s.key)
		if got := paneName(m); got != s.want {
			t.Fatalf("after %v focus = %s, want %s", s.key, got, s.want)
		}
	}
}

func TestTabInTerminalReachesShell(t *testing.T) {
	fake := term.NewFake()
	m, _ := termModel(t, fake)
	m, _ = press(m, tea.KeyCtrlT)
	m, _ = press(m, tea.KeyTab)
	if string(fake.Written) != "\t" {
		t.Fatalf("shell received %q, want a tab (0x09)", fake.Written)
	}
	if paneName(m) != "terminal" {
		t.Fatalf("tab must not move focus out of the terminal, now %s", paneName(m))
	}
}

func TestShiftTabLeavesTerminal(t *testing.T) {
	fake := term.NewFake()
	m, _ := termModel(t, fake)
	m, _ = press(m, tea.KeyCtrlT)
	m, _ = press(m, tea.KeyShiftTab)
	if paneName(m) != "input" {
		t.Fatalf("shift+tab from the terminal should reach the input, got %s", paneName(m))
	}
	if len(fake.Written) != 0 {
		t.Fatalf("shift+tab must not reach the shell: %q", fake.Written)
	}
}

func TestCtrlTHideLandsFocusOnInput(t *testing.T) {
	m, _ := termModel(t, term.NewFake())
	m, _ = press(m, tea.KeyCtrlT)
	m, _ = press(m, tea.KeyCtrlT)
	if paneName(m) != "input" {
		t.Fatalf("hiding the terminal should focus the input, got %s", paneName(m))
	}
}

func paneName(m Model) string {
	switch {
	case m.focusTerm:
		return "terminal"
	case m.focusSidebar && m.section == sectionAgents:
		return "agents"
	case m.focusSidebar:
		return "spaces"
	}
	return "input"
}

func TestTerminalReceivesTypedKeys(t *testing.T) {
	fake := term.NewFake()
	m, _ := termModel(t, fake)
	m, _ = press(m, tea.KeyCtrlT)
	m = typeText(m, "ls")
	m, _ = press(m, tea.KeyEnter)
	if string(fake.Written) != "ls\r" {
		t.Fatalf("shell received %q, want \"ls\\r\"", fake.Written)
	}
	if m.input.Value() != "" {
		t.Fatal("keys typed into the terminal must not reach the chat input")
	}
}

func TestCtrlCInTerminalGoesToShellNotQuit(t *testing.T) {
	fake := term.NewFake()
	m, ws := termModel(t, fake)
	m, _ = press(m, tea.KeyCtrlT)
	_, cmd := press(m, tea.KeyCtrlC)
	if cmd != nil {
		if _, ok := cmd().(tea.QuitMsg); ok {
			t.Fatal("ctrl-c in the terminal must not quit groot")
		}
	}
	if string(fake.Written) != "\x03" {
		t.Fatalf("shell received %q, want ctrl-c", fake.Written)
	}
	if ws.closed {
		t.Fatal("groot must not close while the terminal has focus")
	}
}

func TestCtrlDInTerminalGoesToShell(t *testing.T) {
	fake := term.NewFake()
	m, _ := termModel(t, fake)
	m, _ = press(m, tea.KeyCtrlT)
	_, cmd := press(m, tea.KeyCtrlD)
	if cmd != nil {
		if _, ok := cmd().(tea.QuitMsg); ok {
			t.Fatal("ctrl-d in the terminal must not quit groot")
		}
	}
	if string(fake.Written) != "\x04" {
		t.Fatalf("shell received %q, want ctrl-d", fake.Written)
	}
}

func TestCtrlBWhileTerminalFocusedTogglesSidebar(t *testing.T) {
	fake := term.NewFake()
	m, _ := termModel(t, fake)
	m, _ = press(m, tea.KeyCtrlT)
	m, _ = press(m, tea.KeyCtrlB)
	if m.sidebarOpen {
		t.Fatal("ctrl-b should toggle the sidebar even from the terminal")
	}
	if len(fake.Written) != 0 {
		t.Fatalf("ctrl-b must not be sent to the shell: %q", fake.Written)
	}
}

func TestWindowResizeResizesShell(t *testing.T) {
	fake := term.NewFake()
	m, _ := termModel(t, fake)
	m, _ = press(m, tea.KeyCtrlT)
	next, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	_ = next.(Model)
	if fake.Cols != 52-2 || fake.Rows != 40-2 {
		t.Fatalf("shell size = %dx%d, want %dx%d", fake.Cols, fake.Rows, 52-2, 40-2)
	}
}

func TestTerminalOutputRedraws(t *testing.T) {
	fake := term.NewFake()
	m, _ := termModel(t, fake)
	m, _ = press(m, tea.KeyCtrlT)
	fake.Emit("user@host ~ % ls")
	next, _ := m.Update(termOutputMsg{})
	m = next.(Model)
	if !strings.Contains(m.View(), "user@host ~ % ls") {
		t.Fatalf("new shell output not drawn:\n%s", m.View())
	}
}

func TestShellExitClosesColumnAndNextCtrlTStartsAnother(t *testing.T) {
	first, second := term.NewFake(), term.NewFake()
	m, _ := termModel(t, first, second)
	m, _ = press(m, tea.KeyCtrlT)
	first.Exit()
	next, _ := m.Update(termOutputMsg{})
	m = next.(Model)
	if m.termOpen || m.focusTerm {
		t.Fatal("a finished shell must close its column")
	}
	if !first.Closed {
		t.Fatal("the finished shell must be released")
	}
	if !strings.Contains(m.transcriptText(), "terminal closed") {
		t.Fatalf("user should be told: %s", m.transcriptText())
	}
	m, _ = press(m, tea.KeyCtrlT)
	if !second.Started {
		t.Fatal("ctrl-t after an exit must start a new shell")
	}
}

func TestQuitClosesShell(t *testing.T) {
	fake := term.NewFake()
	m, ws := termModel(t, fake)
	m, _ = press(m, tea.KeyCtrlT)
	m, _ = press(m, tea.KeyShiftTab) // terminal -> input
	_, cmd := press(m, tea.KeyCtrlC)
	if !fake.Closed || !ws.fastClosed {
		t.Fatal("quitting must close the shell and the workspace")
	}
	if _, ok := cmd().(tea.QuitMsg); !ok {
		t.Fatal("expected QuitMsg")
	}
}

func TestTerminalStartFailureIsReported(t *testing.T) {
	fake := term.NewFake()
	fake.StartErr = errors.New("no pty")
	m, _ := termModel(t, fake)
	m, _ = press(m, tea.KeyCtrlT)
	if m.termOpen {
		t.Fatal("a shell that cannot start must not open a column")
	}
	if !strings.Contains(m.transcriptText(), "error: could not start the shell: no pty") {
		t.Fatalf("failure not shown: %s", m.transcriptText())
	}
}

func TestTerminalHiddenWhenChatHidden(t *testing.T) {
	m, _ := termModel(t, term.NewFake())
	m, _ = press(m, tea.KeyCtrlT)
	m, _ = press(m, tea.KeyCtrlE)
	if m.termWidth() != 0 {
		t.Fatal("the terminal column is shown only beside the chat")
	}
}
