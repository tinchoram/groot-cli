package ui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

// pasteText delivers text the way the terminal does: a bracketed paste.
func pasteText(m Model, text string) Model {
	next, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(text), Paste: true})
	return next.(Model)
}

func TestShortPasteStaysInline(t *testing.T) {
	m, _ := newTestModel(t, &fakeSession{model: "m"})
	m = pasteText(m, "one line, short")
	if m.input.Value() != "one line, short" || len(m.pastes) != 0 {
		t.Fatalf("input = %q, pastes = %d", m.input.Value(), len(m.pastes))
	}
}

func TestLongPasteCollapsesToPlaceholder(t *testing.T) {
	m, _ := newTestModel(t, &fakeSession{model: "m"})
	m = pasteText(m, "a1\nb2\nc3\nd4")
	if m.input.Value() != "[Pasted text #1 +4 lines]" {
		t.Fatalf("input = %q", m.input.Value())
	}
	if !strings.Contains(stripANSI(m.View()), "[Pasted text #1 +4 lines]") {
		t.Fatalf("the placeholder must be on screen:\n%s", stripANSI(m.View()))
	}
	long := strings.Repeat("x", 201)
	m, _ = newTestModel(t, &fakeSession{model: "m"})
	m = pasteText(m, long)
	if m.input.Value() != "[Pasted text #1 +1 lines]" {
		t.Fatalf("a paste over 200 characters collapses too: %q", m.input.Value())
	}
}

func TestSendExpandsThePastedText(t *testing.T) {
	chat := &fakeSession{model: "m", reply: "ok"}
	m, _ := newTestModel(t, chat)
	m = pasteText(m, "one\ntwo\nthree\nfour")
	m = typeText(m, " please")
	m, cmd := press(m, tea.KeyEnter)
	next, _ := m.Update(findMsg[answerMsg](t, cmd))
	m = next.(Model)

	want := "one\ntwo\nthree\nfour please"
	if len(chat.sends) != 1 || chat.sends[0] != want {
		t.Fatalf("the model must get the full text: sends=%q", chat.sends)
	}
	out := stripANSI(m.transcriptText())
	if !strings.Contains(out, "[Pasted text #1 +4 lines]") || !strings.Contains(out, "one") {
		t.Fatalf("the echo shows the placeholder and the first line:\n%s", out)
	}
	if strings.Contains(out, "three") {
		t.Fatalf("the echo must not show the whole paste:\n%s", out)
	}
}

func TestSeveralPastesGetNumberedPlaceholders(t *testing.T) {
	chat := &fakeSession{model: "m", reply: "ok"}
	m, _ := newTestModel(t, chat)
	m = pasteText(m, "a\nb\nc\nd")
	m = typeText(m, " and ")
	m = pasteText(m, "e\nf\ng\nh")
	if m.input.Value() != "[Pasted text #1 +4 lines] and [Pasted text #2 +4 lines]" {
		t.Fatalf("input = %q", m.input.Value())
	}
	m, cmd := press(m, tea.KeyEnter)
	next, _ := m.Update(findMsg[answerMsg](t, cmd))
	_ = next
	if len(chat.sends) != 1 || chat.sends[0] != "a\nb\nc\nd and e\nf\ng\nh" {
		t.Fatalf("sends = %q", chat.sends)
	}
}

func TestBackspaceRemovesTheWholePaste(t *testing.T) {
	m, _ := newTestModel(t, &fakeSession{model: "m"})
	m = typeText(m, "see ")
	m = pasteText(m, "a\nb\nc\nd")
	m = typeText(m, " end")
	// Move the cursor right after the placeholder, then backspace.
	for range len(" end") {
		m, _ = press(m, tea.KeyLeft)
	}
	m, _ = press(m, tea.KeyBackspace)
	if m.input.Value() != "see  end" || len(m.pastes) != 0 {
		t.Fatalf("input = %q, pastes = %d", m.input.Value(), len(m.pastes))
	}
}

func TestPasteEchoReachesBugContextAsPlaceholder(t *testing.T) {
	m, _ := newTestModel(t, &fakeSession{model: "m", reply: "ok"})
	m = pasteText(m, "stack\ntrace\nline\nmore")
	m, _ = press(m, tea.KeyEnter)
	ctx := strings.Join(m.recentContext(), "\n")
	if !strings.Contains(ctx, "[Pasted text #1 +4 lines]") || strings.Contains(ctx, "trace") {
		t.Fatalf("bug context must hold the placeholder, not the paste: %q", ctx)
	}
}
