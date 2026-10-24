package term

import (
	tea "github.com/charmbracelet/bubbletea"
)

// specialKeys are the byte sequences of keys that are not plain characters.
var specialKeys = map[tea.KeyType]string{
	tea.KeyEnter:     "\r",
	tea.KeyBackspace: "\x7f",
	tea.KeyTab:       "\t",
	tea.KeyShiftTab:  "\x1b[Z",
	tea.KeyEsc:       "\x1b",
	tea.KeySpace:     " ",
	tea.KeyUp:        "\x1b[A",
	tea.KeyDown:      "\x1b[B",
	tea.KeyRight:     "\x1b[C",
	tea.KeyLeft:      "\x1b[D",
	tea.KeyHome:      "\x1b[H",
	tea.KeyEnd:       "\x1b[F",
	tea.KeyDelete:    "\x1b[3~",
	tea.KeyInsert:    "\x1b[2~",
	tea.KeyPgUp:      "\x1b[5~",
	tea.KeyPgDown:    "\x1b[6~",
}

// KeyBytes translates a Bubble Tea key into the bytes a terminal expects.
// Keys with no mapping send nothing.
func KeyBytes(k tea.KeyMsg) []byte {
	var out []byte
	if k.Alt {
		out = append(out, 0x1b)
	}
	if k.Type == tea.KeyRunes {
		return append(out, string(k.Runes)...)
	}
	if s, ok := specialKeys[k.Type]; ok {
		return append(out, s...)
	}
	// Control keys: ctrl+a is 0x01 and so on up to 0x1f.
	if k.Type >= 0 && k.Type < 32 {
		return append(out, byte(k.Type))
	}
	if len(out) > 0 {
		return out // alt on an unmapped key: the escape alone
	}
	return nil
}
