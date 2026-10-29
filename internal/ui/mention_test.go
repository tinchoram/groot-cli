package ui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"

	"github.com/tinchoram/groot-cli/internal/workspace"
)

// newMentionModel builds a model over three spaces with homonyms:
// default has devjunior and reviewer, explore has search-specialist and
// reviewer, root has groot. The operator is Tinchoram.
func newMentionModel(t *testing.T) (Model, *fakeWorkspace, *fakeSession) {
	t.Helper()
	chat := &fakeSession{model: "m", reply: "ok"}
	ws := newFakeWorkspace(chat)
	ws.order = []string{"default", "explore", "root"}
	ws.spaces["default"] = workspace.SpaceInfo{Name: "default", Path: "/work/default", Agents: []string{"devjunior", "reviewer"}}
	ws.spaces["explore"] = workspace.SpaceInfo{Name: "explore", Path: "/work/explore", Agents: []string{"search-specialist", "reviewer"}}
	ws.spaces["root"] = workspace.SpaceInfo{Name: "root", Path: "/home/u", Agents: []string{"groot"}}
	ws.catalog = append(ws.catalog,
		workspace.AgentInfo{Name: "reviewer", Description: "Reviews diffs", Model: "m", Tools: []string{"read_file"}},
		workspace.AgentInfo{Name: "search-specialist", Description: "Finds code", Model: "m", Tools: []string{"read_file"}},
		workspace.AgentInfo{Name: "groot", Description: "Coordinator", Model: "m", Tools: []string{"send_message"}},
	)
	ws.operator = "Tinchoram"
	m := NewModel(Options{Workspace: ws, Version: "test"})
	m.width, m.height = 120, 40
	m.resize()
	return m, ws, chat
}

func TestMentionsOperatorDetectsWholeWordsOnly(t *testing.T) {
	tests := []struct {
		name string
		text string
		want bool
	}{
		{"plain", "Ping @Tinchoram about the rollout", true},
		{"case insensitive", "ping @tinchoram", true},
		{"start of text", "@Tinchoram: approve?", true},
		{"after punctuation", "(@Tinchoram)", true},
		{"longer name", "@Tinchoramx", false},
		{"address form", "@Tinchoram/desk", false},
		{"inside an address", "explore/@Tinchoram", false},
		{"email like", "write to me@Tinchoram.dev", false},
		{"no mention", "approve the change", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := mentionsName(tt.text, "Tinchoram"); got != tt.want {
				t.Fatalf("mentionsName(%q) = %v, want %v", tt.text, got, tt.want)
			}
		})
	}
}

// withColor turns the color profile on for the test, since styles render
// without escape codes when the output is not a terminal.
func withColor(t *testing.T) {
	t.Helper()
	old := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.TrueColor)
	t.Cleanup(func() { lipgloss.SetColorProfile(old) })
}

func TestHighlightMentionsStylesOperatorAndGrootOnly(t *testing.T) {
	withColor(t)
	line := "@Tinchoram and @groot, not @devjunior or explore/@groot"
	out := highlightMentions(line, "Tinchoram")

	if stripANSI(out) != line {
		t.Fatalf("highlight changed the text: %q", stripANSI(out))
	}
	if !strings.Contains(out, styleAccentBold.Render("@Tinchoram")) {
		t.Fatalf("operator mention not in accent: %q", out)
	}
	if !strings.Contains(out, styleAccentBold.Render("@groot")) {
		t.Fatalf("groot mention not in accent: %q", out)
	}
	if strings.Contains(out, styleAccentBold.Render("@devjunior")) {
		t.Fatalf("an agent mention must stay plain: %q", out)
	}
}

func TestHighlightMentionsSeesThroughStyleCodes(t *testing.T) {
	withColor(t)
	styled := styleDim.Render("see") + " " + styleDim.Render("@Tinchoram") + styleDim.Render(" now")
	out := highlightMentions(styled, "Tinchoram")
	if !strings.Contains(out, styleAccentBold.Render("@Tinchoram")) {
		t.Fatalf("a mention that starts a styled run must still be found: %q", out)
	}
}

