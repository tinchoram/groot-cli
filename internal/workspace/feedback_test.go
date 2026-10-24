package workspace

import (
	"context"
	"github.com/tinchoram/groot-cli/internal/inbox"
	"strings"
	"testing"
)

// TestFeedbackIsSentToDebugAndNothingIsWritten covers the rule that debug is the
// only writer of the backlog: /bug and /feat become messages with the context
// and the version in the body.
func TestFeedbackIsSentToDebugAndNothingIsWritten(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t, text("ok"))
	f.ws.opts.Version = "0.3.0-dev"
	if err := f.spaces.Add("root", t.TempDir(), t0); err != nil {
		t.Fatal(err)
	}
	if err := f.spaces.Assign("root", "debug"); err != nil {
		t.Fatal(err)
	}
	id, recipient, err := f.ws.SendFeedback("default", "reader", "bug", "the approval hides the path",
		[]string{"earlier question", "second line"})
	if err != nil {
		t.Fatal(err)
	}
	if recipient != "root/debug" || id == 0 {
		t.Fatalf("sent to %q as #%d", recipient, id)
	}
	msgs, _ := f.ws.inbox.Unread(ctx, "root", "debug")
	if len(msgs) != 1 {
		t.Fatalf("debug has %d messages", len(msgs))
	}
	m := msgs[0]
	if m.Subject != "bug: the approval hides the path" || m.FromSpace != "default" || m.FromAgent != "reader" {
		t.Fatalf("message = %+v", m)
	}
	for _, want := range []string{"## Context", "earlier question", "version: 0.3.0-dev", "space: default", "agent: reader"} {
		if !strings.Contains(m.Body, want) {
			t.Fatalf("body misses %q:\n%s", want, m.Body)
		}
	}
	items, _ := f.ws.Backlog("")
	if len(items) != 0 {
		t.Fatalf("the backlog must not be written by a report: %d items", len(items))
	}
}

func TestFeedbackBodyIsCappedInsteadOfFailing(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t, text("ok"))
	if err := f.spaces.Add("root", t.TempDir(), t0); err != nil {
		t.Fatal(err)
	}
	if err := f.spaces.Assign("root", "debug"); err != nil {
		t.Fatal(err)
	}
	huge := strings.Repeat("x", inbox.MaxBodyBytes*2)
	id, _, err := f.ws.SendFeedback("default", "reader", "feat", huge, []string{strings.Repeat("y", 5000)})
	if err != nil {
		t.Fatalf("oversized report must be truncated, not refused: %v", err)
	}
	msgs, _ := f.ws.inbox.Unread(ctx, "root", "debug")
	var body string
	for _, m := range msgs {
		if m.ID == id {
			body = m.Body
		}
	}
	if len(body) > inbox.MaxBodyBytes || !strings.Contains(body, "[truncated") || !strings.Contains(body, "version:") {
		t.Fatalf("body len=%d, must be capped, marked truncated and keep the footer", len(body))
	}
}
