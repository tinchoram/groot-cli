package workspace

import (
	"context"
	"testing"
)

// TestEverySendCallsTheDeliveryHook covers the badge refresh: each delivered
// message tells the UI, whoever sent it.
func TestEverySendCallsTheDeliveryHook(t *testing.T) {
	f := newFixture(t, text("ok"))
	addAgentToDefault(t, f, "writer")
	calls := 0
	f.ws.opts.Hooks.OnInbox = func() { calls++ }
	if _, _, err := f.ws.messengerFor("default", "writer").Send(context.Background(), "reader", "s", "b"); err != nil {
		t.Fatal(err)
	}
	if calls != 1 {
		t.Fatalf("delivery hook calls = %d, want 1", calls)
	}
}