func TestReplyMentioningOperatorMarksConversationNeedsYou(t *testing.T) {
	m, _, _ := newMentionModel(t)
	m.busy = true

	next, _ := m.Update(answerMsg{key: "explore/reviewer", text: "Approve the rollout, @Tinchoram?"})
	m = next.(Model)
	if !m.needsYou["explore/reviewer"] {
		t.Fatalf("a reply that mentions the operator must mark its conversation, got %v", m.needsYou)
	}
	if m.needsBadge("explore", "reviewer") == "" {
		t.Fatal("the marked agent must show the needs-you badge")
	}
	if m.needsBadge("default", "devjunior") != "" {
		t.Fatal("an unmarked agent must not show the badge")
	}
	if line := stripANSI(m.statusLine()); !strings.Contains(line, "@") {
		t.Fatalf("status line lacks the needs-you marker: %q", line)
	}
}

func TestNeedsYouMarkClearsWhenTheUserViewsTheConversation(t *testing.T) {
	m, ws, _ := newMentionModel(t)
	ws.spaces["explore"] = workspace.SpaceInfo{Name: "explore", Path: "/work/explore", Agents: []string{"search-specialist", "reviewer"}}
	m.needsYou = map[string]bool{"explore/reviewer": true}

	m.useSpace("explore")
	m.selectAgent("reviewer")
	if m.needsYou["explore/reviewer"] {
		t.Fatal("entering the conversation must clear its needs-you mark")
	}
}

func TestNeedsYouMarkClearsWhenTheUserSendsNextMessage(t *testing.T) {
	m, _, chat := newMentionModel(t)
	m.needsYou = map[string]bool{m.key: true}
	m = sendLine(t, m, "approved")
	if m.needsYou[m.key] {
		t.Fatal("sending a message in the conversation must clear its mark")
	}
	if len(chat.sends) != 1 {
		t.Fatalf("sends = %v", chat.sends)
	}
}

func TestMentionCandidatesCurrentSpaceFirstAndHomonymsQualified(t *testing.T) {
	m, _, _ := newMentionModel(t)
	m = typeText(m, "@")

	got := candidateLabels(m)
	want := []string{"devjunior", "default/reviewer", "explore/search-specialist", "explore/reviewer", "root/groot"}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Fatalf("candidates = %v, want %v", got, want)
	}
}

func TestMentionCandidatesExcludeTheOperator(t *testing.T) {
	m, ws, _ := newMentionModel(t)
	ws.spaces["default"] = workspace.SpaceInfo{Name: "default", Path: "/work/default", Agents: []string{"devjunior", "reviewer", "Tinchoram"}}
	m = typeText(m, "@tin")
	if got := candidateLabels(m); len(got) != 0 {
		t.Fatalf("the operator is not a candidate, got %v", got)
	}
}

func TestMentionCandidatesFilterByPrefixIgnoringCase(t *testing.T) {
	tests := []struct {
		typed string
		want  string
	}{
		{"@rev", "default/reviewer|explore/reviewer"},
		{"@DEV", "devjunior"},
		{"@explore/s", "explore/search-specialist"},
		{"@zzz", ""},
	}
	for _, tt := range tests {
		t.Run(tt.typed, func(t *testing.T) {
			m, _, _ := newMentionModel(t)
			m = typeText(m, tt.typed)
			if got := strings.Join(candidateLabels(m), "|"); got != tt.want {
				t.Fatalf("typed %q: candidates %q, want %q", tt.typed, got, tt.want)
			}
		})
	}
}

func TestMentionListCapsAtEightRows(t *testing.T) {
	m, ws, _ := newMentionModel(t)
	for i := 0; i < 12; i++ {
		name := "agent" + string(rune('a'+i))
		ws.spaces["default"] = workspace.SpaceInfo{Name: "default", Path: "/work/default", Agents: append(ws.spaces["default"].Agents, name)}
	}
	m = typeText(m, "@")
	if n := len(m.completionCandidates()); n != 8 {
		t.Fatalf("mention list has %d rows, want at most 8", n)
	}
}

