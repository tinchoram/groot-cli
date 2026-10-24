package ui

import (
	"strings"
	"testing"
)

func TestBoldRendersWithoutAsterisks(t *testing.T) {
	out := renderMarkdown("**groot** is here", 60)
	plain := stripCells(out)
	if strings.Contains(plain, "**") {
		t.Fatalf("literal asterisks left in %q", plain)
	}
	if !strings.Contains(plain, "groot is here") {
		t.Fatalf("text lost: %q", plain)
	}
}

func TestCodeFenceRenders(t *testing.T) {
	out := renderMarkdown("Run:\n\n```go\nfmt.Println(1)\n```\n", 60)
	plain := stripCells(out)
	if strings.Contains(plain, "```") {
		t.Fatalf("fence markers left in %q", plain)
	}
	if !strings.Contains(plain, "fmt.Println(1)") {
		t.Fatalf("code missing: %q", plain)
	}
}

func TestMarkdownWrapsToWidth(t *testing.T) {
	long := strings.Repeat("alpha beta gamma delta ", 8)
	for _, line := range strings.Split(stripCells(renderMarkdown(long, 24)), "\n") {
		if n := len([]rune(line)); n > 24 {
			t.Fatalf("line of %d cells exceeds width 24: %q", n, line)
		}
	}
}

func TestMarkdownOutputIsCached(t *testing.T) {
	a := renderMarkdown("**cached**", 40)
	b := renderMarkdown("**cached**", 40)
	if a != b {
		t.Fatal("same input and width must give the same output")
	}
	if _, ok := renderedBlocks.get("**cached**", 40); !ok {
		t.Fatal("the rendered block should be cached")
	}
}

func TestCompletedTurnReplacesRawMarkdown(t *testing.T) {
	m, _ := newTestModel(t, &fakeSession{model: "m"})
	m.busy = true
	next, _ := m.Update(streamMsg{delta: "**groot**"})
	m = next.(Model)
	if !strings.Contains(m.transcriptText(), "**groot**") {
		t.Fatal("while streaming the raw text is shown")
	}
	next, _ = m.Update(answerMsg{text: "**groot** is ready"})
	m = next.(Model)
	out := stripCells(m.transcriptText())
	if strings.Contains(out, "**") {
		t.Fatalf("finished turn still shows raw markdown:\n%s", out)
	}
	if !strings.Contains(out, "groot is ready") {
		t.Fatalf("finished turn lost its text:\n%s", out)
	}
}

func TestResizeRewrapsMarkdown(t *testing.T) {
	m, _ := newTestModel(t, &fakeSession{model: "m"})
	next, _ := m.Update(answerMsg{text: strings.Repeat("word ", 30)})
	m = next.(Model)
	m.width = 50
	m.resize()
	for _, line := range strings.Split(stripCells(m.transcriptText()), "\n") {
		if n := len([]rune(line)); n > 60 {
			t.Fatalf("after resize a line is %d cells: %q", n, line)
		}
	}
}
