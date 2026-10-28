package provider

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"testing"

	"github.com/tinchoram/groot-cli/internal/api"
)

// geminiFake is an http.RoundTripper that serves canned Gemini responses and
// records what the adapter sent. No test reaches the network.
type geminiFake struct {
	mu      sync.Mutex
	paths   []string
	headers []http.Header
	bodies  []map[string]any
	reply   string // server-sent events body for streamGenerateContent
	status  int    // non-zero: answer with this HTTP status and errBody
	errBody string
	err     error // non-nil: the transport fails
}

func (f *geminiFake) RoundTrip(r *http.Request) (*http.Response, error) {
	raw, _ := io.ReadAll(r.Body)
	var body map[string]any
	_ = json.Unmarshal(raw, &body)
	f.mu.Lock()
	f.paths = append(f.paths, r.URL.Path)
	f.headers = append(f.headers, r.Header.Clone())
	f.bodies = append(f.bodies, body)
	f.mu.Unlock()
	if f.err != nil {
		return nil, f.err
	}
	if f.status != 0 {
		return &http.Response{
			StatusCode: f.status,
			Header:     http.Header{"Content-Type": {"application/json"}},
			Body:       io.NopCloser(strings.NewReader(f.errBody)),
			Request:    r,
		}, nil
	}
	if strings.HasSuffix(r.URL.Path, "/models") {
		return jsonResponse(r, `{"models":[
			{"name":"models/gemini-3.8-flash","displayName":"Gemini 3.8 Flash","supportedGenerationMethods":["generateContent","countTokens"]},
			{"name":"models/gemini-embedding-1","displayName":"Embedding","supportedGenerationMethods":["embedContent"]},
			{"name":"models/gemma-4","displayName":"Gemma 4","supportedGenerationMethods":["generateContent"]}]}`), nil
	}
	return &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": {"text/event-stream"}},
		Body:       io.NopCloser(strings.NewReader(f.reply)),
		Request:    r,
	}, nil
}

func jsonResponse(r *http.Request, body string) *http.Response {
	return &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": {"application/json"}},
		Body:       io.NopCloser(strings.NewReader(body)),
		Request:    r,
	}
}

// geminiChunk renders one streamed GenerateContentResponse as an SSE event.
func geminiChunk(parts, finish, usage string) string {
	cand := fmt.Sprintf(`{"content":{"role":"model","parts":[%s]}`, parts)
	if finish != "" {
		cand += fmt.Sprintf(`,"finishReason":%q`, finish)
	}
	cand += "}"
	body := fmt.Sprintf(`{"candidates":[%s]`, cand)
	if usage != "" {
		body += `,"usageMetadata":` + usage
	}
	return "data: " + body + "}\n\n"
}

const geminiUsage = `{"promptTokenCount":1000,"cachedContentTokenCount":400,"candidatesTokenCount":50,"thoughtsTokenCount":30,"totalTokenCount":1080}`

func newGeminiFake(t *testing.T, f *geminiFake, o ...func(*GeminiOptions)) *Gemini {
	t.Helper()
	opts := GeminiOptions{Model: "gemini-3.8-flash", APIKey: "test-key", HTTPClient: &http.Client{Transport: f}}
	for _, fn := range o {
		fn(&opts)
	}
	return NewGemini(opts)
}

func userText(s string) api.Message {
	return api.Message{Role: api.RoleUser, Content: []api.Block{{Type: api.BlockText, Text: s}}}
}

// lastBody returns the JSON body of the most recent request.
func lastBody(t *testing.T, f *geminiFake) map[string]any {
	t.Helper()
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.bodies) == 0 {
		t.Fatal("no request was sent")
	}
	return f.bodies[len(f.bodies)-1]
}

// contentsOf returns the contents array of a request body.
func contentsOf(t *testing.T, body map[string]any) []map[string]any {
	t.Helper()
	raw, _ := body["contents"].([]any)
	out := make([]map[string]any, 0, len(raw))
	for _, c := range raw {
		out = append(out, c.(map[string]any))
	}
	return out
}