func TestMentionTabAndEnterInsertTheAddress(t *testing.T) {
	tests := []struct {
		name string
		key  tea.KeyType
	}{
		{"tab", tea.KeyTab},
		{"enter", tea.KeyEnter},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m, _, chat := newMentionModel(t)
			m = typeText(m, "ask @rev")
			m, _ = press(m, tt.key)
			if got := m.input.Value(); got != "ask @default/reviewer " {
				t.Fatalf("input = %q, want the highlighted address and a space", got)
			}
			if len(chat.sends) != 0 {
				t.Fatalf("accepting a candidate must not send: %v", chat.sends)
			}
		})
	}
}

func TestMentionUpDownChooseAndEscCloses(t *testing.T) {
	m, _, _ := newMentionModel(t)
	m = typeText(m, "@rev")
	m, _ = press(m, tea.KeyDown)
	m, _ = press(m, tea.KeyTab)
	if got := m.input.Value(); got != "@explore/reviewer " {
		t.Fatalf("down then tab = %q, want the second candidate", got)
	}
}

func TestMentionEscClosesUntilTheInputChanges(t *testing.T) {
	m, _, _ := newMentionModel(t)
	m = typeText(m, "@rev")
	m, _ = press(m, tea.KeyEsc)
	if len(m.completionCandidates()) != 0 {
		t.Fatal("esc must close the mention list")
	}
	if got := m.input.Value(); got != "@rev" {
		t.Fatalf("esc must not edit the input, got %q", got)
	}
	m = typeText(m, "i")
	if got := candidateLabels(m); strings.Join(got, "|") != "default/reviewer|explore/reviewer" {
		t.Fatalf("typing again must filter again, got %v", got)
	}
}

func TestMentionListStaysOffForSlashCommands(t *testing.T) {
	m, _, _ := newMentionModel(t)
	m = typeText(m, "/bug fix @dev")
	if len(m.completionCandidates()) != 0 {
		t.Fatal("a slash command with an @ in its text must not open the mention list")
	}
}

func TestMentionListOpensOnSecondLine(t *testing.T) {
	m, _, _ := newMentionModel(t)
	m = typeText(m, "one")
	m.input.InsertString("\n")
	m = typeText(m, "@rev")
	if got := candidateLabels(m); len(got) == 0 {
		t.Fatal("a mention on the second line of a message must open the list")
	}
}

func TestSlashCommandStillRunsWhenTextHasAt(t *testing.T) {
	m, _, chat := newMentionModel(t)
	m = sendLine(t, m, "/help @devjunior")
	if !strings.Contains(stripANSI(m.transcriptText()), "available commands:") {
		t.Fatalf("/help with an @ in its argument did not run:\n%s", stripANSI(m.transcriptText()))
	}
	if len(chat.sends) != 0 {
		t.Fatalf("a command must not reach the agent: %v", chat.sends)
	}
}

func TestLeadingMentionAsksAgentInAnotherSpaceWithoutSwitching(t *testing.T) {
	m, ws, chat := newMentionModel(t)
	m = sendLine(t, m, "@explore/search-specialist find the retry loop")

	if ws.active != "default" || ws.agent != "devjunior" {
		t.Fatalf("selection = %s/%s, the ask must not switch", ws.active, ws.agent)
	}
	if len(ws.asked) != 1 || ws.asked[0] != "default/devjunior|explore/search-specialist|find the retry loop" {
		t.Fatalf("asked = %q: the leading mention must be stripped and the rest asked", ws.asked)
	}
	if len(chat.sends) != 0 {
		t.Fatalf("the ask must not reach the screen's agent: %v", chat.sends)
	}
}

