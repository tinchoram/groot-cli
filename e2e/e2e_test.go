//go:build e2e

// Package e2e runs groot's workspace layer against the real Anthropic API. It is
// opt-in twice over: the e2e build tag selects it (make e2e), and each test skips
// unless ANTHROPIC_API_KEY is set in the environment. Nothing reads a .env file.
//
// Every scenario gets a fresh GROOT_HOME and a fresh space directory. The model
// is the cheapest priced one, prompts are tiny and max_tokens is low, so a run
// costs a few hundredths of a cent.
package e2e

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/tinchoram/groot-cli/internal/api"
	"github.com/tinchoram/groot-cli/internal/catalog"
	"github.com/tinchoram/groot-cli/internal/config"
	"github.com/tinchoram/groot-cli/internal/gate"
	"github.com/tinchoram/groot-cli/internal/memory"
	"github.com/tinchoram/groot-cli/internal/provider"
	"github.com/tinchoram/groot-cli/internal/space"
	"github.com/tinchoram/groot-cli/internal/workspace"
)

const (
	// model is claude-haiku-5-5, the cheapest entry of provider/pricing.go
	// ($0.10 input and $0.50 output per million tokens).
	model     = "claude-haiku-5-5"
	maxTokens = 256
	agentName = "reader"
	spaceName = "e2e"
	// costWarning is the spend a run should stay under; costRunaway fails the run.
	costWarning = 0.05
	costRunaway = 0.50
	turnTimeout = 2 * time.Minute
)

// spentUSD is the spend of the scenarios run so far in this process. The tests
// run one after another, so it needs no lock. TestTotalSpend reports it.
var spentUSD float64

// apiKey returns the key from the environment, or skips the test.
func apiKey(t *testing.T) string {
	t.Helper()
	key := os.Getenv("ANTHROPIC_API_KEY")
	if key == "" {
		t.Skip("ANTHROPIC_API_KEY is not set: skipping the end-to-end test (it calls the real API)")
	}
	return key
}

// env is one scenario: a fresh home, a space directory and a call counter.
type env struct {
	home, dir string
	key       string
	calls     *atomic.Int64
}

// newEnv writes an agent that may use tools and a space that holds it.
func newEnv(t *testing.T, key string, tools []string) *env {
	t.Helper()
	e := &env{home: t.TempDir(), dir: t.TempDir(), key: key, calls: &atomic.Int64{}}
	def := catalog.AgentDef{Name: agentName, Description: "answers briefly", Model: model, Tools: tools}
	if err := catalog.Save(e.home, def, "You are a terse assistant. Follow the user's instructions exactly."); err != nil {
		t.Fatalf("save agent: %v", err)
	}
	sp, err := space.Open(filepath.Join(e.home, "spaces.yaml"))
	if err != nil {
		t.Fatalf("open spaces: %v", err)
	}
	for _, step := range []error{
		sp.Add(spaceName, e.dir, time.Now()),
		sp.Assign(spaceName, agentName),
		sp.Use(spaceName),
	} {
		if step != nil {
			t.Fatalf("set up space: %v", step)
		}
	}
	return e
}

// countingProvider counts the model calls that reach the real adapter.
type countingProvider struct {
	*provider.Anthropic
	calls *atomic.Int64
}

func (p countingProvider) Send(ctx context.Context, req provider.Request) (api.Response, error) {
	p.calls.Add(1)
	return p.Anthropic.Send(ctx, req)
}

// open starts a workspace over the home, as groot does at start. A second call
// on the same home is a second instance that can resume the stored sessions.
func (e *env) open(t *testing.T) *workspace.Workspace {
	t.Helper()
	sp, err := space.Open(filepath.Join(e.home, "spaces.yaml"))
	if err != nil {
		t.Fatalf("open spaces: %v", err)
	}
	cfg := config.Default()
	cfg.Provider.Model = model
	ws, err := workspace.New(workspace.Options{
		Home:   e.home,
		Config: cfg,
		Spaces: sp,
		Gate:   gate.AutoApprove{},
		NewProvider: func(m string) provider.Provider {
			return countingProvider{
				Anthropic: provider.NewAnthropic(provider.AnthropicOptions{APIKey: e.key, Model: m, MaxTokens: maxTokens}),
				calls:     e.calls,
			}
		},
	})
	if err != nil {
		t.Fatalf("workspace: %v", err)
	}
	// CloseFast ends conversations with a local summary: no extra model call.
	t.Cleanup(func() { _ = ws.CloseFast() })
	return ws
}

// store opens the space database read-only in spirit: it is only read here.
func (e *env) store(t *testing.T) *memory.SQLite {
	t.Helper()
	st, err := memory.OpenSQLite(filepath.Join(e.dir, ".groot", "groot.db"))
	if err != nil {
		t.Fatalf("open space database: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })
	return st
}

// tally adds the stored spend of this scenario to the run total.
func (e *env) tally(t *testing.T) float64 {
	t.Helper()
	by, err := e.store(t).SpendByAgent(context.Background())
	if err != nil {
		t.Fatalf("read spend: %v", err)
	}
	var total float64
	for _, v := range by {
		total += v
	}
	spentUSD += total
	return total
}

// send runs one turn with a bounded context.
func send(chat workspace.Chat, prompt string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), turnTimeout)
	defer cancel()
	return chat.Send(ctx, prompt)
}

