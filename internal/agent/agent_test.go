package agent

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/tinchoram/groot-cli/internal/api"
	"github.com/tinchoram/groot-cli/internal/audit"
	"github.com/tinchoram/groot-cli/internal/gate"
	"github.com/tinchoram/groot-cli/internal/memory"
	"github.com/tinchoram/groot-cli/internal/provider"
	"github.com/tinchoram/groot-cli/internal/tool"
)

// echoTool is a configurable tool that counts executions.
type echoTool struct {
	risk  tool.Risk
	out   string
	isErr bool
	calls int
}

func (e *echoTool) Definition() api.ToolDef {
	return api.ToolDef{Name: "echo", Description: "echo", InputSchema: json.RawMessage(`{"type":"object"}`)}
}
func (e *echoTool) Risk() tool.Risk             { return e.risk }
func (e *echoTool) RiskFor(string) tool.Risk    { return e.risk }
func (e *echoTool) Preview(input string) string { return "echo " + input }
func (e *echoTool) Execute(context.Context, string) (string, bool) {
	e.calls++
	return e.out, e.isErr
}

// recordingStore records what the agent persists.
type recordingStore struct {
	memory.NoMemory
	started  []memory.Session
	messages []memory.Session // only ID and Agent used as markers
	turns    []int
	roles    []api.Role
	ended    string
	cost     float64
}

func (r *recordingStore) StartSession(_ context.Context, s memory.Session) error {
	r.started = append(r.started, s)
	return nil
}

func (r *recordingStore) AppendMessage(_ context.Context, sessionID string, turn int, m api.Message, _ api.Usage) error {
	r.messages = append(r.messages, memory.Session{ID: sessionID})
	r.turns = append(r.turns, turn)
	r.roles = append(r.roles, m.Role)
	return nil
}

func (r *recordingStore) EndSession(_ context.Context, id string, cost float64) error {
	r.ended, r.cost = id, cost
	return nil
}

// countingCompactor records invocations and passes history through.
type countingCompactor struct {
	calls    int
	lastSeen int
}

func (c *countingCompactor) Compact(_ context.Context, msgs []api.Message) ([]api.Message, error) {
	c.calls++
	c.lastSeen = len(msgs)
	return msgs, nil
}

func text(s string) api.Block { return api.Block{Type: api.BlockText, Text: s} }

func textResponse(s string) provider.Step {
	return provider.Step{Response: api.Response{Content: []api.Block{text(s)}, StopReason: api.StopEndTurn}}
}

func toolUseResponse(id, name, input string) provider.Step {
	return provider.Step{Response: api.Response{
		Content:    []api.Block{text("working"), {Type: api.BlockToolUse, ID: id, Name: name, Input: json.RawMessage(input)}},
		StopReason: api.StopToolUse,
		Usage:      api.Usage{InputTokens: 5, OutputTokens: 3},
	}}
}

func newAgent(p provider.Provider, reg *tool.Registry, g gate.Gate) *Agent {
	return &Agent{Name: "sre", Provider: p, Tools: reg, Gate: g, System: "sys", MaxTurns: 5}
}

func registry(t *testing.T, tools ...tool.Tool) *tool.Registry {
	t.Helper()
	r := tool.NewRegistry()
	for _, tl := range tools {
		if err := r.Register(tl); err != nil {
			t.Fatal(err)
		}
	}
	return r
}

func TestSingleReply(t *testing.T) {
	mock := provider.NewMock(textResponse("hello from groot"))
	var streamed []string
	a := newAgent(mock, registry(t), gate.AutoApprove{})
	a.OnText = func(s string) { streamed = append(streamed, s) }
	a.Provider = mock
	mock.Calls = nil

	reply, err := a.Send(context.Background(), "hi")
	if err != nil {
		t.Fatalf("Send: %v", err)
	}
	if reply != "hello from groot" {
		t.Fatalf("reply %q", reply)
	}
	if strings.Join(streamed, "") != "hello from groot" {
		t.Fatalf("OnText not wired: %v", streamed)
	}
	if mock.Calls[0].System != "sys" || len(mock.Calls[0].Messages) != 1 {
		t.Fatalf("system/messages not passed: %+v", mock.Calls[0])
	}
}