func TestGeminiTextTurnStreamsDeltasAndSendsSystemAndKey(t *testing.T) {
	f := &geminiFake{reply: geminiChunk(`{"text":"Hel"}`, "", "") +
		geminiChunk(`{"text":"lo"}`, "STOP", geminiUsage)}
	g := newGeminiFake(t, f)

	var deltas []string
	resp, err := g.Send(context.Background(), Request{
		System:   "be brief",
		Messages: []api.Message{userText("hi")},
		OnText:   func(d string) { deltas = append(deltas, d) },
	})
	if err != nil {
		t.Fatalf("Send: %v", err)
	}
	if strings.Join(deltas, "") != "Hello" || len(deltas) != 2 {
		t.Fatalf("deltas = %q, want two deltas forming Hello", deltas)
	}
	if len(resp.Content) != 1 || resp.Content[0].Type != api.BlockText || resp.Content[0].Text != "Hello" {
		t.Fatalf("content = %+v, want one text block Hello", resp.Content)
	}
	if resp.StopReason != api.StopEndTurn {
		t.Fatalf("stop = %q, want end_turn", resp.StopReason)
	}
	if !strings.Contains(f.paths[0], "gemini-3.8-flash:streamGenerateContent") {
		t.Fatalf("path = %q, want the streaming method for the configured model", f.paths[0])
	}
	if got := f.headers[0].Get("x-goog-api-key"); got != "test-key" {
		t.Fatalf("x-goog-api-key = %q, want the configured key", got)
	}
	body := lastBody(t, f)
	sys, _ := body["systemInstruction"].(map[string]any)
	if sys == nil || !strings.Contains(fmt.Sprint(sys), "be brief") {
		t.Fatalf("systemInstruction = %v, want the system prompt", sys)
	}
	cs := contentsOf(t, body)
	if len(cs) != 1 || cs[0]["role"] != "user" {
		t.Fatalf("contents = %v, want one user content", cs)
	}
}

func TestGeminiToolCallTurnMapsFunctionCallAndStopReason(t *testing.T) {
	f := &geminiFake{reply: geminiChunk(
		`{"functionCall":{"id":"fc_9","name":"read_file","args":{"path":"a.txt"}}}`, "STOP", geminiUsage)}
	g := newGeminiFake(t, f)

	resp, err := g.Send(context.Background(), Request{
		Messages: []api.Message{userText("read a.txt")},
		Tools: []api.ToolDef{{
			Name:        "read_file",
			Description: "read a file",
			InputSchema: json.RawMessage(`{"type":"object","properties":{"path":{"type":"string"}},"required":["path"]}`),
		}},
	})
	if err != nil {
		t.Fatalf("Send: %v", err)
	}
	if resp.StopReason != api.StopToolUse {
		t.Fatalf("stop = %q, want tool_use when a function call is present", resp.StopReason)
	}
	if len(resp.Content) != 1 {
		t.Fatalf("content = %+v, want one tool_use block", resp.Content)
	}
	b := resp.Content[0]
	if b.Type != api.BlockToolUse || b.ID != "fc_9" || b.Name != "read_file" || string(b.Input) != `{"path":"a.txt"}` {
		t.Fatalf("tool_use block = %+v", b)
	}

	body := lastBody(t, f)
	tools, _ := body["tools"].([]any)
	if len(tools) != 1 {
		t.Fatalf("tools = %v, want one tool entry", body["tools"])
	}
	decls := tools[0].(map[string]any)["functionDeclarations"].([]any)
	decl := decls[0].(map[string]any)
	if decl["name"] != "read_file" || decl["parametersJsonSchema"] == nil {
		t.Fatalf("declaration = %v, want name and parametersJsonSchema", decl)
	}
}