func TestTextTurnRecordsUsageCostAndContext(t *testing.T) {
	e := newEnv(t, apiKey(t), nil)
	ws := e.open(t)
	chat := ws.Chat()

	reply, err := send(chat, "Reply with the single word: pong")
	if err != nil {
		t.Fatalf("turn: %v", err)
	}
	if strings.TrimSpace(reply) == "" {
		t.Fatal("the model returned an empty reply")
	}
	if u := chat.Usage(); u.InputTokens == 0 || u.OutputTokens == 0 {
		t.Fatalf("usage not recorded: %+v", u)
	}
	if chat.CostUSD() <= 0 {
		t.Fatalf("conversation cost = %v, want more than zero", chat.CostUSD())
	}
	if chat.ContextTokens() <= 0 {
		t.Fatalf("context tokens = %d, want more than zero", chat.ContextTokens())
	}

	sess, ok, err := e.store(t).LatestSession(context.Background(), agentName)
	if err != nil || !ok {
		t.Fatalf("latest session: ok=%v err=%v", ok, err)
	}
	if sess.CostUSD <= 0 || sess.ContextTokens <= 0 {
		t.Fatalf("session row = cost %v, context %d; want both above zero", sess.CostUSD, sess.ContextTokens)
	}
	t.Logf("text turn: reply %q, cost $%.6f, context %d tokens", strings.TrimSpace(reply), sess.CostUSD, sess.ContextTokens)
	e.tally(t)
}

func TestReadFileToolReturnsTheSpaceFile(t *testing.T) {
	const token = "groot-e2e-7f3a91"
	e := newEnv(t, apiKey(t), []string{"read_file"})
	if err := os.WriteFile(filepath.Join(e.dir, "fixture.txt"), []byte(token+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	ws := e.open(t)

	reply, err := send(ws.Chat(), "Use the read_file tool to read fixture.txt, then reply with its exact contents and nothing else.")
	if err != nil {
		t.Fatalf("turn: %v", err)
	}
	if !strings.Contains(reply, token) {
		t.Fatalf("reply %q does not carry the file content; the tool did not return it", reply)
	}
	audit, err := os.ReadFile(filepath.Join(e.dir, ".groot", "audit.jsonl"))
	if err != nil || !strings.Contains(string(audit), "read_file") {
		t.Fatalf("audit log must record the read_file call: err=%v\n%s", err, audit)
	}
	// One model call asks for the tool, one answers with the content.
	if n := e.calls.Load(); n < 2 {
		t.Fatalf("model calls = %d, want at least 2 for a tool round trip", n)
	}
	t.Logf("read_file turn: %d model calls", e.calls.Load())
	e.tally(t)
}

func TestResumeRestoresContextBeforeAnyCall(t *testing.T) {
	e := newEnv(t, apiKey(t), nil)
	first := e.open(t)
	chat1 := first.Chat()
	if _, err := send(chat1, "Reply with the single word: pong"); err != nil {
		t.Fatalf("first turn: %v", err)
	}
	stored := chat1.ContextTokens()
	if stored <= 0 {
		t.Fatalf("first turn stored no context: %d", stored)
	}

	before := e.calls.Load()
	second := e.open(t)
	chat2 := second.Chat()
	if _, ok := chat2.Resumed(); !ok {
		t.Fatal("the second instance did not resume the stored session")
	}
	if chat2.SessionID() != chat1.SessionID() {
		t.Fatalf("session = %s, want the resumed %s", chat2.SessionID(), chat1.SessionID())
	}
	if got := chat2.ContextTokens(); got != stored {
		t.Fatalf("restored context = %d, want %d before any call", got, stored)
	}
	if n := e.calls.Load(); n != before {
		t.Fatalf("resuming made %d model calls, want none", n-before)
	}
	e.tally(t)
}

func TestAgentBudgetRefusesATurnWithoutAModelCall(t *testing.T) {
	e := newEnv(t, apiKey(t), nil)
	ws := e.open(t)
	chat := ws.Chat()
	if _, err := send(chat, "Reply with the single word: pong"); err != nil {
		t.Fatalf("first turn: %v", err)
	}
	spent := chat.CostUSD()
	if spent <= 0 {
		t.Fatalf("first turn cost = %v, want more than zero", spent)
	}
	// The budget sits below what the first turn already spent.
	if err := ws.SetAgentBudget(agentName, spent/2); err != nil {
		t.Fatalf("set budget: %v", err)
	}

	before := e.calls.Load()
	_, err := send(chat, "Reply with the single word: ping")
	if err == nil || !strings.Contains(err.Error(), "budget") {
		t.Fatalf("second turn error = %v, want a budget refusal", err)
	}
	if n := e.calls.Load(); n != before {
		t.Fatalf("the refused turn reached the provider %d times", n-before)
	}
	if got := chat.CostUSD(); got != spent {
		t.Fatalf("cost changed from %v to %v on a refused turn", spent, got)
	}
	t.Logf("budget refusal: %v", err)
	e.tally(t)
}

// TestTotalSpend runs last (tests run in source order) and reports the spend of
// the whole run.
func TestTotalSpend(t *testing.T) {
	if spentUSD == 0 {
		t.Skip("no scenario ran, so there is no spend to report")
	}
	t.Logf("total spend of this e2e run: $%.6f", spentUSD)
	if spentUSD > costWarning {
		t.Logf("warning: the run spent more than the $%.2f target", costWarning)
	}
	if spentUSD > costRunaway {
		t.Errorf("the run spent $%.4f, above the $%.2f runaway guard", spentUSD, costRunaway)
	}
}