func TestBareLeadingMentionOnlySwitches(t *testing.T) {
	m, ws, chat := newMentionModel(t)
	m = sendLine(t, m, "@explore/search-specialist")
	if ws.active != "explore" || ws.agent != "search-specialist" {
		t.Fatalf("selection = %s/%s", ws.active, ws.agent)
	}
	if len(chat.sends) != 0 {
		t.Fatalf("a bare mention must not send: %v", chat.sends)
	}
}

func TestLeadingMentionPrefersCurrentSpaceForHomonyms(t *testing.T) {
	m, ws, chat := newMentionModel(t)
	m = sendLine(t, m, "@reviewer look at the diff")
	if len(ws.asked) != 1 || ws.asked[0] != "default/devjunior|default/reviewer|look at the diff" {
		t.Fatalf("asked = %q, want the reviewer of the current space", ws.asked)
	}
	if len(chat.sends) != 0 {
		t.Fatalf("sends = %v", chat.sends)
	}
}

func TestUnknownOrAmbiguousTargetSendsNothing(t *testing.T) {
	tests := []struct {
		name string
		line string
		note string
	}{
		{
			name: "unknown agent",
			line: "@nobody do it",
			note: "no agent @nobody; agents: groot, default/devjunior, default/reviewer, explore/search-specialist, explore/reviewer",
		},
		{
			name: "homonym from a space without it",
			line: "@reviewer hi",
			note: "@reviewer is in more than one space: default/reviewer, explore/reviewer; write the space too",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m, ws, chat := newMentionModel(t)
			ws.order = []string{"root", "default", "explore"}
			if err := ws.UseSpace("root"); err != nil {
				t.Fatal(err)
			}
			m.syncConversation()
			m = sendLine(t, m, tt.line)
			if !strings.Contains(stripANSI(m.transcriptText()), tt.note) {
				t.Fatalf("missing note %q in:\n%s", tt.note, stripANSI(m.transcriptText()))
			}
			if len(chat.sends) != 0 {
				t.Fatalf("nothing may be sent: %v", chat.sends)
			}
		})
	}
}

func TestMentionsInTextAddResolvedNote(t *testing.T) {
	m, _, chat := newMentionModel(t)
	m = sendLine(t, m, "ask @explore/search-specialist and @devjunior, cc @Tinchoram and @nobody, again @devjunior")
	want := "ask @explore/search-specialist and @devjunior, cc @Tinchoram and @nobody, again @devjunior\n[mentions: explore/search-specialist, default/devjunior]"
	if len(chat.sends) != 1 || chat.sends[0] != want {
		t.Fatalf("sent %q, want %q", chat.sends, want)
	}
}

func TestNoMentionNoteWithoutAgents(t *testing.T) {
	m, _, chat := newMentionModel(t)
	m = sendLine(t, m, "cc @Tinchoram only")
	if len(chat.sends) != 1 || chat.sends[0] != "cc @Tinchoram only" {
		t.Fatalf("sent %q: an operator mention is not an agent and adds no note", chat.sends)
	}
}

func TestAgentReplyHighlightsMentionsInTranscript(t *testing.T) {
	withColor(t)
	m, _, chat := newMentionModel(t)
	chat.reply = "Approve it, @Tinchoram? @groot can run the change."
	m = sendLine(t, m, "plan")
	out := m.transcriptText()
	if !strings.Contains(out, styleAccentBold.Render("@Tinchoram")) || !strings.Contains(out, styleAccentBold.Render("@groot")) {
		t.Fatalf("mentions in the reply must be highlighted:\n%q", out)
	}
	if !strings.Contains(stripANSI(out), "Approve it, @Tinchoram?") {
		t.Fatalf("the reply text changed:\n%s", stripANSI(out))
	}
}

// candidateLabels returns the addresses the open mention list offers, without
// the leading @.
func candidateLabels(m Model) []string {
	var out []string
	for _, it := range m.completionCandidates() {
		out = append(out, strings.TrimPrefix(it.insert, "@"))
	}
	return out
}