func TestGeminiToolResultReplayRecoversNameFromToolUse(t *testing.T) {
	f := &geminiFake{reply: geminiChunk(`{"text":"done"}`, "STOP", geminiUsage)}
	g := newGeminiFake(t, f)

	msgs := []api.Message{
		userText("read a.txt"),
		{Role: api.RoleAssistant, Content: []api.Block{{
			Type: api.BlockToolUse, ID: "fc_9", Name: "read_file", Input: json.RawMessage(`{"path":"a.txt"}`),
		}}},
		{Role: api.RoleUser, Content: []api.Block{{
			Type: api.BlockToolResult, ToolUseID: "fc_9", Content: "file body",
		}}},
		{Role: api.RoleUser, Content: []api.Block{{
			Type: api.BlockToolResult, ToolUseID: "fc_9", Content: "boom", IsError: true,
		}}},
	}
	if _, err := g.Send(context.Background(), Request{Messages: msgs}); err != nil {
		t.Fatalf("Send: %v", err)
	}
	cs := contentsOf(t, lastBody(t, f))
	if len(cs) != 4 {
		t.Fatalf("contents = %d, want 4", len(cs))
	}
	if cs[1]["role"] != "model" {
		t.Fatalf("assistant role = %v, want model", cs[1]["role"])
	}
	call := cs[1]["parts"].([]any)[0].(map[string]any)["functionCall"].(map[string]any)
	if call["name"] != "read_file" || call["id"] != "fc_9" {
		t.Fatalf("functionCall = %v", call)
	}
	resp := cs[2]["parts"].([]any)[0].(map[string]any)["functionResponse"].(map[string]any)
	if resp["name"] != "read_file" || resp["id"] != "fc_9" {
		t.Fatalf("functionResponse = %v, want the name recovered from the tool_use", resp)
	}
	if out := resp["response"].(map[string]any); out["output"] != "file body" {
		t.Fatalf("response = %v, want output file body", out)
	}
	errResp := cs[3]["parts"].([]any)[0].(map[string]any)["functionResponse"].(map[string]any)
	if out := errResp["response"].(map[string]any); out["error"] != "boom" || errResp["name"] != "read_file" {
		t.Fatalf("error response = %v", errResp)
	}
}

func TestGeminiSyntheticCallIDsAreNotSentBack(t *testing.T) {
	f := &geminiFake{reply: geminiChunk(`{"functionCall":{"name":"read_file","args":{"path":"x"}}}`, "STOP", "")}
	g := newGeminiFake(t, f)

	resp, err := g.Send(context.Background(), Request{Messages: []api.Message{userText("go")}})
	if err != nil {
		t.Fatalf("Send: %v", err)
	}
	id := resp.Content[0].ID
	if id == "" {
		t.Fatal("a call without an API id must still get a groot id")
	}
	msgs := []api.Message{
		userText("go"),
		{Role: api.RoleAssistant, Content: resp.Content},
		{Role: api.RoleUser, Content: []api.Block{{Type: api.BlockToolResult, ToolUseID: id, Content: "ok"}}},
	}
	if _, err := g.Send(context.Background(), Request{Messages: msgs}); err != nil {
		t.Fatalf("second Send: %v", err)
	}
	cs := contentsOf(t, lastBody(t, f))
	call := cs[1]["parts"].([]any)[0].(map[string]any)["functionCall"].(map[string]any)
	if _, sent := call["id"]; sent {
		t.Fatalf("groot-generated id %q was sent to the API: %v", id, call)
	}
	resp2 := cs[2]["parts"].([]any)[0].(map[string]any)["functionResponse"].(map[string]any)
	if _, sent := resp2["id"]; sent || resp2["name"] != "read_file" {
		t.Fatalf("functionResponse = %v, want the name and no synthetic id", resp2)
	}
}

func TestGeminiThinkingSignatureIsReplayedUnchanged(t *testing.T) {
	sigThought := base64.StdEncoding.EncodeToString([]byte{0x01, 0xff, 0x7a})
	sigAnswer := base64.StdEncoding.EncodeToString([]byte{0x42, 0x10})
	f := &geminiFake{reply: geminiChunk(`{"text":"plan","thought":true,"thoughtSignature":"`+sigThought+`"}`, "", "") +
		geminiChunk(`{"text":"answer","thoughtSignature":"`+sigAnswer+`"}`, "STOP", geminiUsage)}
	g := newGeminiFake(t, f)

	resp, err := g.Send(context.Background(), Request{Messages: []api.Message{userText("q")}, Effort: "low"})
	if err != nil {
		t.Fatalf("Send: %v", err)
	}
	if len(resp.Content) != 2 || resp.Content[0].Type != api.BlockThinking || resp.Content[0].Text != "plan" {
		t.Fatalf("content = %+v, want a thinking block then the answer", resp.Content)
	}
	if resp.Content[0].Signature != "gemini:"+sigThought || resp.Content[1].Signature != "gemini:"+sigAnswer {
		t.Fatalf("signatures = %q / %q, want the Gemini-prefixed values", resp.Content[0].Signature, resp.Content[1].Signature)
	}

	msgs := []api.Message{userText("q"), {Role: api.RoleAssistant, Content: resp.Content}, userText("again")}
	if _, err := g.Send(context.Background(), Request{Messages: msgs}); err != nil {
		t.Fatalf("second Send: %v", err)
	}
	cs := contentsOf(t, lastBody(t, f))
	parts := cs[1]["parts"].([]any)
	thought := parts[0].(map[string]any)
	if thought["thought"] != true || thought["text"] != "plan" || thought["thoughtSignature"] != sigThought {
		t.Fatalf("replayed thought part = %v", thought)
	}
	answer := parts[1].(map[string]any)
	if answer["thoughtSignature"] != sigAnswer {
		t.Fatalf("replayed signature = %v, want %q unchanged", answer["thoughtSignature"], sigAnswer)
	}
}

