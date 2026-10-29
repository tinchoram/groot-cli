package workspace

import (
	"context"
	"strings"
	"testing"
)

// isOpen reports whether the conversation of an agent in the default space is open.
func isOpen(f *fixture, agent string) bool {
	f.ws.mu.Lock()
	defer f.ws.mu.Unlock()
	_, ok := f.ws.convs["default/"+agent]
	return ok
}

func TestMessageToAgentWithoutConversationStartsOneTurn(t *testing.T) {
	f := newFixture(t, text("hello back"))
	h := wireTriggers(f)
	if isOpen(f, "reader") {
		t.Fatal("the reader must start closed")
	}

	deliverFromWriter(t, f, "note", "are you there?")

	if got := awaitString(t, h.answers, "answer of the opened agent"); got != "hello back" {
		t.Fatalf("answer = %q", got)
	}
	if !isOpen(f, "reader") {
		t.Fatal("the message must open the agent's conversation")
	}
	if len(f.mock.Calls) != 1 {
		t.Fatalf("turns = %d, want one", len(f.mock.Calls))
	}
	if prompt := f.mock.Calls[0].Messages[0].Content[0].Text; !strings.Contains(prompt, "are you there?") {
		t.Fatalf("the turn must read the message:\n%s", prompt)
	}
}

func TestLazyOpenKeepsTheAutomaticTurnLimit(t *testing.T) {
	f := newFixture(t)
	f.ws.opts.Config.Work.MaxAutoTurns = 0
	h := wireTriggers(f)

	deliverFromWriter(t, f, "note", "waits")

	awaitWarning(t, h.warnings, "automatic turns: the limit")
	if !isOpen(f, "reader") {
		t.Fatal("the conversation opens even at the limit")
	}
	if len(f.mock.Calls) != 0 {
		t.Fatalf("at the limit no turn may start, got %d", len(f.mock.Calls))
	}
	if n, _ := f.ws.inbox.CountUnread(context.Background(), "default", "reader"); n != 1 {
		t.Fatalf("the message must wait in the inbox, unread = %d", n)
	}
}
