package term

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func TestKeyBytes(t *testing.T) {
	cases := []struct {
		name string
		key  tea.KeyMsg
		want string
	}{
		{"rune", tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("ls")}, "ls"},
		{"unicode rune", tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("é")}, "é"},
		{"space", tea.KeyMsg{Type: tea.KeySpace}, " "},
		{"enter", tea.KeyMsg{Type: tea.KeyEnter}, "\r"},
		{"backspace", tea.KeyMsg{Type: tea.KeyBackspace}, "\x7f"},
		{"tab", tea.KeyMsg{Type: tea.KeyTab}, "\t"},
		{"shift-tab", tea.KeyMsg{Type: tea.KeyShiftTab}, "\x1b[Z"},
		{"esc", tea.KeyMsg{Type: tea.KeyEsc}, "\x1b"},
		{"up", tea.KeyMsg{Type: tea.KeyUp}, "\x1b[A"},
		{"down", tea.KeyMsg{Type: tea.KeyDown}, "\x1b[B"},
		{"right", tea.KeyMsg{Type: tea.KeyRight}, "\x1b[C"},
		{"left", tea.KeyMsg{Type: tea.KeyLeft}, "\x1b[D"},
		{"home", tea.KeyMsg{Type: tea.KeyHome}, "\x1b[H"},
		{"end", tea.KeyMsg{Type: tea.KeyEnd}, "\x1b[F"},
		{"delete", tea.KeyMsg{Type: tea.KeyDelete}, "\x1b[3~"},
		{"page up", tea.KeyMsg{Type: tea.KeyPgUp}, "\x1b[5~"},
		{"page down", tea.KeyMsg{Type: tea.KeyPgDown}, "\x1b[6~"},
		{"ctrl-a", tea.KeyMsg{Type: tea.KeyCtrlA}, "\x01"},
		{"ctrl-c", tea.KeyMsg{Type: tea.KeyCtrlC}, "\x03"},
		{"ctrl-d", tea.KeyMsg{Type: tea.KeyCtrlD}, "\x04"},
		{"ctrl-l", tea.KeyMsg{Type: tea.KeyCtrlL}, "\x0c"},
		{"alt-x", tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("x"), Alt: true}, "\x1bx"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := string(KeyBytes(c.key)); got != c.want {
				t.Fatalf("KeyBytes = %q want %q", got, c.want)
			}
		})
	}
}

func TestKeyBytesUnknownKeyIsEmpty(t *testing.T) {
	if got := KeyBytes(tea.KeyMsg{Type: tea.KeyF20}); len(got) != 0 {
		t.Fatalf("unmapped key should send nothing, got %q", got)
	}
}
