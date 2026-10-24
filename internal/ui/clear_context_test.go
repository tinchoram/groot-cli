package ui

import (
	"strings"
	"testing"

	"github.com/tinchoram/groot-cli/internal/workspace"
)

func TestClearResetsTheHeaderAndTheSidebarContext(t *testing.T) {
	chat := &fakeSession{model: "m", ctxTokens: 170_000, window: 200_000}
	m, ws := newTestModel(t, chat)
	active, _ := ws.ActiveSpace()
	agent := ws.Agents()[0].Name
	fullRow := workspace.ContextRow{
		Space: active.Name, Agent: agent, Model: "claude-sonnet-5-5",
		Window: 200_000, Tokens: 170_000, Percent: 85,
	}
	ws.contexts = []workspace.ContextRow{fullRow}
	m.refreshUnread()

	// What the workspace reports once /clear has detached the conversation:
	// the live conversation has no context and the latest session is the new one.
	chat.onClear = func() {
		chat.ctxTokens = 0
		ws.contexts = []workspace.ContextRow{{
			Space: active.Name, Agent: agent, Model: "claude-sonnet-5-5",
			Window: 200_000, Tokens: 0, Percent: 0,
		}}
	}

	if out := stripANSI(m.header()); !strings.Contains(out, "ctx 170k/200k 85%") {
		t.Fatalf("before /clear the header must show the context:\n%s", out)
	}
	if out := stripANSI(strings.Join(m.sidebarLines(40, false), "\n")); !strings.Contains(out, agent+" 85%") {
		t.Fatalf("before /clear the sidebar must show the context:\n%s", out)
	}

	m.input.SetValue("/clear")
	next, _ := m.submit()
	m = next.(Model)

	if out := stripANSI(m.header()); strings.Contains(out, "ctx") {
		t.Fatalf("after /clear the header must show no context:\n%s", out)
	}
	if out := stripANSI(strings.Join(m.sidebarLines(40, false), "\n")); strings.Contains(out, "85%") {
		t.Fatalf("after /clear the sidebar must drop the agent context:\n%s", out)
	}
}
