package ui

import (
	"errors"
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
)

func TestAskSendsWithoutMovingTheScreen(t *testing.T) {
	m, ws, chat := newMentionModel(t)
	before := m.key
	m = sendLine(t, m, "@explore/search-specialist find the retry loop")

	if ws.ConversationKey() != before || m.key != before {
		t.Fatalf("the screen moved to %q, want it to stay on %q", m.key, before)
	}
	if len(ws.asked) != 1 || ws.asked[0] != "default/devjunior|explore/search-specialist|find the retry loop" {
		t.Fatalf("asked = %q", ws.asked)
	}
	if len(chat.sends) != 0 {
		t.Fatalf("the question must not be sent to the screen's agent: %v", chat.sends)
	}
}

func TestAskIgnoresTheBusyScreen(t *testing.T) {
	m, ws, _ := newMentionModel(t)
	m.busy = true
	m = sendLine(t, m, "@reviewer look at the diff")

	out := stripANSI(m.transcriptText())
	if strings.Contains(out, "agent is working; wait or switch") {
		t.Fatalf("an ask must not wait for the screen's turn:\n%s", out)
	}
	if len(ws.asked) != 1 || ws.asked[0] != "default/devjunior|default/reviewer|look at the diff" {
		t.Fatalf("asked = %q", ws.asked)
	}
}

func TestAskToBusyAgentSaysItWaits(t *testing.T) {
	m, ws, _ := newMentionModel(t)
	m.runs["default/reviewer"] = &run{busy: true}
	m = sendLine(t, m, "@reviewer look at the diff")

	if !strings.Contains(stripANSI(m.transcriptText()), "default/reviewer is working; the ask waits") {
		t.Fatalf("the waiting ask must say so:\n%s", stripANSI(m.transcriptText()))
	}
	if len(ws.asked) != 1 {
		t.Fatalf("the ask is still sent: %q", ws.asked)
	}
}

func TestBareAddressSwitchesAndAsksNothing(t *testing.T) {
	m, ws, _ := newMentionModel(t)
	m = sendLine(t, m, "@explore/search-specialist")

	if m.key != "explore/search-specialist" {
		t.Fatalf("the screen = %q, want the bare address to switch", m.key)
	}
	if len(ws.asked) != 0 {
		t.Fatalf("a bare address must not ask: %q", ws.asked)
	}
}

func TestAskToUnknownAgentKeepsTheNote(t *testing.T) {
	m, ws, _ := newMentionModel(t)
	m = sendLine(t, m, "@nobody do it")

	if !strings.Contains(stripANSI(m.transcriptText()), "no agent @nobody; agents: ") {
		t.Fatalf("the unknown-agent note is missing:\n%s", stripANSI(m.transcriptText()))
	}
	if len(ws.asked) != 0 {
		t.Fatalf("nothing may be asked: %q", ws.asked)
	}
}

func TestAskFailureIsShownOnTheScreen(t *testing.T) {
	m, ws, _ := newMentionModel(t)
	ws.askErr = errors.New("unknown recipient")
	m = sendLine(t, m, "@reviewer hi")

	if !strings.Contains(stripANSI(m.transcriptText()), "error: unknown recipient") {
		t.Fatalf("the failure must be shown:\n%s", stripANSI(m.transcriptText()))
	}
}

func TestAskAnswerShowsABlockOnTheOriginScreen(t *testing.T) {
	m, _, _ := newMentionModel(t)
	m = update(t, m, askMsg{origin: "default/devjunior", key: "default/reviewer", text: "the diff is fine"})

	out := stripANSI(m.transcriptText())
	if !strings.Contains(out, "default/reviewer:") || !strings.Contains(out, "the diff is fine") {
		t.Fatalf("the answer must be a block on the origin screen:\n%s", out)
	}
	if m.key != "default/devjunior" {
		t.Fatalf("the screen moved to %q", m.key)
	}
}

func TestAskAnswerOffTheOriginScreenIsOneLine(t *testing.T) {
	m, _, _ := newMentionModel(t)
	m = update(t, m, askMsg{origin: "explore/search-specialist", key: "default/reviewer", text: "first line\nsecond line"})

	out := stripANSI(m.transcriptText())
	if !strings.Contains(out, "default/reviewer: first line") {
		t.Fatalf("one line with the first line of the answer is expected:\n%s", out)
	}
	if strings.Contains(out, "second line") {
		t.Fatalf("only the first line is shown:\n%s", out)
	}
	if m.key != "default/devjunior" {
		t.Fatalf("the screen moved to %q", m.key)
	}
}

func TestAskNoticeIsCutToTheScreenWidth(t *testing.T) {
	m, _, _ := newMentionModel(t)
	long := strings.Repeat("word ", 60)
	m = update(t, m, askMsg{origin: "explore/search-specialist", key: "default/reviewer", text: long})

	for _, line := range strings.Split(stripANSI(m.transcriptText()), "\n") {
		if strings.Contains(line, "default/reviewer:") {
			if w := lipgloss.Width(line); w > m.contentWidth() {
				t.Fatalf("the notice is %d cells, the screen has %d", w, m.contentWidth())
			}
			if !strings.Contains(line, "…") {
				t.Fatalf("a cut notice ends with an ellipsis: %q", line)
			}
			return
		}
	}
	t.Fatalf("no notice line:\n%s", stripANSI(m.transcriptText()))
}

func TestAskAnswerIsNotRepeatedByTheTurnsEnd(t *testing.T) {
	m, _, _ := newMentionModel(t)
	m = update(t, m, askMsg{origin: "explore/search-specialist", key: "default/reviewer", text: "done"})
	m = update(t, m, answerMsg{key: "default/reviewer", text: "done"})

	if n := strings.Count(stripANSI(m.transcriptText()), "default/reviewer: done"); n != 1 {
		t.Fatalf("the answer must be announced once, got %d:\n%s", n, stripANSI(m.transcriptText()))
	}
}

func TestAnswerOfAskedAgentOffScreenIsOneLine(t *testing.T) {
	m, _, _ := newMentionModel(t)
	m.asked["default/reviewer"] = true
	m = update(t, m, answerMsg{key: "default/reviewer", text: "all good\nmore detail"})

	out := stripANSI(m.transcriptText())
	if !strings.Contains(out, "default/reviewer: all good") || strings.Contains(out, "more detail") {
		t.Fatalf("an asked agent's answer off the screen is one line:\n%s", out)
	}
	if strings.Contains(out, "\nall good\n") {
		t.Fatalf("the full answer must not appear on the screen:\n%s", out)
	}
}

func TestMentionedOperatorOffScreenMarksAndNotesOnce(t *testing.T) {
	m, _, _ := newMentionModel(t)
	m = update(t, m, answerMsg{key: "default/reviewer", text: "@Tinchoram please approve"})

	if !m.needsYou["default/reviewer"] {
		t.Fatal("the @ marker is unchanged")
	}
	if !strings.Contains(stripANSI(m.transcriptText()), "default/reviewer: @Tinchoram please approve") {
		t.Fatalf("a mention off the screen is one line:\n%s", stripANSI(m.transcriptText()))
	}
}

func TestAnswerOnTheScreenAddsNoLine(t *testing.T) {
	m, _, _ := newMentionModel(t)
	m.asked["default/devjunior"] = true
	m = update(t, m, answerMsg{key: "default/devjunior", text: "here"})

	if strings.Contains(stripANSI(m.transcriptText()), "default/devjunior: here") {
		t.Fatalf("the answer of the screen is not announced as a line:\n%s", stripANSI(m.transcriptText()))
	}
}
