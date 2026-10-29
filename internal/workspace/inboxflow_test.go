package workspace

import (
	"context"
	"strings"
	"sync"
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
	// The deliveries would open the reader and start its turns: the test reads the inbox only.
	f.ws.opts.Config.Work.MaxAutoTurns = 0
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
	f.ws.opts.Config.Work.MaxAutoTurns = 0
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

func TestMessageStartsATurnOutsideWorkMode(t *testing.T) {
	f := newFixture(t, text("reply"))
	h := wireTriggers(f)
	_ = f.ws.SelectAgent("reader")
	_ = f.ws.Chat()

	deliverFromWriter(t, f, "spec", "build the thing")

	if got := awaitString(t, h.notices, "trigger notice"); !strings.Contains(got, "triggered by message") {
		t.Fatalf("notice = %q", got)
	}
	if got := awaitString(t, h.answers, "answer of the triggered turn"); got != "reply" {
		t.Fatalf("answer = %q", got)
	}
	if prompt := f.mock.Calls[0].Messages[0].Content[0].Text; !strings.Contains(prompt, "build the thing") {
		t.Fatalf("the turn reads the message:\n%s", prompt)
	}
}

func TestReplyReachesTheSenderWithoutUserAction(t *testing.T) {
	f := newFixture(t,
		toolStep("tu_1", "send_message", map[string]string{"to": "writer", "subject": "done", "body": "built it"}),
		text("sent"),
	)
	saveAgent(t, f, "reader", "builds things", []string{"read_inbox", "send_message"})
	if err := f.ws.reload(); err != nil {
		t.Fatal(err)
	}
	h := wireTriggers(f)
	_ = f.ws.SelectAgent("reader")
	_ = f.ws.Chat()

	deliverFromWriter(t, f, "spec", "build the thing")
	awaitString(t, h.answers, "answer of the triggered turn")

	if n, _ := f.ws.inbox.CountUnread(context.Background(), "default", "writer"); n != 1 {
		t.Fatalf("writer unread = %d, want the reply of the triggered turn", n)
	}
}

func TestMessageDuringATurnStartsOneFollowUpWhenItEnds(t *testing.T) {
	f := newFixture(t, text("first"), text("follow-up"))
	h := wireTriggers(f)
	started, release := make(chan struct{}), make(chan struct{})
	var once sync.Once
	// The first text of the running turn holds the turn until the test releases it.
	f.ws.opts.Hooks.OnText = func(string, string) {
		once.Do(func() {
			close(started)
			<-release
		})
	}
	_ = f.ws.SelectAgent("reader")
	conv := f.ws.Chat()
	done := make(chan error, 1)
	go func() {
		_, err := conv.Send(context.Background(), "go")
		done <- err
	}()
	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("the running turn did not start")
	}

	deliverFromWriter(t, f, "late", "arrived mid-turn")
	select {
	case got := <-h.notices:
		t.Fatalf("a busy agent started a turn: %q", got)
	case <-time.After(150 * time.Millisecond):
	}

	close(release)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if got := awaitString(t, h.answers, "follow-up answer"); got != "follow-up" {
		t.Fatalf("follow-up answer = %q", got)
	}
	if n := len(f.mock.Calls); n != 2 {
		t.Fatalf("model calls = %d, want the running turn and one follow-up", n)
	}
	last := f.mock.Calls[1].Messages[len(f.mock.Calls[1].Messages)-1].Content[0].Text
	if !strings.Contains(last, "arrived mid-turn") {
		t.Fatalf("the follow-up reads the message:\n%s", last)
	}
}

func TestAutomaticTurnsAreCappedPerSession(t *testing.T) {
	f := newFixture(t, text("first auto reply"), text("never sent"))
	f.ws.opts.Config.Work.MaxAutoTurns = 1
	h := wireTriggers(f)
	_ = f.ws.SelectAgent("reader")
	_ = f.ws.Chat()

	deliverFromWriter(t, f, "one", "first")
	awaitString(t, h.answers, "first triggered answer")

	deliverFromWriter(t, f, "two", "second")
	want := "automatic turns: the limit (1 per session) was reached; the message from default/writer waits in the inbox (/readinbox)"
	if got := awaitWarning(t, h.warnings, "automatic turns: the limit"); got != want {
		t.Fatalf("warning = %q, want %q", got, want)
	}
	if n := len(f.mock.Calls); n != 1 {
		t.Fatalf("the capped message made a model call: %d calls", n)
	}
}

func TestLimitWarningShowsOncePerSession(t *testing.T) {
	f := newFixture(t, text("first auto reply"))
	f.ws.opts.Config.Work.MaxAutoTurns = 1
	h := wireTriggers(f)
	_ = f.ws.SelectAgent("reader")
	_ = f.ws.Chat()

	deliverFromWriter(t, f, "one", "first")
	awaitString(t, h.answers, "first triggered answer")
	deliverFromWriter(t, f, "two", "second")
	deliverFromWriter(t, f, "three", "third")

	warned := 0
	deadline := time.After(300 * time.Millisecond)
	for done := false; !done; {
		select {
		case got := <-h.warnings:
			if strings.Contains(got, "automatic turns: the limit") {
				warned++
			}
		case <-deadline:
			done = true
		}
	}
	if warned != 1 {
		t.Fatalf("limit warnings = %d, want one per session", warned)
	}
}

func TestAutomaticTurnCounterResetsWithTheSession(t *testing.T) {
	f := newFixture(t, text("first auto reply"), text("after clear"))
	f.ws.opts.Config.Work.MaxAutoTurns = 1
	h := wireTriggers(f)
	_ = f.ws.SelectAgent("reader")
	_ = f.ws.Chat()

	deliverFromWriter(t, f, "one", "first")
	awaitString(t, h.answers, "first triggered answer")

	f.ws.convFor("default/reader").Clear()

	deliverFromWriter(t, f, "two", "second")
	if got := awaitString(t, h.answers, "answer in the new session"); got != "after clear" {
		t.Fatalf("answer in the new session = %q", got)
	}
}
