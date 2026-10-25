package workspace

import (
	"context"
	"strings"
	"testing"
)

const debugNotInstalledHint = "the debug agent is not installed: ask groot to install it (install_agent debug), or run /agent install debug"

// When debug is assigned to no space, reports say so and point to groot, and
// nothing is written to any inbox.
func TestFeedbackWithoutDebugSaysHowToInstallIt(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t, text("ok"))

	_, _, err := f.ws.SendFeedback("default", "reader", "bug", "the approval hides the path", nil)
	if err == nil || err.Error() != debugNotInstalledHint {
		t.Fatalf("SendFeedback without debug: err = %v, want %q", err, debugNotInstalledHint)
	}
	counts, _ := f.ws.UnreadCounts(ctx)
	if len(counts) != 0 {
		t.Fatalf("a refused report must write no inbox row: %v", counts)
	}
}

// A bare "debug" address gets the same answer as a report.
func TestBareDebugAddressWithoutDebugSaysHowToInstallIt(t *testing.T) {
	f := newFixture(t, text("ok"))
	_, _, err := f.ws.messengerFor("default", "reader").Send(context.Background(), "debug", "bug: x", "body")
	if err == nil || !strings.Contains(err.Error(), debugNotInstalledHint) {
		t.Fatalf("send to debug without debug: err = %v, want the install hint", err)
	}
}
