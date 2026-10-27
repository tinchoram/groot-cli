package workspace

import (
	"context"
	"strings"
	"testing"
	"time"
)

// ackFrom sends an ack from writer to reader through the messenger.
func ackFrom(t *testing.T, f *fixture, body string) {
	t.Helper()
	if sp, _ := f.spaces.Get("default"); !contains(sp.Agents, "writer") {
		addAgentToDefault(t, f, "writer")
	}
	if _, _, err := f.ws.messengerFor("default", "writer").Ack(context.Background(), "reader", body); err != nil {
		t.Fatal(err)
	}
}

func TestAckIsNeverInjectedIntoATurn(t *testing.T) {
	f := newFixture(t, text("ok"))
	ackFrom(t, f, "FEAT-016 recorded")
	_ = f.ws.SelectAgent("reader")
	if _, err := f.ws.Chat().Send(context.Background(), "go"); err != nil {
		t.Fatal(err)
	}
	prompt := f.mock.Calls[0].Messages[0].Content[0].Text
	if strings.Contains(prompt, "FEAT-016") || strings.Contains(prompt, "[inbox]") {
		t.Fatalf("an ack reached the turn:\n%s", prompt)
	}
	// The turn opens with its time line; the user's text is the rest of it.
	if !strings.HasPrefix(prompt, "Current time: ") || !strings.HasSuffix(prompt, "\n\ngo") {
		t.Fatalf("prompt = %q, want the time line and the user's text alone", prompt)
	}
}

func TestAckDoesNotCountOrTriggerATurnInWorkMode(t *testing.T) {
	f := newFixture(t, text("auto reply"))
	h := wireTriggers(f)
	_ = f.ws.SelectAgent("reader")
	_ = f.ws.Chat()
	if err := f.ws.SetWorkMode("default", "reader", true); err != nil {
		t.Fatal(err)
	}
	ackFrom(t, f, "FEAT-016 recorded")

	select {
	case got := <-h.notices:
		t.Fatalf("an ack started a turn: %q", got)
	case <-time.After(150 * time.Millisecond):
	}
	counts, err := f.ws.UnreadCounts(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if n, ok := counts["default/reader"]; ok {
		t.Fatalf("the sidebar badge counts an ack: %d", n)
	}
}

func TestAcksAreTakenOnceForTheActiveAgent(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t, text("ok"))
	_ = f.ws.SelectAgent("reader")
	ackFrom(t, f, "FEAT-016 recorded")

	first, err := f.ws.TakeAcks(ctx)
	if err != nil || len(first) != 1 || first[0].Body != "FEAT-016 recorded" {
		t.Fatalf("TakeAcks = %+v (%v), want the one ack", first, err)
	}
	second, err := f.ws.TakeAcks(ctx)
	if err != nil || len(second) != 0 {
		t.Fatalf("second TakeAcks = %+v (%v), want none: an ack is shown once", second, err)
	}
	if hist, _ := f.ws.Inbox(ctx, 10); len(hist) != 1 || !hist[0].IsAck() {
		t.Fatalf("history must keep the ack: %+v", hist)
	}
}

// Debug confirmations go out as acks, whatever the confirm setting: the reply
// rule is appended to the prompt on every open of debug.
func TestDebugConfirmationsAreSentAsAcks(t *testing.T) {
	for _, confirm := range []bool{true, false} {
		rule := debugReplyRule(confirm)
		if !strings.Contains(rule, "ack: true") {
			t.Fatalf("confirm=%v: reply rule does not ask for ack: true:\n%s", confirm, rule)
		}
	}
	if on := debugReplyRule(true); !strings.Contains(on, `"<ID> recorded"`) || !strings.Contains(on, "ack: true") {
		t.Fatalf("the recorded confirmation must be an ack:\n%s", on)
	}
}
