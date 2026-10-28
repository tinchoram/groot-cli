package ui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// completionInserts returns the text each candidate completes to.
func completionInserts(m Model) []string {
	var out []string
	for _, c := range m.completionCandidates() {
		out = append(out, c.insert)
	}
	return out
}

func TestCompletionFiltersCommandsByPrefix(t *testing.T) {
	m, _ := newTestModel(t, &fakeSession{model: "m"})
	m = typeText(m, "/SP")
	got := completionInserts(m)
	if len(got) != 1 || got[0] != "/space" {
		t.Fatalf("candidates for /SP = %v, want [/space]", got)
	}
}

func TestCompletionShowsAtMostSixCommands(t *testing.T) {
	m, _ := newTestModel(t, &fakeSession{model: "m"})
	m = typeText(m, "/")
	got := completionInserts(m)
	if len(got) == 0 || len(got) > 6 {
		t.Fatalf("candidates for / = %d, want 1 to 6: %v", len(got), got)
	}
}

func TestCompletionSubCommands(t *testing.T) {
	m, _ := newTestModel(t, &fakeSession{model: "m"})
	if got := completionInserts(typeText(m, "/space n")); len(got) != 2 || !contains(got, "/space new") || !contains(got, "/space network") {
		t.Fatalf("candidates for /space n = %v, want [/space new /space network]", got)
	}
	m, _ = newTestModel(t, &fakeSession{model: "m"})
	got := completionInserts(typeText(m, "/space "))
	for _, want := range []string{"/space list", "/space routes"} {
		if !contains(got, want) {
			t.Fatalf("candidates for \"/space \" = %v, missing %s", got, want)
		}
	}
	m, _ = newTestModel(t, &fakeSession{model: "m"})
	if got := completionInserts(typeText(m, "/allow ")); len(got) != 1 || got[0] != "/allow clear" {
		t.Fatalf("candidates for \"/allow \" = %v, want [/allow clear]", got)
	}
	m, _ = newTestModel(t, &fakeSession{model: "m"})
	if got := completionInserts(typeText(m, "/space new foo")); len(got) != 0 {
		t.Fatalf("a typed argument must close the list, got %v", got)
	}
	m, _ = newTestModel(t, &fakeSession{model: "m"})
	if got := completionInserts(typeText(m, "/model ")); len(got) != 0 {
		t.Fatalf("/model takes a name, not sub-commands, got %v", got)
	}
}

func TestTabCompletesHighlightedWithTrailingSpace(t *testing.T) {
	m, _ := newTestModel(t, &fakeSession{model: "m"})
	m = typeText(m, "/sp")
	m, _ = press(m, tea.KeyTab)
	if got := m.input.Value(); got != "/space " {
		t.Fatalf("tab completed to %q, want %q", got, "/space ")
	}
	if !m.completionOpen() {
		t.Fatal("after completing /space the sub-command list should open")
	}
}

func TestDownMovesHighlightAndKeepsHistory(t *testing.T) {
	m, _ := newTestModel(t, &fakeSession{model: "m"})
	m.hist = NewHistory([]string{"an earlier prompt"}, 100)
	m = typeText(m, "/")
	before := completionInserts(m)
	m, _ = press(m, tea.KeyDown)
	if m.compSel != 1 {
		t.Fatalf("down should move the highlight to 1, got %d", m.compSel)
	}
	if m.input.Value() != "/" || m.hist.Browsing() {
		t.Fatalf("down with the list open must not recall history: input %q browsing %v", m.input.Value(), m.hist.Browsing())
	}
	m, _ = press(m, tea.KeyTab)
	if got := m.input.Value(); got != before[1]+" " {
		t.Fatalf("tab after down completed to %q, want %q", got, before[1]+" ")
	}
}

func TestDownRecallsHistoryWhenListIsClosed(t *testing.T) {
	m, _ := newTestModel(t, &fakeSession{model: "m"})
	m.hist = NewHistory([]string{"an earlier prompt"}, 100)
	m, _ = press(m, tea.KeyUp)
	if m.input.Value() != "an earlier prompt" {
		t.Fatalf("up with no list should recall history, got %q", m.input.Value())
	}
}

func TestEscClosesListAndKeepsInput(t *testing.T) {
	m, _ := newTestModel(t, &fakeSession{model: "m"})
	m = typeText(m, "/sp")
	m, _ = press(m, tea.KeyEsc)
	if m.completionOpen() {
		t.Fatal("esc should close the list")
	}
	if m.input.Value() != "/sp" {
		t.Fatalf("esc cleared the input: %q", m.input.Value())
	}
	m = typeText(m, "a")
	if !m.completionOpen() {
		t.Fatal("typing after esc should filter and reopen the list")
	}
}

