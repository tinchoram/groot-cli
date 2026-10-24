package workspace

import (
	"context"
	"strings"
	"testing"
	"time"
)

// deliverFromWriter sends a message from writer to reader through the messenger.
func deliverFromWriter(t *testing.T, f *fixture, subject, body string) {
	t.Helper()
	if sp, _ := f.spaces.Get("default"); !contains(sp.Agents, "writer") {
		addAgentToDefault(t, f, "writer")
	}
	if _, _, err := f.ws.messengerFor("default", "writer").Send(context.Background(), "reader", subject, body); err != nil {
		t.Fatal(err)
	}
}

func TestTurnReceivesTheUnreadMessagesAndMarksThemRead(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t, text("ok"))
	for i := 0; i < 7; i++ {
		deliverFromWriter(t, f, "note", "body")
	}
	_ = f.ws.SelectAgent("reader")
	conv := f.ws.Chat()
	if _, err := conv.Send(ctx, "go"); err != nil {
		t.Fatal(err)
	}
	prompt := f.mock.Calls[0].Messages[0].Content[0].Text
	if n := strings.Count(prompt, "[inbox] from default/writer"); n != inboxInjectMax {
		t.Fatalf("the turn gets %d messages, want %d", n, inboxInjectMax)
	}
	if !strings.Contains(prompt, "2 more unread messages stay in the inbox") || !strings.HasSuffix(prompt, "go") {
		t.Fatalf("the rest is noted and the prompt comes last:\n%s", prompt)
	}
	if n, _ := f.ws.inbox.CountUnread(ctx, "default", "reader"); n != 2 {
		t.Fatalf("unread after the turn = %d, want the 2 that did not fit", n)
	}
}

func TestTurnCapsTheInjectedTextAtEightKilobytes(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t, text("ok"))
	big := strings.Repeat("x", 4000)
	for i := 0; i < 3; i++ {
		deliverFromWriter(t, f, "big", big)
	}
	_ = f.ws.SelectAgent("reader")
	if _, err := f.ws.Chat().Send(ctx, "go"); err != nil {
		t.Fatal(err)
	}
	prompt := f.mock.Calls[0].Messages[0].Content[0].Text
	if n := strings.Count(prompt, "[inbox] from default/writer"); n != 2 {
		t.Fatalf("two 4 KB messages fit in 8 KB, got %d", n)
	}
	if len(prompt) > inboxInjectBytes+1024 {
		t.Fatalf("the injected text is %d bytes", len(prompt))
	}
}

// triggerHooks collects what the workspace reports when it starts turns itself.
type triggerHooks struct {
	answers  chan string
	notices  chan string
	warnings chan string
}

func wireTriggers(f *fixture) triggerHooks {
	h := triggerHooks{answers: make(chan string, 4), notices: make(chan string, 4), warnings: make(chan string, 4)}
	f.ws.opts.Hooks.OnAnswer = func(key, text string, err error) { h.answers <- text }
	f.ws.opts.Hooks.OnNotice = func(key, text string) { h.notices <- text }
	f.ws.opts.Hooks.OnWarning = func(key, msg string) { h.warnings <- msg }
	return h
}

func awaitWarning(t *testing.T, ch chan string, contains string) string {
	t.Helper()
	deadline := time.After(2 * time.Second)
	for {
		select {
		case v := <-ch:
			if strings.Contains(v, contains) {
				return v
			}
		case <-deadline:
			t.Fatalf("no warning containing %q", contains)
		}
	}
}

func awaitString(t *testing.T, ch chan string, what string) string {
	t.Helper()
	select {
	case v := <-ch:
		return v
	case <-time.After(2 * time.Second):
		t.Fatalf("no %s", what)
	}
	return ""
}

func TestMessageTriggersATurnInWorkMode(t *testing.T) {
	f := newFixture(t, text("auto reply"))
	h := wireTriggers(f)
	_ = f.ws.SelectAgent("reader")
	conv := f.ws.Chat()
	if err := f.ws.SetWorkMode("default", "reader", true); err != nil {
		t.Fatal(err)
	}
	_ = conv

	deliverFromWriter(t, f, "spec", "build the thing")

	if got := awaitString(t, h.notices, "trigger notice"); !strings.Contains(got, "triggered by message") {
		t.Fatalf("notice = %q", got)
	}
	if got := awaitString(t, h.answers, "answer of the triggered turn"); got != "auto reply" {
		t.Fatalf("answer = %q", got)
	}
	prompt := f.mock.Calls[0].Messages[0].Content[0].Text
	if !strings.Contains(prompt, "build the thing") {
		t.Fatalf("the triggered turn reads the message:\n%s", prompt)
	}
}

func TestMessageDoesNotTriggerATurnOutsideWorkMode(t *testing.T) {
	f := newFixture(t, text("never"))
	h := wireTriggers(f)
	_ = f.ws.SelectAgent("reader")
	_ = f.ws.Chat()

	deliverFromWriter(t, f, "spec", "build the thing")

	select {
	case <-h.answers:
		t.Fatal("without work mode nothing runs: the message waits for the next turn")
	case <-time.After(150 * time.Millisecond):
	}
	if n, _ := f.ws.inbox.CountUnread(context.Background(), "default", "reader"); n != 1 {
		t.Fatalf("the message stays unread for the badge, unread = %d", n)
	}
}

func TestAutomaticTurnsAreCappedPerSession(t *testing.T) {
	f := newFixture(t, text("first auto reply"), text("never sent"))
	f.ws.opts.Config.Work.MaxAutoTurns = 1
	h := wireTriggers(f)
	_ = f.ws.SelectAgent("reader")
	_ = f.ws.Chat()
	if err := f.ws.SetWorkMode("default", "reader", true); err != nil {
		t.Fatal(err)
	}

	deliverFromWriter(t, f, "one", "first")
	awaitString(t, h.answers, "first triggered answer")

	deliverFromWriter(t, f, "two", "second")
	if got := awaitWarning(t, h.warnings, "automatic turn limit (1 per session)"); !strings.Contains(got, "automatic turn limit (1 per session)") {
		t.Fatalf("warning = %q", got)
	}
	if n := len(f.mock.Calls); n != 1 {
		t.Fatalf("the capped message made a model call: %d calls", n)
	}
}
