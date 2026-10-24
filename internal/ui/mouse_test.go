package ui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/tinchoram/groot-cli/internal/clipboard"
	"github.com/tinchoram/groot-cli/internal/term"
)

type mouseFixture struct {
	m      Model
	ws     *fakeWorkspace
	clip   *clipboard.Fake
	layout string
	shell  *term.Fake
}

func newMouseFixture(t *testing.T, shell *term.Fake, opts ...func(*Options)) *mouseFixture {
	t.Helper()
	if shell == nil {
		shell = term.NewFake("user@mac default % ")
	}
	chat := &fakeSession{model: "m"}
	ws := newFakeWorkspace(chat)
	clip := &clipboard.Fake{}
	layout := filepath.Join(t.TempDir(), "layout.yaml")
	o := Options{
		Workspace:   ws,
		Bridge:      NewBridge(),
		Clipboard:   clip,
		LayoutPath:  layout,
		NewTerminal: func() term.Terminal { return shell },
	}
	for _, f := range opts {
		f(&o)
	}
	m := NewModel(o)
	next, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	return &mouseFixture{m: next.(Model), ws: ws, clip: clip, layout: layout, shell: shell}
}

func mouseEv(action tea.MouseAction, button tea.MouseButton, x, y int) tea.MouseMsg {
	return tea.MouseMsg{X: x, Y: y, Action: action, Button: button}
}

func (f *mouseFixture) send(msg tea.Msg) {
	next, _ := f.m.Update(msg)
	f.m = next.(Model)
}

func (f *mouseFixture) press(x, y int) {
	f.send(mouseEv(tea.MouseActionPress, tea.MouseButtonLeft, x, y))
}
func (f *mouseFixture) drag(x, y int) {
	f.send(mouseEv(tea.MouseActionMotion, tea.MouseButtonLeft, x, y))
}
func (f *mouseFixture) release(x, y int) {
	f.send(mouseEv(tea.MouseActionRelease, tea.MouseButtonLeft, x, y))
}

// rowOf returns the screen row (y) of the first chat content line containing text.
func (f *mouseFixture) rowOf(text string) int {
	for i, l := range f.m.contentPlain {
		if strings.Contains(l, text) {
			return 1 + i - f.m.vp.YOffset
		}
	}
	return -1
}

func TestClickSidebarRowSelectsSpace(t *testing.T) {
	f := newMouseFixture(t, nil)
	_ = f.ws.NewSpace("payments", "/work/pay")
	spaces := f.m.ws.Spaces()
	row := -1
	for i, r := range f.m.sidebarRows(f.m.sidebarCols()-4, false) {
		if r.kind == rowSpace && r.index == 1 {
			row = i
		}
	}
	if row < 0 {
		t.Fatalf("payments is not in the sidebar rows (spaces %v)", spaces)
	}
	f.press(3, row+1)
	if f.ws.active != "payments" {
		t.Fatalf("click on the space row should switch to it, active=%q", f.ws.active)
	}
	if !f.m.focusSidebar {
		t.Fatal("a click in the sidebar focuses it")
	}
}

func TestClickInInputFocusesInput(t *testing.T) {
	f := newMouseFixture(t, nil)
	f.send(tea.KeyMsg{Type: tea.KeyCtrlT}) // focus the terminal
	inputY := 1 + f.m.vp.Height + 1
	f.press(f.m.sidebarCols()+3, inputY)
	if f.m.focusTerm || f.m.focusSidebar {
		t.Fatal("a click in the chat input must focus the input")
	}
}

func TestDragSelectsChatTextAndCopies(t *testing.T) {
	f := newMouseFixture(t, nil)
	f.m.lines = []string{"alpha beta gamma"}
	f.m.refresh()
	y := f.rowOf("alpha beta gamma")
	if y < 0 {
		t.Fatal("line not visible")
	}
	x0 := f.m.sidebarCols()
	f.press(x0, y)
	f.drag(x0+4, y)
	f.release(x0+4, y)
	if len(f.clip.Texts) != 1 || f.clip.Texts[0] != "alpha" {
		t.Fatalf("copied %q, want [alpha]", f.clip.Texts)
	}
}

func TestDoubleClickSelectsWord(t *testing.T) {
	f := newMouseFixture(t, nil)
	f.m.lines = []string{"see foo.bar baz"}
	f.m.refresh()
	y := f.rowOf("see foo.bar baz")
	x := f.m.sidebarCols() + 6
	f.press(x, y)
	f.release(x, y)
	f.press(x, y)
	f.release(x, y)
	if len(f.clip.Texts) != 1 || f.clip.Texts[0] != "foo.bar" {
		t.Fatalf("double click copied %q", f.clip.Texts)
	}
}

func TestTripleClickSelectsLine(t *testing.T) {
	f := newMouseFixture(t, nil)
	f.m.lines = []string{"whole line here"}
	f.m.refresh()
	y := f.rowOf("whole line here")
	x := f.m.sidebarCols() + 2
	for i := 0; i < 3; i++ {
		f.press(x, y)
		f.release(x, y)
	}
	if len(f.clip.Texts) == 0 || f.clip.Texts[len(f.clip.Texts)-1] != "whole line here" {
		t.Fatalf("triple click copied %q", f.clip.Texts)
	}
}

func TestDividerDragResizesSidebarAndSavesLayout(t *testing.T) {
	f := newMouseFixture(t, nil)
	divider := f.m.sidebarCols() - 1
	f.press(divider, 5)
	f.drag(40, 5)
	f.release(40, 5)
	if f.m.sidebarCols() != 41 {
		t.Fatalf("sidebar columns = %d after drag to 40", f.m.sidebarCols())
	}
	saved, err := LoadLayout(f.layout)
	if err != nil || saved.SidebarWidth != 41 {
		t.Fatalf("layout not saved: %+v (%v)", saved, err)
	}
}

