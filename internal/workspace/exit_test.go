package workspace

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/tinchoram/groot-cli/internal/api"
	"github.com/tinchoram/groot-cli/internal/catalog"
	"github.com/tinchoram/groot-cli/internal/memory"
	"github.com/tinchoram/groot-cli/internal/provider"
)

// slowSummary answers the turns from the mock, but a session summary never
// returns until its context ends, as a slow model would behave.
type slowSummary struct {
	*provider.Mock
}

func (s slowSummary) Send(ctx context.Context, req provider.Request) (api.Response, error) {
	if req.System == summarySystem {
		<-ctx.Done()
		return api.Response{}, ctx.Err()
	}
	return s.Mock.Send(ctx, req)
}

// storedSummaries reads the session summaries of the space database after the
// workspace has closed it.
func storedSummaries(t *testing.T, dir, agent string) string {
	t.Helper()
	st, err := memory.OpenSQLite(filepath.Join(dir, ".groot", "groot.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	out, err := st.Preamble(context.Background(), agent, 10)
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func TestFastCloseMakesNoModelCallAndWritesTheLocalSummary(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t, text("the answer to the question"))
	_ = f.ws.SelectAgent("reader")
	conv := f.ws.Chat()
	if _, err := conv.Send(ctx, "what is the question"); err != nil {
		t.Fatal(err)
	}
	calls := len(f.mock.Calls)

	if err := f.ws.CloseFast(); err != nil {
		t.Fatal(err)
	}
	if len(f.mock.Calls) != calls {
		t.Fatalf("a fast close called the model %d times", len(f.mock.Calls)-calls)
	}
	got := storedSummaries(t, f.dir, "reader")
	if !strings.Contains(got, "Last request: what is the question") || !strings.Contains(got, "Last answer: the answer to the question") {
		t.Fatalf("local summary missing:\n%s", got)
	}
}

func TestLocalSummaryCapsTheAnswerAtThreeHundredCharacters(t *testing.T) {
	ctx := context.Background()
	long := strings.Repeat("x", 500)
	f := newFixture(t, text(long))
	_ = f.ws.SelectAgent("reader")
	conv := f.ws.Chat()
	if _, err := conv.Send(ctx, "q"); err != nil {
		t.Fatal(err)
	}
	summary := conv.(*Conversation).localSummary()
	answer := summary[strings.Index(summary, "Last answer: ")+len("Last answer: "):]
	if n := len([]rune(answer)); n > localSummaryRunes+1 {
		t.Fatalf("answer kept %d characters, want at most %d", n, localSummaryRunes)
	}
}

func TestGracefulCloseFallsBackToTheLocalSummaryWhenTheModelIsSlow(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t, text("an answer"))
	f.ws.opts.NewProvider = func(string) provider.Provider { return slowSummary{f.mock} }
	_ = f.ws.SelectAgent("reader")
	conv := f.ws.Chat()
	if _, err := conv.Send(ctx, "slow question"); err != nil {
		t.Fatal(err)
	}

	budget, cancel := context.WithTimeout(ctx, 150*time.Millisecond)
	defer cancel()
	start := time.Now()
	if err := f.ws.Close(budget); err != nil {
		t.Fatalf("close: %v", err)
	}
	if took := time.Since(start); took > time.Second {
		t.Fatalf("close took %v; the budget was 150ms", took)
	}
	if got := storedSummaries(t, f.dir, "reader"); !strings.Contains(got, "Last request: slow question") {
		t.Fatalf("the fallback summary is missing:\n%s", got)
	}
}

func TestGracefulCloseSummarizesConversationsInParallel(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t, text("first"), text("second"))
	if err := catalog.Save(f.home, catalog.AgentDef{Name: "writer", Description: "writes", Tools: []string{"read_file"}}, "You write."); err != nil {
		t.Fatal(err)
	}
	if err := f.ws.reload(); err != nil {
		t.Fatal(err)
	}
	if err := f.spaces.Assign("default", "writer"); err != nil {
		t.Fatal(err)
	}
	f.ws.opts.NewProvider = func(string) provider.Provider { return slowSummary{f.mock} }
	for _, agent := range []string{"reader", "writer"} {
		if err := f.ws.SelectAgent(agent); err != nil {
			t.Fatal(err)
		}
		if _, err := f.ws.Chat().Send(ctx, "hello from "+agent); err != nil {
			t.Fatal(err)
		}
	}

	budget, cancel := context.WithTimeout(ctx, 300*time.Millisecond)
	defer cancel()
	start := time.Now()
	if err := f.ws.Close(budget); err != nil {
		t.Fatalf("close: %v", err)
	}
	// Two summaries that each wait for the budget take one budget when run in
	// parallel, and two when run one after the other.
	if took := time.Since(start); took > 550*time.Millisecond {
		t.Fatalf("summaries ran one after the other: close took %v", took)
	}
}
