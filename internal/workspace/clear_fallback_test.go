package workspace

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/tinchoram/groot-cli/internal/provider"
)

// When the model summary of a cleared session fails (a timeout, most often),
// the session still gets a local summary and the caller is told it was a
// fallback, not an error.
func TestClearKeepsALocalSummaryWhenTheModelSummaryFails(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t, text("one"), provider.Step{Err: context.DeadlineExceeded}, text("two"))
	_ = f.ws.SelectAgent("reader")
	conv := f.ws.Chat()
	if _, err := conv.Send(ctx, "first question"); err != nil {
		t.Fatal(err)
	}
	oldID := conv.SessionID()

	err := conv.Clear()(ctx)
	if !errors.Is(err, ErrSummaryFallback) {
		t.Fatalf("clear must report the fallback, got %v", err)
	}
	if !strings.Contains(err.Error(), "reader") || !strings.Contains(err.Error(), "local summary") {
		t.Fatalf("fallback message = %q", err.Error())
	}

	st, _ := f.ws.storesFor(f.dir)
	entries, err := st.mem.Recall(ctx, "first question", "reader", 10)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, e := range entries {
		if e.Source == "session:"+oldID && strings.Contains(e.Text, "Last request: first question") {
			found = true
		}
	}
	if !found {
		t.Fatalf("local summary of %s missing, entries = %+v", oldID, entries)
	}
	latest, ok, _ := st.mem.LatestSession(ctx, "reader")
	if !ok || latest.ID != oldID || !latest.Cleared {
		t.Fatalf("the old session must still be ended and cleared: %+v", latest)
	}
}
