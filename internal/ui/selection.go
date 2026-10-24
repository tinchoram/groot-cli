package ui

import (
	"strings"
	"unicode"

	"github.com/charmbracelet/lipgloss"
)

// point is a cell in the text being selected: a row and a column, both
// zero-based and relative to the selectable area.
type point struct{ row, col int }

// selection is a drag from anchor to cursor. Both cells are included.
type selection struct {
	anchor, cursor point
	active         bool
}

func (s selection) ordered() (start, end point) {
	a, c := s.anchor, s.cursor
	if c.row < a.row || (c.row == a.row && c.col < a.col) {
		a, c = c, a
	}
	return a, c
}

// extractText returns the selected text. Rows are joined with "\n", and
// trailing spaces on each row are trimmed.
func extractText(rows []string, s selection) string {
	if !s.active {
		return ""
	}
	start, end := s.ordered()
	var out []string
	for r := start.row; r <= end.row && r < len(rows); r++ {
		runes := []rune(rows[r])
		from, to := 0, len(runes)
		if r == start.row {
			from = start.col
		}
		if r == end.row {
			to = end.col + 1
		}
		from = min(max(from, 0), len(runes))
		to = min(max(to, from), len(runes))
		out = append(out, strings.TrimRight(string(runes[from:to]), " "))
	}
	return strings.Join(out, "\n")
}

// isWordRune is what a double-click treats as part of a word.
func isWordRune(r rune) bool {
	return unicode.IsLetter(r) || unicode.IsDigit(r) || strings.ContainsRune("_-./:@~", r)
}

// wordAt returns the rune span of the word under col. On a space it selects
// that single cell.
func wordAt(row string, col int) (from, to int) {
	runes := []rune(row)
	if col < 0 || col >= len(runes) {
		return col, col + 1
	}
	if !isWordRune(runes[col]) {
		return col, col + 1
	}
	from, to = col, col+1
	for from > 0 && isWordRune(runes[from-1]) {
		from--
	}
	for to < len(runes) && isWordRune(runes[to]) {
		to++
	}
	return from, to
}

// lineSpan is the whole row, in runes.
func lineSpan(row string) (from, to int) {
	return 0, len([]rune(row))
}

// highlightCells renders plain with runes [from, to) in the selection colour.
// The visible text is unchanged; colours of the row are not kept.
func highlightCells(plain string, from, to int) string {
	runes := []rune(plain)
	from = min(max(from, 0), len(runes))
	to = min(max(to, from), len(runes))
	return string(runes[:from]) +
		styleSelection.Render(string(runes[from:to])) +
		string(runes[to:])
}

// highlightRows returns styled with the rows covered by s replaced by their
// plain text highlighted over the selected columns. Rows outside the
// selection keep their colours; selected rows show only the highlight.
func highlightRows(styled, plain []string, s selection) []string {
	if !s.active {
		return styled
	}
	out := make([]string, len(styled))
	copy(out, styled)
	start, end := s.ordered()
	for r := start.row; r <= end.row && r < len(out) && r < len(plain); r++ {
		from, to := 0, len([]rune(plain[r]))
		if r == start.row {
			from = start.col
		}
		if r == end.row {
			to = end.col + 1
		}
		out[r] = highlightCells(plain[r], from, to)
	}
	return out
}

// styleSelection is the green background of selected text.
var styleSelection = lipgloss.NewStyle().
	Background(lipgloss.Color(colorGreen)).
	Foreground(lipgloss.Color("#0B1F12"))