func TestDividerDragHasMinimumWidth(t *testing.T) {
	f := newMouseFixture(t, nil)
	divider := f.m.sidebarCols() - 1
	f.press(divider, 5)
	f.drag(2, 5)
	f.release(2, 5)
	if f.m.sidebarCols() != 20 {
		t.Fatalf("sidebar must not go under 20 columns, got %d", f.m.sidebarCols())
	}
}

func TestDividerDragKeepsChatAtLeastFortyColumns(t *testing.T) {
	f := newMouseFixture(t, nil)
	f.send(tea.KeyMsg{Type: tea.KeyCtrlT})
	f.m.focusTerm = false
	divider := f.m.sidebarCols() - 1
	f.press(divider, 5)
	f.drag(60, 5)
	f.release(60, 5)
	if chat := f.m.chatWidth(); chat < 40 {
		t.Fatalf("chat shrank to %d columns, minimum is 40", chat)
	}
}

func TestMouseTrackingAppGetsTheClick(t *testing.T) {
	shell := term.NewFake("vim")
	shell.TrackOn = true
	f := newMouseFixture(t, shell)
	f.send(tea.KeyMsg{Type: tea.KeyCtrlT})
	x := f.m.chatWidth() + f.m.sidebarCols() + 5
	f.press(x, 4)
	if len(shell.Mouse) != 1 || shell.Mouse[0].Kind != term.MouseDown {
		t.Fatalf("app should receive the press: %+v", shell.Mouse)
	}
	if shell.Mouse[0].X != 4 || shell.Mouse[0].Y != 3 {
		t.Fatalf("app cell = (%d,%d), want (4,3)", shell.Mouse[0].X, shell.Mouse[0].Y)
	}
	if f.m.sel.active {
		t.Fatal("no selection while the app tracks the mouse")
	}
}

func TestTerminalSelectionCopies(t *testing.T) {
	shell := term.NewFake("user@mac default % ls", "README.md")
	f := newMouseFixture(t, shell)
	f.send(tea.KeyMsg{Type: tea.KeyCtrlT})
	x := f.m.chatWidth() + f.m.sidebarCols() + 1
	f.press(x, 1)
	f.drag(x+3, 1)
	f.release(x+3, 1)
	if len(f.clip.Texts) != 1 || f.clip.Texts[0] != "user" {
		t.Fatalf("terminal copy = %q", f.clip.Texts)
	}
}

func TestPasteIntoInputCollapsesNewlines(t *testing.T) {
	f := newMouseFixture(t, nil)
	f.send(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("first\nsecond\r\nthird"), Paste: true})
	if got := f.m.input.Value(); got != "first second third" {
		t.Fatalf("input after paste = %q", got)
	}
}

func TestPasteIntoTerminalGoesToShell(t *testing.T) {
	shell := term.NewFake()
	f := newMouseFixture(t, shell)
	f.send(tea.KeyMsg{Type: tea.KeyCtrlT})
	f.send(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("two\nlines"), Paste: true})
	if len(shell.Pasted) != 1 || shell.Pasted[0] != "two\nlines" {
		t.Fatalf("shell pastes = %q", shell.Pasted)
	}
}

func TestCopyShowsCharacterCountBriefly(t *testing.T) {
	f := newMouseFixture(t, nil)
	f.m.lines = []string{"hello world"}
	f.m.refresh()
	y := f.rowOf("hello world")
	x := f.m.sidebarCols()
	f.press(x, y)
	f.drag(x+4, y)
	f.release(x+4, y)
	if !strings.Contains(f.m.View(), "copied 5 chars") {
		t.Fatalf("copy note missing:\n%s", f.m.View())
	}
	if !strings.Contains(f.m.View(), "copied 5 chars") {
		t.Fatal("note should still show right after the copy")
	}
}

func TestAnyKeyClearsSelection(t *testing.T) {
	f := newMouseFixture(t, nil)
	f.m.lines = []string{"abc"}
	f.m.refresh()
	y := f.rowOf("abc")
	f.press(f.m.sidebarCols(), y)
	f.drag(f.m.sidebarCols()+1, y)
	f.release(f.m.sidebarCols()+1, y)
	f.send(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("x")})
	if f.m.sel.active {
		t.Fatal("a key must clear the selection")
	}
}

func TestLayoutFileRestoresColumns(t *testing.T) {
	path := filepath.Join(t.TempDir(), "layout.yaml")
	if err := SaveLayout(path, Layout{SidebarWidth: 34, SidebarVisible: true}); err != nil {
		t.Fatal(err)
	}
	l, err := LoadLayout(path)
	if err != nil {
		t.Fatal(err)
	}
	f := newMouseFixture(t, nil, func(o *Options) { o.Layout = &l })
	if f.m.sidebarCols() != 34 {
		t.Fatalf("restored sidebar = %d, want 34", f.m.sidebarCols())
	}
}

func TestNoLayoutFileWrittenWithoutPath(t *testing.T) {
	f := newMouseFixture(t, nil, func(o *Options) { o.LayoutPath = "" })
	f.press(f.m.sidebarCols()-1, 5)
	f.drag(33, 5)
	f.release(33, 5)
	if _, err := os.Stat(f.layout); err == nil {
		t.Fatal("an empty LayoutPath must not write anything")
	}
}
