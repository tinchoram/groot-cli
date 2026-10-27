package workspace

import (
	"context"
	"strings"
	"testing"
	"time"
)

// Every turn gets a fresh time line in front of the base prompt; the base prompt
// itself, which is cached with the agent, does not change between turns.
func TestEveryTurnSeesItsOwnTimeLineAndTheBasePromptStaysPut(t *testing.T) {
	f := newFixture(t, text("one"), text("two"))
	at := time.Date(2026, 10, 25, 21, 42, 7, 0, time.UTC)
	f.ws.opts.Now = func() time.Time { return at }
	_ = f.ws.SelectAgent("reader")

	instants := []time.Time{at.Add(time.Hour), at.Add(2 * time.Hour)}
	for i, prompt := range []string{"first", "second"} {
		at = instants[i]
		if _, err := f.ws.Chat().Send(context.Background(), prompt); err != nil {
			t.Fatal(err)
		}
	}
	if len(f.mock.Calls) < 2 {
		t.Fatalf("calls = %d, want 2", len(f.mock.Calls))
	}
	first, second := f.mock.Calls[0].System, f.mock.Calls[1].System

	// The time line is per turn: it opens the user message of its turn.
	for i := range instants {
		turn := lastUserText(f.mock.Calls[i])
		want := "Current time: " + instants[i].In(time.Local).Format("2006-01-02 15:04") + " ("
		if !strings.HasPrefix(turn, want) {
			t.Fatalf("turn %d: user turn does not open with %q:\n%s", i+1, want, turn)
		}
	}
	// The system prompt is the same bytes on every turn, so the cached prefix holds.
	if first != second {
		t.Fatal("the system prompt must not change between turns")
	}
	if strings.Contains(first, "Current time:") {
		t.Fatal("the time line is not part of the system prompt")
	}
}
