package ui

import (
	"strings"
	"testing"
)

// The status line above the input names the active agent as a badge, so the
// user always sees who they are writing to, whatever the line shows next.
func TestStatusLineStartsWithTheAgentBadge(t *testing.T) {
	m, ws := newTestModel(t, costSession())
	ws.agent = "search-specialist"

	out := stripANSI(m.statusLine())
	if !strings.HasPrefix(strings.TrimSpace(out), "search-specialist") {
		t.Fatalf("idle status line must start with the agent: %q", out)
	}
	if strings.Index(out, "search-specialist") > strings.Index(out, "m ·") {
		t.Fatalf("the agent goes before the model: %q", out)
	}

	m.busy = true
	if out := stripANSI(m.statusLine()); !strings.Contains(out, "search-specialist") || !strings.Contains(out, "thinking...") {
		t.Fatalf("busy status line keeps the agent: %q", out)
	}
	m.busy = false

	m.closing = true
	if out := stripANSI(m.statusLine()); !strings.Contains(out, "search-specialist") {
		t.Fatalf("closing status line keeps the agent: %q", out)
	}
}

// Colours are not emitted without a terminal, so the test checks the badge
// goes through its style (the padding shows) rather than the escape codes.
func TestAgentBadgeUsesTheBadgeStyle(t *testing.T) {
	m, ws := newTestModel(t, costSession())
	ws.agent = "search-specialist"
	badge := m.agentBadge()
	if want := styleAgentBadge.Render("search-specialist"); badge != want {
		t.Fatalf("badge = %q, want %q", badge, want)
	}
	if stripANSI(badge) != " search-specialist " {
		t.Fatalf("badge must be padded: %q", stripANSI(badge))
	}
}

func TestAgentBadgeWithoutAgent(t *testing.T) {
	m, ws := newTestModel(t, costSession())
	ws.agent = ""
	if b := m.agentBadge(); b != "" {
		t.Fatalf("no agent, no badge: %q", b)
	}
	if out := stripANSI(m.statusLine()); !strings.Contains(out, "m") {
		t.Fatalf("status line still shows the model: %q", out)
	}
}
