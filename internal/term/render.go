package term

import (
	"fmt"
	"image/color"
	"strings"

	"github.com/charmbracelet/lipgloss"
	uv "github.com/charmbracelet/ultraviolet"
)

// screen is the part of the emulator that rendering needs.
type screen interface {
	CellAt(x, y int) *uv.Cell
	Width() int
	Height() int
	CursorPosition() uv.Position
	CursorHidden() bool
}

// renderScreen turns every row of the screen into one styled string. The
// cursor cell is drawn reversed so the user can see where input goes.
func renderScreen(s screen) []string {
	w, h := s.Width(), s.Height()
	cursorX, cursorY := -1, -1
	if !s.CursorHidden() {
		pos := s.CursorPosition()
		cursorX, cursorY = pos.X, pos.Y
	}
	rows := make([]string, h)
	for y := 0; y < h; y++ {
		var b strings.Builder
		for x := 0; x < w; x++ {
			cell := s.CellAt(x, y)
			if cell == nil {
				b.WriteByte(' ')
				continue
			}
			if cell.Width == 0 && cell.Content == "" {
				continue // right half of a wide character
			}
			content := cell.Content
			if content == "" {
				content = " "
			}
			st := styleOf(cell.Style)
			if x == cursorX && y == cursorY {
				st = st.Reverse(true)
			}
			b.WriteString(st.Render(content))
		}
		rows[y] = b.String()
	}
	return rows
}

// styleOf maps an emulator style to a lipgloss style.
func styleOf(s uv.Style) lipgloss.Style {
	st := lipgloss.NewStyle()
	if s.Fg != nil {
		st = st.Foreground(lipgloss.Color(hexOf(s.Fg)))
	}
	if s.Bg != nil {
		st = st.Background(lipgloss.Color(hexOf(s.Bg)))
	}
	st = st.Bold(s.Attrs&uv.AttrBold != 0).
		Faint(s.Attrs&uv.AttrFaint != 0).
		Italic(s.Attrs&uv.AttrItalic != 0).
		Reverse(s.Attrs&uv.AttrReverse != 0).
		Strikethrough(s.Attrs&uv.AttrStrikethrough != 0)
	if s.Underline != uv.UnderlineNone {
		st = st.Underline(true)
	}
	return st
}

// hexOf converts a colour to #rrggbb.
func hexOf(c color.Color) string {
	r, g, b, _ := c.RGBA()
	return fmt.Sprintf("#%02x%02x%02x", r>>8, g>>8, b>>8)
}
