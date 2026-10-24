package provider

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/tinchoram/groot-cli/internal/api"
)

// sse renders events in the Messages API server-sent-event format.
func sse(events ...string) string {
	var sb strings.Builder
	for _, e := range events {
		var typ struct {
			Type string `json:"type"`
		}
		_ = json.Unmarshal([]byte(e), &typ)
		fmt.Fprintf(&sb, "event: %s\ndata: %s\n\n", typ.Type, e)
	}
	return sb.String()
}

const messageStart = `{"type":"message_start","message":{"id":"msg_1","type":"message","role":"assistant","model":"claude-sonnet-5-5","content":[],"stop_reason":null,"stop_sequence":null,"usage":{"input_tokens":25,"output_tokens":1,"cache_creation_input_tokens":0,"cache_read_input_tokens":0}}}`

type fakeAPI struct {
	mu     sync.Mutex
	bodies []string
	reply  string
	status int
}

func (f *fakeAPI) handler(t *testing.T) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		f.mu.Lock()
		f.bodies = append(f.bodies, string(body))
		f.mu.Unlock()
		if r.URL.Path != "/v1/messages" {
			http.NotFound(w, r)
			return
		}
		if f.status != 0 {
			w.Header().Set("Content-Type", "application/json")
			w.Header().Set("Retry-After", "0")
			w.WriteHeader(f.status)
			fmt.Fprint(w, `{"type":"error","error":{"type":"rate_limit_error","message":"slow down"}}`)
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, f.reply)
	})
}

func newTestProvider(t *testing.T, f *fakeAPI, retries int) (*Anthropic, func()) {
	t.Helper()
	srv := httptest.NewServer(f.handler(t))
	a := NewAnthropic(AnthropicOptions{APIKey: "test-key", BaseURL: srv.URL, MaxRetries: retries})
	return a, srv.Close
}

func TestStreamingTextAndUsage(t *testing.T) {
	f := &fakeAPI{reply: sse(
		messageStart,
		`{"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}`,
		`{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"Hello"}}`,
		`{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":" world"}}`,
		`{"type":"content_block_stop","index":0}`,
		`{"type":"message_delta","delta":{"stop_reason":"end_turn","stop_sequence":null},"usage":{"output_tokens":15}}`,
		`{"type":"message_stop"}`,
	)}
	a, done := newTestProvider(t, f, 0)
	defer done()

	var streamed strings.Builder
	resp, err := a.Send(context.Background(), Request{
		System:   "You are groot.",
		Messages: []api.Message{{Role: api.RoleUser, Content: []api.Block{{Type: api.BlockText, Text: "hi"}}}},
		Tools:    []api.ToolDef{{Name: "kubectl", Description: "k", InputSchema: json.RawMessage(`{"type":"object","properties":{"args":{"type":"array"}},"required":["args"]}`)}},
		OnText:   func(d string) { streamed.WriteString(d) },
	})
	if err != nil {
		t.Fatalf("Send: %v", err)
	}
	if resp.Text() != "Hello world" || streamed.String() != "Hello world" {
		t.Fatalf("text=%q streamed=%q", resp.Text(), streamed.String())
	}
	if resp.StopReason != api.StopEndTurn {
		t.Fatalf("stop reason %q", resp.StopReason)
	}
	if resp.Usage.InputTokens != 25 || resp.Usage.OutputTokens != 15 {
		t.Fatalf("usage %+v", resp.Usage)
	}
	if resp.Model != "claude-sonnet-5-5" {
		t.Fatalf("model %q", resp.Model)
	}

	body := f.bodies[0]
	for _, want := range []string{`"stream":true`, `"max_tokens":16000`, `"thinking":{"type":"adaptive"}`, `"cache_control":{"type":"ephemeral"}`, `"model":"claude-sonnet-5-5"`} {
		if !strings.Contains(body, want) {
			t.Errorf("request body missing %s\n%s", want, body)
		}
	}
	if strings.Count(body, `"cache_control"`) < 2 {
		t.Errorf("expected cache_control on system and tools:\n%s", body)
	}

	if a.Usage().InputTokens != 25 {
		t.Fatalf("provider usage not tracked: %+v", a.Usage())
	}
	wantCost := (25*2 + 15*10) / 1e6
	if math.Abs(a.EstimatedCostUSD()-wantCost) > 1e-12 {
		t.Fatalf("cost %v want %v", a.EstimatedCostUSD(), wantCost)
	}
}

