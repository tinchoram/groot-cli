package term

import (
	"testing"

	"github.com/charmbracelet/x/ansi"
)

func TestMouseTrackingFollowsAppModes(t *testing.T) {
	p := &ptyTerminal{}
	if p.Tracking() {
		t.Fatal("no tracking before the app asks for it")
	}
	p.setMode(ansi.ModeMouseNormal, true)
	if !p.Tracking() {
		t.Fatal("normal mouse tracking should count as tracking")
	}
	p.setMode(ansi.ModeMouseNormal, false)
	if p.Tracking() {
		t.Fatal("tracking must end when the app turns it off")
	}
}

func TestBracketedPasteFollowsAppMode(t *testing.T) {
	p := &ptyTerminal{}
	p.setMode(ansi.ModeBracketedPaste, true)
	if !p.bracketed.Load() {
		t.Fatal("bracketed paste should be on")
	}
	p.setMode(ansi.ModeBracketedPaste, false)
	if p.bracketed.Load() {
		t.Fatal("bracketed paste should be off")
	}
}

func TestFakeTerminalRecordsPasteAndMouse(t *testing.T) {
	f := NewFake()
	_ = f.Paste("two\nlines")
	_ = f.SendMouse(MouseEvent{Kind: MouseDown, Button: 0, X: 3, Y: 1})
	if len(f.Pasted) != 1 || f.Pasted[0] != "two\nlines" || len(f.Mouse) != 1 {
		t.Fatalf("fake recorded paste=%v mouse=%v", f.Pasted, f.Mouse)
	}
}