func TestGeminiDropsUnsignedThoughtsAndForeignSignatures(t *testing.T) {
	msgs := []api.Message{
		userText("q"),
		{Role: api.RoleAssistant, Content: []api.Block{
			{Type: api.BlockThinking, Text: "unsigned"},
			{Type: api.BlockThinking, Text: "anthropic", Signature: "EuYBCkQYAiJA-anthropic"},
			{Type: api.BlockText, Text: "kept"},
		}},
	}
	cs, err := toGeminiContents(msgs)
	if err != nil {
		t.Fatalf("toGeminiContents: %v", err)
	}
	if len(cs[1].Parts) != 1 || cs[1].Parts[0].Text != "kept" {
		t.Fatalf("assistant parts = %+v, want only the text block", cs[1].Parts)
	}
}

func TestAnthropicDropsGeminiSignatures(t *testing.T) {
	msgs := []api.Message{
		userText("q"),
		{Role: api.RoleAssistant, Content: []api.Block{
			{Type: api.BlockThinking, Text: "gemini thought", Signature: "gemini:AAE="},
		}},
	}
	out, err := toSDKMessages(msgs)
	if err != nil {
		t.Fatalf("toSDKMessages: %v", err)
	}
	if len(out[1].Content) != 0 {
		t.Fatalf("a Gemini-signed thought was replayed to Anthropic: %+v", out[1].Content)
	}
}

func TestGeminiUsageAccountingSplitsCachedAndThinkingTokens(t *testing.T) {
	f := &geminiFake{reply: geminiChunk(`{"text":"ok"}`, "STOP", geminiUsage)}
	g := newGeminiFake(t, f, func(o *GeminiOptions) { o.Model = "gemini-4-argon" })

	if _, err := g.Send(context.Background(), Request{Messages: []api.Message{userText("hi")}}); err != nil {
		t.Fatalf("Send: %v", err)
	}
	u := g.Usage()
	// prompt 1000 minus cached 400 is input; candidates 50 plus thoughts 30 is output.
	if u.InputTokens != 600 || u.OutputTokens != 80 || u.CacheReadTokens != 400 || u.CacheCreationTokens != 0 {
		t.Fatalf("usage = %+v, want input 600, output 80, cache read 400, no cache write", u)
	}
	// 600*2 + 80*10 + 400*2*0.05 = 2040 per million tokens.
	want := 0.00204
	if got := g.EstimatedCostUSD(); got < want-1e-9 || got > want+1e-9 {
		t.Fatalf("cost = %v, want %v (cached reads at 0.05 of input)", got, want)
	}
}

func TestGeminiEffortMapsToThinkingLevelWithClamp(t *testing.T) {
	cases := map[string]string{
		"low":    "LOW",
		"medium": "MEDIUM",
		"high":   "HIGH",
		"xhigh":  "HIGH",
		"max":    "HIGH",
	}
	for effort, want := range cases {
		t.Run(effort, func(t *testing.T) {
			f := &geminiFake{reply: geminiChunk(`{"text":"ok"}`, "STOP", "")}
			g := newGeminiFake(t, f)
			if _, err := g.Send(context.Background(), Request{Messages: []api.Message{userText("hi")}, Effort: effort}); err != nil {
				t.Fatalf("Send: %v", err)
			}
			gc, _ := lastBody(t, f)["generationConfig"].(map[string]any)
			tc, _ := gc["thinkingConfig"].(map[string]any)
			if tc["thinkingLevel"] != want {
				t.Fatalf("thinkingLevel = %v, want %s", tc["thinkingLevel"], want)
			}
		})
	}
}

func TestGeminiNoEffortSendsNoThinkingConfig(t *testing.T) {
	f := &geminiFake{reply: geminiChunk(`{"text":"ok"}`, "STOP", "")}
	g := newGeminiFake(t, f)
	if _, err := g.Send(context.Background(), Request{Messages: []api.Message{userText("hi")}}); err != nil {
		t.Fatalf("Send: %v", err)
	}
	if gc, _ := lastBody(t, f)["generationConfig"].(map[string]any); gc != nil {
		if _, set := gc["thinkingConfig"]; set {
			t.Fatalf("thinkingConfig sent without an effort: %v", gc)
		}
	}
}

