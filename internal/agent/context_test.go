package agent

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/tinchoram/groot-cli/internal/api"
	"github.com/tinchoram/groot-cli/internal/compact"
	"github.com/tinchoram/groot-cli/internal/provider"
)

// sizedStep answers with a context of input prompt tokens, cached or not.
func sizedStep(text string, input int64) provider.Step {
	s := replyStep(text)
	s.Response.Usage = api.Usage{InputTokens: input}
	return s
}

// overflowStep fails the way the Anthropic API does for a prompt over the window.
func overflowStep() provider.Step {
	return provider.Step{Err: &provider.Error{
		Kind:    provider.KindStatus,
		Status:  400,
		Message: `400 Bad Request {"type":"error","error":{"type":"invalid_request_error","message":"prompt is too long: 201000 tokens > 200000 maximum"}}`,
	}}
}

// seededHistory is n alternating user and assistant text messages.
func seededHistory(n int) []api.Message {
	var out []api.Message
	for i := 0; i < n; i++ {
		if i%2 == 0 {
			out = append(out, api.Message{Role: api.RoleUser, Content: []api.Block{{Type: api.BlockText, Text: "q"}}})
		} else {
			out = append(out, api.Message{Role: api.RoleAssistant, Content: []api.Block{{Type: api.BlockText, Text: "a"}}})
		}
	}
	return out
}

// collector gathers the warnings and notices an agent emits.
type collector struct{ msgs []string }

func (c *collector) warn(msg string) { c.msgs = append(c.msgs, msg) }

func (c *collector) containing(sub string) int {
	n := 0
	for _, m := range c.msgs {
		if strings.Contains(m, sub) {
			n++
		}
	}
	return n
}

func TestAnswerSetsTheContextToTheInputPlusCacheTokens(t *testing.T) {
	ctx := context.Background()
	store := openStore(t)
	step := replyStep("one")
	step.Response.Usage = api.Usage{InputTokens: 1000, CacheCreationTokens: 200, CacheReadTokens: 300, OutputTokens: 50}
	ag := &Agent{Name: "reader", Provider: provider.NewMock(step), Memory: store}
	if _, err := ag.Send(ctx, "hi"); err != nil {
		t.Fatal(err)
	}
	if got := ag.ContextTokens(); got != 1500 {
		t.Fatalf("ContextTokens = %d, want 1500 (input + cache write + cache read, not output)", got)
	}
	sess, ok, err := store.LatestSession(ctx, "reader")
	if err != nil || !ok || sess.ContextTokens != 1500 {
		t.Fatalf("stored context = %d ok=%v err=%v, want 1500", sess.ContextTokens, ok, err)
	}
}

func TestContextWindowFollowsTheModel(t *testing.T) {
	mock := provider.NewMock()
	mock.SetModel("claude-haiku-5-5")
	ag := &Agent{Name: "reader", Provider: mock, WindowFor: func(model string) int {
		if model == "claude-haiku-5-5" {
			return 1000
		}
		return 0
	}}
	if got := ag.ContextWindow(); got != 1000 {
		t.Fatalf("ContextWindow = %d, want 1000 from WindowFor", got)
	}
	bare := &Agent{Name: "reader", Provider: mock}
	if got := bare.ContextWindow(); got != provider.DefaultContextWindow {
		t.Fatalf("ContextWindow without WindowFor = %d, want %d", got, provider.DefaultContextWindow)
	}
}

func TestRestoredContextIsShownBeforeTheNextTurn(t *testing.T) {
	ag := &Agent{Name: "reader", Provider: provider.NewMock()}
	ag.RestoreContext(1234)
	if got := ag.ContextTokens(); got != 1234 {
		t.Fatalf("ContextTokens after RestoreContext = %d, want 1234", got)
	}
}

func TestTokenTriggerCompactsBeforeTheCall(t *testing.T) {
	ctx := context.Background()
	summarizer := provider.NewMock(replyStep("SUMMARY"))
	mock := provider.NewMock(sizedStep("next", 100))
	ag := &Agent{
		Name:              "reader",
		Provider:          mock,
		Memory:            openStore(t),
		Compactor:         compact.Summarize{Provider: summarizer, Threshold: 1_000},
		ContextMaxPercent: 80,
		WindowFor:         func(string) int { return 10_000 },
	}
	ag.Resume("s-1", seededHistory(6), 1, 0)
	ag.RestoreContext(9_000) // 90% of the window, over the 80% trigger

	if _, err := ag.Send(ctx, "next"); err != nil {
		t.Fatal(err)
	}
	// The message-count threshold (1000) is far away, so only the token trigger
	// can have summarized the seven messages before the call.
	if len(summarizer.Calls) != 1 {
		t.Fatalf("summarizer called %d times, want 1 from the token trigger", len(summarizer.Calls))
	}
	if got := len(mock.Calls[0].Messages); got >= 7 {
		t.Fatalf("model saw %d messages, want fewer than 7 after the token-triggered compaction", got)
	}
}

func TestTokenTriggerStaysOffBelowThePercentage(t *testing.T) {
	ctx := context.Background()
	mock := provider.NewMock(sizedStep("next", 100))
	ag := &Agent{
		Name:              "reader",
		Provider:          mock,
		Memory:            openStore(t),
		Compactor:         compact.Summarize{Provider: provider.NewMock(), Threshold: 1_000},
		ContextMaxPercent: 80,
		WindowFor:         func(string) int { return 10_000 },
	}
	ag.Resume("s-1", seededHistory(6), 1, 0)
	ag.RestoreContext(1_000) // 10%
	if _, err := ag.Send(ctx, "next"); err != nil {
		t.Fatal(err)
	}
	if got := len(mock.Calls[0].Messages); got != 7 {
		t.Fatalf("model saw %d messages, want all 7 below the trigger", got)
	}
}