func TestStreamingToolUseAndThinkingRoundTrip(t *testing.T) {
	f := &fakeAPI{reply: sse(
		messageStart,
		`{"type":"content_block_start","index":0,"content_block":{"type":"thinking","thinking":"","signature":""}}`,
		`{"type":"content_block_delta","index":0,"delta":{"type":"thinking_delta","thinking":"Check pods."}}`,
		`{"type":"content_block_delta","index":0,"delta":{"type":"signature_delta","signature":"sig123"}}`,
		`{"type":"content_block_stop","index":0}`,
		`{"type":"content_block_start","index":1,"content_block":{"type":"tool_use","id":"tu_1","name":"kubectl","input":{}}}`,
		`{"type":"content_block_delta","index":1,"delta":{"type":"input_json_delta","partial_json":"{\"args\":"}}`,
		`{"type":"content_block_delta","index":1,"delta":{"type":"input_json_delta","partial_json":"[\"get\",\"pods\"]}"}}`,
		`{"type":"content_block_stop","index":1}`,
		`{"type":"message_delta","delta":{"stop_reason":"tool_use","stop_sequence":null},"usage":{"output_tokens":9}}`,
		`{"type":"message_stop"}`,
	)}
	a, done := newTestProvider(t, f, 0)
	defer done()

	resp, err := a.Send(context.Background(), Request{Messages: []api.Message{{Role: api.RoleUser, Content: []api.Block{{Type: api.BlockText, Text: "pods?"}}}}})
	if err != nil {
		t.Fatal(err)
	}
	if resp.StopReason != api.StopToolUse {
		t.Fatalf("stop reason %q", resp.StopReason)
	}
	uses := resp.ToolUses()
	if len(uses) != 1 || uses[0].ID != "tu_1" || uses[0].Name != "kubectl" {
		t.Fatalf("tool_use not mapped: %+v", uses)
	}
	if string(uses[0].Input) != `{"args":["get","pods"]}` {
		t.Fatalf("tool input not accumulated: %s", uses[0].Input)
	}
	var thinking api.Block
	for _, b := range resp.Content {
		if b.Type == api.BlockThinking {
			thinking = b
		}
	}
	if thinking.Signature != "sig123" || thinking.Text != "Check pods." {
		t.Fatalf("thinking block not mapped: %+v", thinking)
	}

	// The next turn must send the thinking block back with its signature and
	// the tool result must reference the tool_use id.
	history := []api.Message{
		{Role: api.RoleUser, Content: []api.Block{{Type: api.BlockText, Text: "pods?"}}},
		{Role: api.RoleAssistant, Content: resp.Content},
		{Role: api.RoleUser, Content: []api.Block{{Type: api.BlockToolResult, ToolUseID: "tu_1", Content: "pod-a Running"}}},
	}
	f.reply = sse(messageStart, `{"type":"message_stop"}`)
	if _, err := a.Send(context.Background(), Request{Messages: history}); err != nil {
		t.Fatal(err)
	}
	body := f.bodies[len(f.bodies)-1]
	for _, want := range []string{`"signature":"sig123"`, `"type":"tool_use"`, `"id":"tu_1"`, `"type":"tool_result"`, `"tool_use_id":"tu_1"`} {
		if !strings.Contains(body, want) {
			t.Errorf("second request missing %s\n%s", want, body)
		}
	}
}

func TestRateLimitIsTypedError(t *testing.T) {
	f := &fakeAPI{status: http.StatusTooManyRequests}
	a, done := newTestProvider(t, f, -1)
	defer done()

	_, err := a.Send(context.Background(), Request{Messages: []api.Message{{Role: api.RoleUser, Content: []api.Block{{Type: api.BlockText, Text: "x"}}}}})
	if err == nil {
		t.Fatal("expected error")
	}
	if !IsRateLimit(err) {
		t.Fatalf("429 should be a rate limit error, got %v", err)
	}
	var pe *Error
	if ok := errors.As(err, &pe); !ok || pe.Status != 429 {
		t.Fatalf("status not preserved: %+v", pe)
	}
}

func TestConnectionFailureIsTypedError(t *testing.T) {
	a := NewAnthropic(AnthropicOptions{APIKey: "k", BaseURL: "http://127.0.0.1:1", MaxRetries: -1})
	_, err := a.Send(context.Background(), Request{Messages: []api.Message{{Role: api.RoleUser, Content: []api.Block{{Type: api.BlockText, Text: "x"}}}}})
	var pe *Error
	if !errors.As(err, &pe) || pe.Kind != KindConnection {
		t.Fatalf("expected connection error, got %v", err)
	}
}

func TestSetModelChangesRequestedModel(t *testing.T) {
	f := &fakeAPI{reply: sse(messageStart, `{"type":"message_stop"}`)}
	a, done := newTestProvider(t, f, 0)
	defer done()
	a.SetModel("claude-haiku-5-5")
	if a.Model() != "claude-haiku-5-5" {
		t.Fatalf("model %s", a.Model())
	}
	_, _ = a.Send(context.Background(), Request{Messages: []api.Message{{Role: api.RoleUser, Content: []api.Block{{Type: api.BlockText, Text: "x"}}}}})
	if !strings.Contains(f.bodies[0], `"model":"claude-haiku-5-5"`) {
		t.Fatalf("model not applied: %s", f.bodies[0])
	}
}