func TestGeminiFinishReasonsMapToStopReasons(t *testing.T) {
	cases := map[string]api.StopReason{
		"MAX_TOKENS":         api.StopMaxTokens,
		"SAFETY":             api.StopRefusal,
		"PROHIBITED_CONTENT": api.StopRefusal,
		"BLOCKLIST":          api.StopRefusal,
		"RECITATION":         api.StopOther,
	}
	for finish, want := range cases {
		f := &geminiFake{reply: geminiChunk(`{"text":"x"}`, finish, "")}
		g := newGeminiFake(t, f)
		resp, err := g.Send(context.Background(), Request{Messages: []api.Message{userText("hi")}})
		if err != nil {
			t.Fatalf("%s: Send: %v", finish, err)
		}
		if resp.StopReason != want {
			t.Fatalf("%s: stop = %q, want %q", finish, resp.StopReason, want)
		}
	}
}

func TestGeminiRateLimitAndAuthAreTypedErrors(t *testing.T) {
	cases := []struct {
		status int
		kind   ErrorKind
	}{
		{http.StatusTooManyRequests, KindRateLimit},
		{http.StatusUnauthorized, KindAuth},
		{http.StatusForbidden, KindAuth},
		{http.StatusInternalServerError, KindStatus},
	}
	for _, c := range cases {
		f := &geminiFake{status: c.status, errBody: fmt.Sprintf(`{"error":{"code":%d,"message":"nope","status":"X"}}`, c.status)}
		g := newGeminiFake(t, f)
		_, err := g.Send(context.Background(), Request{Messages: []api.Message{userText("hi")}})
		var pe *Error
		if !errors.As(err, &pe) {
			t.Fatalf("HTTP %d: error %v is not a typed provider error", c.status, err)
		}
		if pe.Kind != c.kind || pe.Status != c.status {
			t.Fatalf("HTTP %d: kind %v status %d, want kind %v", c.status, pe.Kind, pe.Status, c.kind)
		}
	}
}

func TestGeminiTransportFailureIsConnectionError(t *testing.T) {
	f := &geminiFake{err: errors.New("dial refused")}
	g := newGeminiFake(t, f)
	_, err := g.Send(context.Background(), Request{Messages: []api.Message{userText("hi")}})
	var pe *Error
	if !errors.As(err, &pe) || pe.Kind != KindConnection {
		t.Fatalf("error = %v, want a KindConnection provider error", err)
	}
}

func TestGeminiListModelsKeepsGenerateContentGeminiModels(t *testing.T) {
	f := &geminiFake{}
	g := newGeminiFake(t, f)
	models, err := g.ListModels(context.Background())
	if err != nil {
		t.Fatalf("ListModels: %v", err)
	}
	if len(models) != 1 || models[0].ID != "gemini-3.8-flash" || models[0].DisplayName != "Gemini 3.8 Flash" {
		t.Fatalf("models = %+v, want only gemini-3.8-flash (no embedding models, no non-gemini ids)", models)
	}
}

func TestGeminiSendWithoutKeyIsAuthError(t *testing.T) {
	t.Setenv("GEMINI_API_KEY", "")
	t.Setenv("GOOGLE_API_KEY", "")
	f := &geminiFake{status: http.StatusUnauthorized, errBody: `{"error":{"code":401,"message":"no key"}}`}
	g := NewGemini(GeminiOptions{Model: "gemini-3.8-flash", HTTPClient: &http.Client{Transport: f}})
	_, err := g.Send(context.Background(), Request{Messages: []api.Message{userText("hi")}})
	var pe *Error
	if !errors.As(err, &pe) || pe.Kind != KindAuth {
		t.Fatalf("error = %v, want a KindAuth provider error", err)
	}
	if len(f.paths) != 0 {
		t.Fatalf("a call without a key reached the transport: %v", f.paths)
	}
}

func TestGeminiModelsHaveTheirOwnContextWindow(t *testing.T) {
	for _, id := range []string{"gemini-4-argon", "gemini-3.8-flash"} {
		if got := ContextWindow(id, nil); got != 1_048_576 {
			t.Fatalf("ContextWindow(%s) = %d, want 1048576 (the documented input limit)", id, got)
		}
	}
}
