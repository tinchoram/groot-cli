package ui

import "testing"

func sel(ar, ac, cr, cc int) selection {
	return selection{anchor: point{ar, ac}, cursor: point{cr, cc}, active: true}
}

func TestExtractTextSingleRow(t *testing.T) {
	rows := []string{"hello world"}
	if got := extractText(rows, sel(0, 0, 0, 4)); got != "hello" {
		t.Fatalf("got %q", got)
	}
}

func TestExtractTextAcrossRowsJoinsAndTrims(t *testing.T) {
	rows := []string{"first line   ", "second", "third row"}
	got := extractText(rows, sel(0, 6, 2, 4))
	if got != "line\nsecond\nthird" {
		t.Fatalf("got %q", got)
	}
}

func TestExtractTextReversedDragMatchesForward(t *testing.T) {
	rows := []string{"first line   ", "second", "third row"}
	forward := extractText(rows, sel(0, 6, 2, 4))
	backward := extractText(rows, sel(2, 4, 0, 6))
	if forward != backward {
		t.Fatalf("reversed %q != forward %q", backward, forward)
	}
}

func TestExtractTextInactiveOrOutOfRangeIsEmpty(t *testing.T) {
	rows := []string{"abc"}
	if got := extractText(rows, selection{}); got != "" {
		t.Fatalf("inactive selection gave %q", got)
	}
	if got := extractText(rows, sel(0, 0, 5, 5)); got != "abc" {
		t.Fatalf("row past the end is clipped to the text: %q", got)
	}
}

func TestWordAtFindsWordBounds(t *testing.T) {
	row := "see foo.bar baz"
	from, to := wordAt(row, 6)
	if row[from:to] != "foo.bar" {
		t.Fatalf("word = %q", row[from:to])
	}
	from, to = wordAt("a  b", 1)
	if from != 1 || to != 2 {
		t.Fatalf("a space selects only its own cell: %d-%d", from, to)
	}
}

func TestLineSpanCoversWholeRow(t *testing.T) {
	from, to := lineSpan("héllo")
	if from != 0 || to != 5 {
		t.Fatalf("line span = %d-%d (runes, not bytes)", from, to)
	}
}

func TestHighlightKeepsVisibleText(t *testing.T) {
	plain := "hello world"
	got := stripCells(highlightCells(plain, 6, 11))
	if got != plain {
		t.Fatalf("highlight changed the text: %q", got)
	}
}