func TestNoListWithoutSlashOnFirstLine(t *testing.T) {
	m, _ := newTestModel(t, &fakeSession{model: "m"})
	if m = typeText(m, "help"); m.completionOpen() {
		t.Fatal("no list for text that does not start with /")
	}
	m, _ = newTestModel(t, &fakeSession{model: "m"})
	m = typeText(m, "/sp")
	m, _ = press(m, tea.KeyCtrlJ)
	m = typeText(m, "x")
	if m.completionOpen() {
		t.Fatal("no list when the cursor is not on the first line")
	}
}

func TestNoListWhenSidebarOrTerminalHasFocus(t *testing.T) {
	m, _ := newTestModel(t, &fakeSession{model: "m"})
	m = typeText(m, "/sp")
	m.focusSidebar = true
	if m.completionOpen() {
		t.Fatal("no list while the sidebar has focus")
	}
	m.focusSidebar, m.focusTerm = false, true
	if m.completionOpen() {
		t.Fatal("no list while the terminal has focus")
	}
}

func TestNoListDuringApprovalOrPasteBox(t *testing.T) {
	m, _ := newTestModel(t, &fakeSession{model: "m"})
	m = typeText(m, "/sp")
	m.pending = &confirmState{prompt: "write"}
	if m.completionOpen() {
		t.Fatal("no list during an approval")
	}
	m.pending = nil
	m, _ = newTestModel(t, &fakeSession{model: "m"})
	m = typeText(m, "/")
	m = m.insertPaste("one\ntwo\nthree\nfour")
	if m.completionOpen() {
		t.Fatal("no list while a paste box is in the input")
	}
}

func TestEnterCompletesWhenOneCandidateRemains(t *testing.T) {
	m, _ := newTestModel(t, &fakeSession{model: "m"})
	m = typeText(m, "/hel")
	m, _ = press(m, tea.KeyEnter)
	if got := m.input.Value(); got != "/help " {
		t.Fatalf("enter completed to %q, want %q", got, "/help ")
	}
	if strings.Contains(stripANSI(m.transcriptText()), "available commands") {
		t.Fatal("enter that completes must not send the command")
	}
}

func TestEnterSendsExactCommand(t *testing.T) {
	m, _ := newTestModel(t, &fakeSession{model: "m"})
	m = sendLine(t, m, "/help")
	if !strings.Contains(stripANSI(m.transcriptText()), "available commands") {
		t.Fatalf("exact /help should be sent:\n%s", stripANSI(m.transcriptText()))
	}
}

func TestEnterSendsWhenSeveralCandidatesRemain(t *testing.T) {
	m, _ := newTestModel(t, &fakeSession{model: "m"})
	m = sendLine(t, m, "/mod")
	if !strings.Contains(stripANSI(m.transcriptText()), "unknown command /mod") {
		t.Fatalf("ambiguous /mod should be sent as typed:\n%s", stripANSI(m.transcriptText()))
	}
}

func TestCompletionTruncatesAtNarrowWidth(t *testing.T) {
	m, _ := newTestModel(t, &fakeSession{model: "m"})
	m.sidebarOpen = false
	m.width = 40
	m.resize()
	m = typeText(m, "/space ")
	lines := m.completionLines()
	if len(lines) == 0 {
		t.Fatal("expected completion rows")
	}
	sawEllipsis := false
	for _, l := range lines {
		if w := lipgloss.Width(l); w > m.chatWidth() {
			t.Fatalf("row is %d cells, chat is %d: %q", w, m.chatWidth(), stripANSI(l))
		}
		if strings.Contains(stripANSI(l), "…") {
			sawEllipsis = true
		}
	}
	if !sawEllipsis {
		t.Fatalf("long help should be cut with …:\n%s", stripANSI(strings.Join(lines, "\n")))
	}
}

func TestCompletionRowsOnlyWhileOpen(t *testing.T) {
	m, _ := newTestModel(t, &fakeSession{model: "m"})
	closed := m.vp.Height
	m = typeText(m, "/")
	if m.vp.Height >= closed {
		t.Fatalf("open list should take transcript rows: closed %d, open %d", closed, m.vp.Height)
	}
	m, _ = press(m, tea.KeyEsc)
	if m.vp.Height != closed {
		t.Fatalf("closed list must give the rows back: want %d, got %d", closed, m.vp.Height)
	}
}

func TestCompletionListRendersAboveTheInput(t *testing.T) {
	m, _ := newTestModel(t, &fakeSession{model: "m"})
	m = typeText(m, "/sp")
	view := stripANSI(m.View())
	list := strings.Index(view, "spaces and the agents assigned to them")
	box := strings.Index(view, "❯")
	if list < 0 || box < 0 || list > box {
		t.Fatalf("the list must render above the input:\n%s", view)
	}
}

func TestEveryKnownCommandHasHelp(t *testing.T) {
	for name := range knownCommands {
		found := false
		for _, e := range helpEntries {
			if firstToken(e.usage) == "/"+name {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("/%s is known to the UI but has no /help entry", name)
		}
	}
}