func TestToolLoopExecutesAndFeedsResultBack(t *testing.T) {
	echo := &echoTool{risk: tool.RiskRead, out: "pod-a Running"}
	mock := provider.NewMock(
		toolUseResponse("tu_1", "echo", `{"x":1}`),
		textResponse("all good"),
	)
	dir := t.TempDir()
	al, err := audit.Open(filepath.Join(dir, "audit.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	defer al.Close()

	var calls []string
	a := newAgent(mock, registry(t, echo), gate.AutoApprove{})
	a.Audit = al
	a.OnToolCall = func(name, preview string) { calls = append(calls, name+"|"+preview) }

	reply, err := a.Send(context.Background(), "check pods")
	if err != nil {
		t.Fatalf("Send: %v", err)
	}
	if reply != "all good" {
		t.Fatalf("reply %q", reply)
	}
	if echo.calls != 1 {
		t.Fatalf("tool executed %d times", echo.calls)
	}
	if len(calls) != 1 || calls[0] != `echo|echo {"x":1}` {
		t.Fatalf("OnToolCall got %v", calls)
	}

	second := mock.Calls[1].Messages
	last := second[len(second)-1]
	if last.Role != api.RoleUser || len(last.Content) != 1 {
		t.Fatalf("tool results must go in a user message: %+v", last)
	}
	res := last.Content[0]
	if res.Type != api.BlockToolResult || res.ToolUseID != "tu_1" || res.Content != "pod-a Running" || res.IsError {
		t.Fatalf("tool_result wrong: %+v", res)
	}

	raw, _ := os.ReadFile(filepath.Join(dir, "audit.jsonl"))
	if !strings.Contains(string(raw), `"decision":"allow"`) || strings.Count(string(raw), "\n") != 1 {
		t.Fatalf("audit record missing: %s", raw)
	}
}

func TestDeniedToolReturnsErrorResultWithoutExecuting(t *testing.T) {
	writer := &echoTool{risk: tool.RiskWrite, out: "written"}
	mock := provider.NewMock(
		toolUseResponse("tu_9", "echo", `{"path":"x"}`),
		textResponse("understood"),
	)
	a := newAgent(mock, registry(t, writer), gate.ReadOnly{})

	if _, err := a.Send(context.Background(), "change it"); err != nil {
		t.Fatal(err)
	}
	if writer.calls != 0 {
		t.Fatal("denied tool must not execute")
	}
	res := mock.Calls[1].Messages[len(mock.Calls[1].Messages)-1].Content[0]
	if !res.IsError || res.Content != "denied by user" || res.ToolUseID != "tu_9" {
		t.Fatalf("denial result wrong: %+v", res)
	}
}

func TestUnknownToolIsErrorResult(t *testing.T) {
	mock := provider.NewMock(toolUseResponse("tu_2", "ghost", `{}`), textResponse("ok"))
	a := newAgent(mock, registry(t), gate.AutoApprove{})
	if _, err := a.Send(context.Background(), "x"); err != nil {
		t.Fatal(err)
	}
	res := mock.Calls[1].Messages[len(mock.Calls[1].Messages)-1].Content[0]
	if !res.IsError || !strings.Contains(res.Content, "unknown tool") {
		t.Fatalf("unknown tool result wrong: %+v", res)
	}
}

func TestMaxTurnsStopsLoop(t *testing.T) {
	echo := &echoTool{risk: tool.RiskRead, out: "ok"}
	mock := provider.NewMock(
		toolUseResponse("a", "echo", `{}`),
		toolUseResponse("b", "echo", `{}`),
		toolUseResponse("c", "echo", `{}`),
	)
	a := newAgent(mock, registry(t, echo), gate.AutoApprove{})
	a.MaxTurns = 2
	_, err := a.Send(context.Background(), "loop forever")
	if !errors.Is(err, ErrMaxTurns) {
		t.Fatalf("want ErrMaxTurns, got %v", err)
	}
	if len(mock.Calls) != 2 {
		t.Fatalf("made %d model calls, want 2", len(mock.Calls))
	}
}

func TestCompactionIsInvokedBeforeEachModelCall(t *testing.T) {
	cmp := &countingCompactor{}
	mock := provider.NewMock(toolUseResponse("t", "echo", `{}`), textResponse("done"))
	a := newAgent(mock, registry(t, &echoTool{risk: tool.RiskRead, out: "x"}), gate.AutoApprove{})
	a.Compactor = cmp
	if _, err := a.Send(context.Background(), "go"); err != nil {
		t.Fatal(err)
	}
	if cmp.calls != 2 {
		t.Fatalf("compactor called %d times, want 2 (once per model call)", cmp.calls)
	}
}

func TestMessagesAndSessionArePersisted(t *testing.T) {
	st := &recordingStore{}
	mock := provider.NewMock(textResponse("hello"))
	a := newAgent(mock, registry(t), gate.AutoApprove{})
	a.Memory = st

	if _, err := a.Send(context.Background(), "hi"); err != nil {
		t.Fatal(err)
	}
	if len(st.started) != 1 || st.started[0].ID == "" || st.started[0].Agent != "sre" {
		t.Fatalf("session not started: %+v", st.started)
	}
	if len(st.roles) != 2 || st.roles[0] != api.RoleUser || st.roles[1] != api.RoleAssistant {
		t.Fatalf("expected user then assistant persisted, got %v", st.roles)
	}
	if err := a.End(context.Background()); err != nil {
		t.Fatal(err)
	}
	if st.ended != a.SessionID() {
		t.Fatalf("session ended with wrong id: %q vs %q", st.ended, a.SessionID())
	}
}

func TestProviderErrorIsReturned(t *testing.T) {
	mock := provider.NewMock(provider.Step{Err: &provider.Error{Kind: provider.KindRateLimit, Status: 429, Message: "slow"}})
	a := newAgent(mock, registry(t), gate.AutoApprove{})
	_, err := a.Send(context.Background(), "x")
	if !provider.IsRateLimit(err) {
		t.Fatalf("typed error should surface, got %v", err)
	}
}

func TestNilGateDefaultsToReadOnly(t *testing.T) {
	writer := &echoTool{risk: tool.RiskWrite, out: "written"}
	mock := provider.NewMock(toolUseResponse("x", "echo", `{}`), textResponse("ok"))
	a := &Agent{Provider: mock, Tools: registry(t, writer), MaxTurns: 3}
	if _, err := a.Send(context.Background(), "go"); err != nil {
		t.Fatal(err)
	}
	if writer.calls != 0 {
		t.Fatal("with no gate configured, writes must be denied")
	}
}

func TestCompactNowAppliesStrategyOnDemand(t *testing.T) {
	cmp := &countingCompactor{}
	mock := provider.NewMock(textResponse("first"))
	a := newAgent(mock, registry(t), gate.AutoApprove{})
	a.Compactor = cmp
	if _, err := a.Send(context.Background(), "hello"); err != nil {
		t.Fatal(err)
	}
	before := cmp.calls
	if err := a.Compact(context.Background()); err != nil {
		t.Fatalf("Compact: %v", err)
	}
	if cmp.calls != before+1 {
		t.Fatalf("Compact should run the strategy once, calls %d -> %d", before, cmp.calls)
	}
}

func TestStartOpensSessionBeforeFirstSend(t *testing.T) {
	st := &recordingStore{}
	a := newAgent(provider.NewMock(), registry(t), gate.AutoApprove{})
	a.Memory = st
	a.Start(context.Background())
	if a.SessionID() == "" || len(st.started) != 1 {
		t.Fatalf("Start must open the session: id=%q started=%d", a.SessionID(), len(st.started))
	}
	a.Start(context.Background())
	if len(st.started) != 1 {
		t.Fatal("Start must be idempotent")
	}
}

func TestTranscriptRendersHistory(t *testing.T) {
	mock := provider.NewMock(textResponse("pods look fine"))
	a := newAgent(mock, registry(t), gate.AutoApprove{})
	if a.Transcript() != "" {
		t.Fatal("empty session has an empty transcript")
	}
	if _, err := a.Send(context.Background(), "check pods"); err != nil {
		t.Fatal(err)
	}
	tr := a.Transcript()
	if !strings.Contains(tr, "check pods") || !strings.Contains(tr, "pods look fine") {
		t.Fatalf("transcript incomplete: %s", tr)
	}
}

// slowProvider delays every call so Send is in flight while other goroutines read.
type slowProvider struct {
	*provider.Mock
	delay time.Duration
}

func (s slowProvider) Send(ctx context.Context, req provider.Request) (api.Response, error) {
	time.Sleep(s.delay)
	return s.Mock.Send(ctx, req)
}

func TestTranscriptAndCompactAreSafeDuringSend(t *testing.T) {
	echo := &echoTool{risk: tool.RiskRead, out: "ok"}
	mock := provider.NewMock(toolUseResponse("t1", "echo", `{}`), textResponse("done"))
	a := newAgent(slowProvider{Mock: mock, delay: 20 * time.Millisecond}, registry(t, echo), gate.AutoApprove{})
	a.Compactor = &countingCompactor{}

	ctx := context.Background()
	done := make(chan error, 1)
	go func() {
		_, err := a.Send(ctx, "check")
		done <- err
	}()

	reads := 0
	for finished := false; !finished; {
		select {
		case err := <-done:
			if err != nil {
				t.Fatalf("Send: %v", err)
			}
			finished = true
		default:
			_ = a.Transcript()
			_ = a.SessionID()
			if err := a.Compact(ctx); err != nil {
				t.Fatalf("Compact: %v", err)
			}
			reads++
		}
	}
	if reads == 0 {
		t.Fatal("expected concurrent reads while Send was running")
	}
	if !strings.Contains(a.Transcript(), "done") {
		t.Fatalf("transcript lost the final answer: %s", a.Transcript())
	}
}

func TestMessageCountTracksHistory(t *testing.T) {
	mock := provider.NewMock(textResponse("hi"))
	a := newAgent(mock, registry(t), gate.AutoApprove{})
	if a.MessageCount() != 0 {
		t.Fatal("new agent has no messages")
	}
	if _, err := a.Send(context.Background(), "hello"); err != nil {
		t.Fatal(err)
	}
	if a.MessageCount() != 2 {
		t.Fatalf("want user+assistant = 2, got %d", a.MessageCount())
	}
}

func TestReadOnlyShellIsAllowedAndWritingShellIsDenied(t *testing.T) {
	dir := t.TempDir()
	auditPath := filepath.Join(dir, "audit.jsonl")
	al, err := audit.Open(auditPath)
	if err != nil {
		t.Fatal(err)
	}
	defer al.Close()
	mock := provider.NewMock(
		toolUseResponse("read", "bash", `{"command":"echo classified"}`),
		toolUseResponse("write", "bash", `{"command":"rm -rf target"}`),
		textResponse("done"),
	)
	a := newAgent(mock, registry(t, tool.NewBash(dir)), gate.ReadOnly{})
	a.Audit = al
	if _, err := a.Send(context.Background(), "go"); err != nil {
		t.Fatal(err)
	}
	read := mock.Calls[1].Messages[len(mock.Calls[1].Messages)-1].Content[0]
	if read.IsError || !strings.Contains(read.Content, "classified") {
		t.Fatalf("read-only command should run under the read-only policy: %+v", read)
	}
	denied := mock.Calls[2].Messages[len(mock.Calls[2].Messages)-1].Content[0]
	if !denied.IsError || denied.Content != "denied by user" {
		t.Fatalf("destructive command must be denied: %+v", denied)
	}
	raw, _ := os.ReadFile(auditPath)
	if !strings.Contains(string(raw), `"risk":"read"`) || !strings.Contains(string(raw), `"risk":"exec"`) {
		t.Fatalf("audit must record each call's own risk: %s", raw)
	}
}
