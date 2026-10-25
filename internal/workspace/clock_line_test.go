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

	for i, sys := range []string{first, second} {
		want := "Current time: " + instants[i].In(time.Local).Format("2006-01-02 15:04") + " ("
		if !strings.HasPrefix(sys, want) {
			t.Fatalf("turn %d: system does not open with %q:\n%s", i+1, want, sys)
		}
	}
	if first == second {
		t.Fatal("two turns carried the same system prompt; the time line must be fresh")
	}
	baseOf := func(sys string) string { return sys[strings.Index(sys, "\n\n")+2:] }
	if baseOf(first) != baseOf(second) {
		t.Fatal("the base prompt changed between turns; only the time line may differ")
	}
}
