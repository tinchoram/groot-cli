package ui

import (
	"context"
	"strings"
	"testing"

	"github.com/tinchoram/groot-cli/internal/workspace"
)

// ContextTokens and ContextWindow answer the fake session's context.
func (f *fakeSession) ContextTokens() int64 { return f.ctxTokens }
func (f *fakeSession) ContextWindow() int   { return f.window }

// Contexts answers the fake's rows and records the all flag.
func (f *fakeWorkspace) Contexts(_ context.Context, all bool) ([]workspace.ContextRow, error) {
	f.contextAll = append(f.contextAll, all)
	return f.contexts, nil
}

func (f *fakeWorkspace) SetContextWindow(model string, tokens int) error {
	if f.windowErr != nil {
		return f.windowErr
	}
	if f.windowSet == nil {
		f.windowSet = map[string]int{}
	}
	f.windowSet[model] = tokens
	return nil
}

func TestContextBadgeColorsByBand(t *testing.T) {
	if got := contextBadge(84_000, 200_000); got != styleDim.Render("42%") {
		t.Fatalf("under 70 percent = %q, want the dim style", got)
	}
	if got := contextBadge(140_000, 200_000); got != styleTool.Render("70%") {
		t.Fatalf("from 70 percent = %q, want the warning style", got)
	}
	if got := contextBadge(180_000, 200_000); got != styleError.Render("90%") {
		t.Fatalf("from 90 percent = %q, want the error style", got)
	}
	if got := contextBadge(0, 200_000); got != "" {
		t.Fatalf("an unknown context shows nothing: %q", got)
	}
}

func TestContextBadgeComposesWithTheSpendBadge(t *testing.T) {
	spend := spendBadge(0.90, 2)
	if got := stripANSI(joinBadges(spend, contextBadge(84_000, 200_000))); got != " $0.90/$2.00 · 42%" {
		t.Fatalf("spend and context = %q, want \" $0.90/$2.00 · 42%%\"", got)
	}
	if got := stripANSI(joinBadges("", contextBadge(84_000, 200_000))); got != " 42%" {
		t.Fatalf("context alone = %q, want \" 42%%\"", got)
	}
}

func TestHeaderShowsContextWithTheAbsoluteWhenItFits(t *testing.T) {
	chat := &fakeSession{model: "m", ctxTokens: 84_000, window: 200_000}
	m, _ := newTestModel(t, chat)
	m.width = 160
	if out := stripANSI(m.header()); !strings.Contains(out, "ctx 84k/200k 42%") {
		t.Fatalf("header lacks the full context:\n%s", out)
	}
}

func TestHeaderFallsBackToThePercentageWhenNarrow(t *testing.T) {
	chat := &fakeSession{model: "m", ctxTokens: 84_000, window: 200_000}
	m, _ := newTestModel(t, chat)
	m.sidebarOpen = false
	m.width = 40
	m.resize()
	out := stripANSI(m.header())
	if !strings.Contains(out, "ctx 42%") || strings.Contains(out, "84k") {
		t.Fatalf("narrow header should show only the percentage:\n%s", out)
	}
}

func TestHeaderHasNoContextBeforeTheFirstAnswer(t *testing.T) {
	m, _ := newTestModel(t, &fakeSession{model: "m", window: 200_000})
	if out := stripANSI(m.header()); strings.Contains(out, "ctx") {
		t.Fatalf("no answer yet, header must not show a context: %s", out)
	}
}

func TestSidebarShowsTheContextOfEachAgent(t *testing.T) {
	m, ws := newTestModel(t, &fakeSession{model: "m"})
	active, _ := ws.ActiveSpace()
	agent := ws.Agents()[0].Name
	ws.contexts = []workspace.ContextRow{{
		Space: active.Name, Agent: agent, Model: "claude-sonnet-5-5",
		Window: 200_000, Tokens: 170_000, Percent: 85,
	}}
	m.refreshUnread()
	out := stripANSI(strings.Join(m.sidebarLines(40, false), "\n"))
	if !strings.Contains(out, agent+" 85%") {
		t.Fatalf("sidebar lacks the agent context:\n%s", out)
	}
}

func TestContextCommandPrintsTheTable(t *testing.T) {
	m, ws := newTestModel(t, &fakeSession{model: "m"})
	agent := ws.Agents()[0].Name
	ws.contexts = []workspace.ContextRow{{
		Space: "default", Agent: agent, Model: "claude-sonnet-5-5",
		Window: 200_000, Tokens: 84_000, Percent: 42, Strategy: "none",
	}}
	m = sendLine(t, m, "/context")
	out := stripANSI(m.transcriptText())
	for _, want := range []string{agent, "claude-sonnet-5-5", "84k", "42%", "no (strategy none)"} {
		if !strings.Contains(out, want) {
			t.Fatalf("/context lacks %q:\n%s", want, out)
		}
	}
	if got := ws.contextAll; len(got) == 0 || got[len(got)-1] {
		t.Fatalf("/context must ask for the active space only: %v", got)
	}
}

func TestContextAllAsksForEverySpace(t *testing.T) {
	m, ws := newTestModel(t, &fakeSession{model: "m"})
	m = sendLine(t, m, "/context all")
	if got := ws.contextAll; len(got) == 0 || !got[len(got)-1] {
		t.Fatalf("/context all must ask for every space: %v", got)
	}
}

func TestContextWindowCommandSavesAndPrints(t *testing.T) {
	m, ws := newTestModel(t, &fakeSession{model: "m"})
	m = sendLine(t, m, "/context window claude-haiku-5-5 128000")
	if ws.windowSet["claude-haiku-5-5"] != 128000 {
		t.Fatalf("window not saved: %v", ws.windowSet)
	}
	if out := stripANSI(m.transcriptText()); !strings.Contains(out, "context window of claude-haiku-5-5 set to 128000 tokens") {
		t.Fatalf("/context window must confirm:\n%s", out)
	}
}

func TestContextWindowCommandRefusesBadInput(t *testing.T) {
	m, ws := newTestModel(t, &fakeSession{model: "m"})
	m = sendLine(t, m, "/context window claude-haiku-5-5 lots")
	m = sendLine(t, m, "/context window")
	if len(ws.windowSet) != 0 {
		t.Fatalf("bad input must not be saved: %v", ws.windowSet)
	}
	if out := stripANSI(m.transcriptText()); !strings.Contains(out, "usage: /context") {
		t.Fatalf("bad input must print the usage:\n%s", out)
	}
}

func TestContextOverflowNoticeHasNoWarningPrefix(t *testing.T) {
	if got := warningLine("context overflow: compacted history and retried"); got != "context overflow: compacted history and retried" {
		t.Fatalf("overflow notice = %q, want the plain line", got)
	}
	if got := warningLine("context at 93%, set compaction.strategy to window or summarize"); !strings.HasPrefix(got, "warning: ") {
		t.Fatalf("the 90%% notice is a warning: %q", got)
	}
}
