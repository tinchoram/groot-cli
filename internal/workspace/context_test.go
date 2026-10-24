package workspace

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/tinchoram/groot-cli/internal/api"
	"github.com/tinchoram/groot-cli/internal/memory"
)

func TestContextLevelBands(t *testing.T) {
	const window = 200_000
	cases := []struct {
		name   string
		tokens int64
		window int
		want   ContextLevel
	}{
		{"nothing measured yet", 0, window, ContextNone},
		{"no window known", 50_000, 0, ContextNone},
		{"well under 70 percent", 84_000, window, ContextOK},
		{"69 percent is still ok", 138_000, window, ContextOK},
		{"warning band starts at 70 percent", 140_000, window, ContextWarn},
		{"89 percent is still warning", 178_000, window, ContextWarn},
		{"red band starts at 90 percent", 180_000, window, ContextFull},
		{"over the window is red", 250_000, window, ContextFull},
	}
	for _, c := range cases {
		if got := ContextLevelOf(c.tokens, c.window); got != c.want {
			t.Errorf("%s: level = %v, want %v", c.name, got, c.want)
		}
	}
}

// seedContext stores a session of agent whose last answer read tokens of context.
func seedContext(t *testing.T, f *fixture, agent string, tokens int64) {
	t.Helper()
	ctx := context.Background()
	f.ws.mu.Lock()
	st, err := f.ws.storesFor(f.dir)
	f.ws.mu.Unlock()
	if err != nil {
		t.Fatal(err)
	}
	id := "ctx-" + agent
	if err := st.mem.StartSession(ctx, memory.Session{ID: id, Agent: agent, Model: "mock-model", StartedAt: time.Now().UTC()}); err != nil {
		t.Fatal(err)
	}
	if err := st.mem.SetContextTokens(ctx, id, tokens); err != nil {
		t.Fatal(err)
	}
}

func TestContextsReadTheLastSessionOfEachAgent(t *testing.T) {
	f := newFixture(t, text("ok"))
	seedContext(t, f, "reader", 84_000)

	rows, err := f.ws.Contexts(context.Background(), false)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 {
		t.Fatalf("rows = %+v, want the one agent of the active space", rows)
	}
	r := rows[0]
	if r.Agent != "reader" || r.Space != "default" || r.Tokens != 84_000 || r.Window != 200_000 || r.Percent != 42 {
		t.Fatalf("row = %+v, want reader 84000 of 200000 (42%%)", r)
	}
	if r.Strategy != "none" || r.Triggers {
		t.Fatalf("default strategy must not trigger: %+v", r)
	}
}

func TestContextWindowOverridePersistsAndApplies(t *testing.T) {
	f := newFixture(t, text("ok"))
	path := filepath.Join(t.TempDir(), "groot.yaml")
	f.ws.opts.ConfigPath = path
	seedContext(t, f, "reader", 84_000)

	if err := f.ws.SetContextWindow("mock-model", 100_000); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(path)
	if err != nil || !strings.Contains(string(raw), "mock-model") {
		t.Fatalf("groot.yaml must hold the override: err=%v\n%s", err, raw)
	}
	rows, err := f.ws.Contexts(context.Background(), false)
	if err != nil {
		t.Fatal(err)
	}
	if rows[0].Window != 100_000 || rows[0].Percent != 84 {
		t.Fatalf("row after override = %+v, want window 100000 and 84%%", rows[0])
	}
	if err := f.ws.SetContextWindow("mock-model", 0); err == nil {
		t.Fatal("a zero window must be refused")
	}
	if err := f.ws.SetContextWindow("", 5); err == nil {
		t.Fatal("an empty model must be refused")
	}
}

func TestContextRowTriggersOnlyWithAStrategyAboveThePercentage(t *testing.T) {
	f := newFixture(t, text("ok"))
	seedContext(t, f, "reader", 170_000) // 85 percent
	f.ws.mu.Lock()
	f.ws.opts.Config.Compaction.Strategy = "summarize"
	f.ws.opts.Config.Compaction.Threshold = 60
	f.ws.opts.Config.Compaction.MaxContextPercent = 80
	f.ws.mu.Unlock()

	rows, err := f.ws.Contexts(context.Background(), false)
	if err != nil {
		t.Fatal(err)
	}
	if !rows[0].Triggers || rows[0].Strategy != "summarize" {
		t.Fatalf("row = %+v, want the token trigger to fire at 85 percent", rows[0])
	}

	f.ws.mu.Lock()
	f.ws.opts.Config.Compaction.MaxContextPercent = 90
	f.ws.mu.Unlock()
	rows, _ = f.ws.Contexts(context.Background(), false)
	if rows[0].Triggers {
		t.Fatalf("row = %+v, the trigger must stay off below its percentage", rows[0])
	}
}

func TestFormatContextsShowsEveryColumn(t *testing.T) {
	out := FormatContexts([]ContextRow{
		{Space: "default", Agent: "reader", Model: "claude-sonnet-5-5", Window: 200_000, Tokens: 84_000, Percent: 42, Strategy: "none"},
	})
	for _, want := range []string{"SPACE", "reader", "claude-sonnet-5-5", "200k", "84k", "42%", "none", "no"} {
		if !strings.Contains(out, want) {
			t.Fatalf("table lacks %q:\n%s", want, out)
		}
	}
	if empty := FormatContexts(nil); !strings.Contains(empty, "no agents") {
		t.Fatalf("empty table = %q", empty)
	}
}

func TestResumedSessionShowsItsStoredContext(t *testing.T) {
	f := newFixture(t, text("unused"))
	ctx := context.Background()
	f.ws.mu.Lock()
	st, err := f.ws.storesFor(f.dir)
	f.ws.mu.Unlock()
	if err != nil {
		t.Fatal(err)
	}
	if err := st.mem.StartSession(ctx, memory.Session{ID: "resume-1", Agent: "reader", Model: "mock-model", StartedAt: time.Now().UTC()}); err != nil {
		t.Fatal(err)
	}
	if err := st.mem.AppendMessage(ctx, "resume-1", 1, api.Message{Role: api.RoleUser,
		Content: []api.Block{{Type: api.BlockText, Text: "earlier"}}}, api.Usage{}); err != nil {
		t.Fatal(err)
	}
	if err := st.mem.SetContextTokens(ctx, "resume-1", 61_000); err != nil {
		t.Fatal(err)
	}

	if err := f.ws.SelectAgent("reader"); err != nil {
		t.Fatal(err)
	}
	chat := f.ws.Chat()
	if _, ok := chat.Resumed(); !ok {
		t.Fatal("the stored session should be resumed")
	}
	if got := chat.ContextTokens(); got != 61_000 {
		t.Fatalf("resumed ContextTokens = %d, want 61000 before any new turn", got)
	}
}