func TestTokenTriggerDisabledWithZeroPercent(t *testing.T) {
	ctx := context.Background()
	mock := provider.NewMock(sizedStep("next", 100))
	ag := &Agent{
		Name:              "reader",
		Provider:          mock,
		Memory:            openStore(t),
		Compactor:         compact.Summarize{Provider: provider.NewMock(), Threshold: 1_000},
		ContextMaxPercent: 0,
		WindowFor:         func(string) int { return 10_000 },
	}
	ag.Resume("s-1", seededHistory(6), 1, 0)
	ag.RestoreContext(9_900)
	if _, err := ag.Send(ctx, "next"); err != nil {
		t.Fatal(err)
	}
	if got := len(mock.Calls[0].Messages); got != 7 {
		t.Fatalf("model saw %d messages with the trigger off, want 7", got)
	}
}

func TestSummarizeUnderPressureCompactsWithoutTheThreshold(t *testing.T) {
	ctx := context.Background()
	summarizer := provider.NewMock(replyStep("SUMMARY"))
	mock := provider.NewMock(sizedStep("next", 100))
	ag := &Agent{
		Name:              "reader",
		Provider:          mock,
		Memory:            openStore(t),
		Compactor:         compact.Summarize{Provider: summarizer, Threshold: 1_000},
		ContextMaxPercent: 80,
		WindowFor:         func(string) int { return 10_000 },
	}
	ag.Resume("s-1", seededHistory(8), 1, 0)
	ag.RestoreContext(9_000)
	if _, err := ag.Send(ctx, "next"); err != nil {
		t.Fatal(err)
	}
	if len(summarizer.Calls) != 1 {
		t.Fatalf("summarizer called %d times, want 1", len(summarizer.Calls))
	}
	if got := len(mock.Calls[0].Messages); got >= 9 {
		t.Fatalf("model saw %d messages, want fewer than 9 after summarizing", got)
	}
}

func TestStrategyNoneWarnsOnceAboveNinetyPercent(t *testing.T) {
	ctx := context.Background()
	col := &collector{}
	mock := provider.NewMock(sizedStep("one", 100), sizedStep("two", 100))
	ag := &Agent{
		Name:              "reader",
		Provider:          mock,
		Memory:            openStore(t),
		ContextMaxPercent: 80,
		WindowFor:         func(string) int { return 10_000 },
		OnWarning:         col.warn,
	}
	ag.RestoreContext(9_300)
	for _, p := range []string{"a", "b"} {
		if _, err := ag.Send(ctx, p); err != nil {
			t.Fatal(err)
		}
	}
	if n := col.containing("set compaction.strategy to window or summarize"); n != 1 {
		t.Fatalf("the 90%% warning appeared %d times, want once", n)
	}
	if got := len(mock.Calls[1].Messages); got != 3 {
		t.Fatalf("strategy none must not drop history: second call saw %d messages, want 3", got)
	}
}

func TestOverflowCompactsOnceAndRetries(t *testing.T) {
	ctx := context.Background()
	col := &collector{}
	mock := provider.NewMock(overflowStep(), replyStep("recovered"))
	ag := &Agent{
		Name:          "reader",
		Provider:      mock,
		Memory:        openStore(t),
		EmergencyKeep: 2,
		OnWarning:     col.warn,
	}
	ag.Resume("s-1", seededHistory(8), 1, 0)
	text, err := ag.Send(ctx, "next")
	if err != nil {
		t.Fatalf("Send after an overflow: %v", err)
	}
	if text != "recovered" {
		t.Fatalf("answer = %q, want the retry's answer", text)
	}
	if len(mock.Calls) != 2 {
		t.Fatalf("model called %d times, want the failed call and one retry", len(mock.Calls))
	}
	if len(mock.Calls[1].Messages) >= len(mock.Calls[0].Messages) {
		t.Fatalf("the retry was not compacted: %d then %d messages", len(mock.Calls[0].Messages), len(mock.Calls[1].Messages))
	}
	if n := col.containing("context overflow: compacted history and retried"); n != 1 {
		t.Fatalf("overflow notice appeared %d times, want once", n)
	}
}

func TestSecondOverflowSurfacesTheError(t *testing.T) {
	ctx := context.Background()
	mock := provider.NewMock(overflowStep(), overflowStep(), replyStep("never"))
	ag := &Agent{Name: "reader", Provider: mock, Memory: openStore(t), EmergencyKeep: 2}
	ag.Resume("s-1", seededHistory(8), 1, 0)
	_, err := ag.Send(ctx, "next")
	if !provider.IsContextOverflow(err) {
		t.Fatalf("Send error = %v, want the overflow error after one retry", err)
	}
	if len(mock.Calls) != 2 {
		t.Fatalf("model called %d times, want 2 (no second retry)", len(mock.Calls))
	}
}

func TestOverflowWithNothingToCompactSurfacesTheError(t *testing.T) {
	ctx := context.Background()
	mock := provider.NewMock(overflowStep(), replyStep("never"))
	ag := &Agent{Name: "reader", Provider: mock, Memory: openStore(t)}
	_, err := ag.Send(ctx, "tiny")
	if !provider.IsContextOverflow(err) {
		t.Fatalf("Send error = %v, want the overflow error", err)
	}
	if len(mock.Calls) != 1 {
		t.Fatalf("model called %d times, want 1 when no retry is possible", len(mock.Calls))
	}
	if errors.Is(err, context.Canceled) {
		t.Fatal("unexpected cancellation")
	}
}
