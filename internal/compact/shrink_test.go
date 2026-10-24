package compact

import (
	"context"
	"testing"

	"github.com/tinchoram/groot-cli/internal/api"
)

// alternating returns n messages: user, assistant, user, ...
func alternating(n int) []api.Message {
	var msgs []api.Message
	for i := 0; i < n; i++ {
		if i%2 == 0 {
			msgs = append(msgs, user("question"))
		} else {
			msgs = append(msgs, assistant("answer"))
		}
	}
	return msgs
}

func TestSummarizeHalveCompactsBelowTheThreshold(t *testing.T) {
	// The message-count threshold is far away: Halve must still compact.
	msgs := alternating(12)
	fp := &fakeProvider{reply: "SUMMARY"}
	out, err := Summarize{Provider: fp, Threshold: 1000}.CompactHalf(context.Background(), msgs)
	if err != nil {
		t.Fatal(err)
	}
	if fp.calls != 1 {
		t.Fatalf("summarizer called %d times, want 1", fp.calls)
	}
	// Roughly half of the history is kept verbatim, plus the two summary messages.
	if len(out) > 2+len(msgs)/2+1 || len(out) >= len(msgs) {
		t.Fatalf("history after halving = %d messages, from %d", len(out), len(msgs))
	}
}

func TestShrinkUsesHalvingForSummarize(t *testing.T) {
	msgs := alternating(12)
	fp := &fakeProvider{reply: "SUMMARY"}
	out, err := Shrink(context.Background(), Summarize{Provider: fp, Threshold: 1000}, msgs)
	if err != nil {
		t.Fatal(err)
	}
	if len(out) >= len(msgs) {
		t.Fatalf("Shrink did not shorten the history: %d -> %d", len(msgs), len(out))
	}
}

func TestShrinkLeavesOtherStrategiesToTheirOwnRule(t *testing.T) {
	msgs := alternating(12)
	out, err := Shrink(context.Background(), SlidingWindow{Keep: 4}, msgs)
	if err != nil {
		t.Fatal(err)
	}
	if len(out) > 4 || len(out) == 0 {
		t.Fatalf("window under pressure = %d messages, want the last 4 at most", len(out))
	}
	same, _ := Shrink(context.Background(), None{}, msgs)
	if len(same) != len(msgs) {
		t.Fatalf("None under pressure changed the history: %d -> %d", len(msgs), len(same))
	}
}
