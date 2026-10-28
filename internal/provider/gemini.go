package provider

import (
	"context"
	crand "crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"strings"
	"sync"

	"google.golang.org/genai"

	"github.com/tinchoram/groot-cli/internal/api"
)

const (
	// DefaultGeminiModel is used when a Gemini provider has no model.
	DefaultGeminiModel = "gemini-3.8-flash"

	// geminiSyntheticIDPrefix marks call ids groot made up because the API
	// returned none. They are kept in the transcript but never sent back.
	geminiSyntheticIDPrefix = "groot-call-"

	// geminiSignaturePrefix marks Block.Signature values that came from
	// Gemini. Other providers' signatures are never replayed to Gemini, and
	// Gemini's are never replayed to Anthropic.
	geminiSignaturePrefix = "gemini:"
)

// GeminiOptions configures the Gemini adapter. Zero values select defaults.
type GeminiOptions struct {
	// APIKey is the Gemini Developer API key. The adapter never reads the
	// environment: the caller resolves the key the same way as for Anthropic.
	APIKey string
	Model  string
	// MaxTokens caps output per response; zero means DefaultMaxTokens.
	MaxTokens int64
	// Effort maps to ThinkingConfig.ThinkingLevel (see geminiThinkingLevel).
	Effort string
	// HTTPClient carries the calls. Nil uses the SDK default; tests inject a
	// fake transport here.
	HTTPClient *http.Client
}

// Gemini is the Provider backed by the Gemini Developer API.
type Gemini struct {
	apiKey    string
	client    *genai.Client
	clientErr error
	maxTokens int32
	effort    string

	mu    sync.Mutex
	model string
	usage tracker
}

// NewGemini builds the adapter. It does not contact the API.
func NewGemini(o GeminiOptions) *Gemini {
	model := o.Model
	if model == "" {
		model = DefaultGeminiModel
	}
	maxTokens := o.MaxTokens
	if maxTokens <= 0 {
		maxTokens = DefaultMaxTokens
	}
	g := &Gemini{
		apiKey:    o.APIKey,
		maxTokens: int32(maxTokens),
		effort:    o.Effort,
		model:     model,
	}
	if o.APIKey != "" {
		g.client, g.clientErr = genai.NewClient(context.Background(), &genai.ClientConfig{
			APIKey:     o.APIKey,
			Backend:    genai.BackendGeminiAPI,
			HTTPClient: o.HTTPClient,
		})
	}
	return g
}

// Model returns the model used for the next call.
func (g *Gemini) Model() string {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.model
}

// SetModel changes the model for subsequent calls.
func (g *Gemini) SetModel(name string) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.model = name
}

// Usage returns the accumulated token usage of this provider.
func (g *Gemini) Usage() api.Usage {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.usage.total
}

// EstimatedCostUSD returns the accumulated estimated cost.
func (g *Gemini) EstimatedCostUSD() float64 {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.usage.cost
}

// Send streams one generateContent call. Text deltas go to req.OnText as they
// arrive; the returned response is the accumulated turn.
func (g *Gemini) Send(ctx context.Context, req Request) (api.Response, error) {
	if err := g.ready(); err != nil {
		return api.Response{}, err
	}
	model := g.Model()
	effort := req.Effort
	if effort == "" {
		effort = g.effort
	}
	contents, err := toGeminiContents(req.Messages)
	if err != nil {
		return api.Response{}, err
	}
	cfg, err := buildGeminiConfig(req, effort, g.maxTokens)
	if err != nil {
		return api.Response{}, err
	}

	var blocks []api.Block
	var usage *genai.GenerateContentResponseUsageMetadata
	var finish genai.FinishReason
	for chunk, err := range g.client.Models.GenerateContentStream(ctx, model, contents, cfg) {
		if err != nil {
			return api.Response{}, classifyGemini(err)
		}
		if chunk.UsageMetadata != nil {
			usage = chunk.UsageMetadata
		}
		if len(chunk.Candidates) == 0 {
			continue
		}
		cand := chunk.Candidates[0]
		if cand.FinishReason != "" {
			finish = cand.FinishReason
		}
		if cand.Content == nil {
			continue
		}
		for _, p := range cand.Content.Parts {
			blocks = appendGeminiPart(blocks, p, req.OnText)
		}
	}

	resp := api.Response{
		Model:      model,
		Content:    blocks,
		StopReason: mapGeminiStop(finish, blocks),
	}
	if usage != nil {
		resp.Usage = geminiUsageOf(usage)
	}
	g.mu.Lock()
	g.usage.add(resp.Usage, model)
	g.mu.Unlock()
	return resp, nil
}

// ListModels returns the Gemini models that can generate content.
func (g *Gemini) ListModels(ctx context.Context) ([]ModelInfo, error) {
	if err := g.ready(); err != nil {
		return nil, err
	}
	var out []ModelInfo
	for m, err := range g.client.Models.All(ctx) {
		if err != nil {
			return nil, classifyGemini(err)
		}
		id := strings.TrimPrefix(m.Name, "models/")
		if !strings.HasPrefix(id, "gemini-") || !slices.Contains(m.SupportedActions, "generateContent") {
			continue
		}
		out = append(out, ModelInfo{ID: id, DisplayName: m.DisplayName})
	}
	return out, nil
}

// ready reports why the adapter cannot call the API, or nil.
func (g *Gemini) ready() error {
	if g.apiKey == "" {
		return &Error{Kind: KindAuth, Message: "no Gemini API key configured"}
	}
	if g.clientErr != nil {
		return &Error{Kind: KindOther, Message: g.clientErr.Error(), Err: g.clientErr}
	}
	return nil
}

// buildGeminiConfig assembles the generation config for one call.
func buildGeminiConfig(req Request, effort string, maxTokens int32) (*genai.GenerateContentConfig, error) {
	tools, err := toGeminiTools(req.Tools)
	if err != nil {
		return nil, err
	}
	cfg := &genai.GenerateContentConfig{
		MaxOutputTokens: maxTokens,
		Tools:           tools,
	}
	if req.System != "" {
		cfg.SystemInstruction = &genai.Content{Parts: []*genai.Part{{Text: req.System}}}
	}
	if level, ok := geminiThinkingLevel(effort); ok {
		cfg.ThinkingConfig = &genai.ThinkingConfig{IncludeThoughts: true, ThinkingLevel: level}
	}
	return cfg, nil
}

// geminiThinkingLevel maps groot's effort to a thinking level. xhigh and max
// clamp to high, which is the highest level Gemini offers.
func geminiThinkingLevel(effort string) (genai.ThinkingLevel, bool) {
	switch effort {
	case "low":
		return genai.ThinkingLevelLow, true
	case "medium":
		return genai.ThinkingLevelMedium, true
	case "high", "xhigh", "max":
		return genai.ThinkingLevelHigh, true
	}
	return "", false
}

// toGeminiContents converts groot messages to Gemini contents. A tool result
// carries only the call id, so its function name is recovered from the
// tool_use with that id earlier in the same request.
func toGeminiContents(msgs []api.Message) ([]*genai.Content, error) {
	names := map[string]string{}
	out := make([]*genai.Content, 0, len(msgs))
	for _, m := range msgs {
		var parts []*genai.Part
		for _, b := range m.Content {
			switch b.Type {
			case api.BlockText:
				sig, err := decodeGeminiSignature(b.Signature)
				if err != nil {
					return nil, err
				}
				if b.Text == "" && sig == nil {
					continue
				}
				parts = append(parts, &genai.Part{Text: b.Text, ThoughtSignature: sig})
			case api.BlockToolUse:
				names[b.ID] = b.Name
				args := map[string]any{}
				if len(b.Input) > 0 {
					if err := json.Unmarshal(b.Input, &args); err != nil {
						return nil, fmt.Errorf("tool %s: input is not a JSON object: %w", b.Name, err)
					}
				}
				sig, err := decodeGeminiSignature(b.Signature)
				if err != nil {
					return nil, err
				}
				parts = append(parts, &genai.Part{
					FunctionCall:     &genai.FunctionCall{ID: apiCallID(b.ID), Name: b.Name, Args: args},
					ThoughtSignature: sig,
				})
			case api.BlockToolResult:
				name, ok := names[b.ToolUseID]
				if !ok {
					return nil, fmt.Errorf("tool result %s has no matching tool call in this request", b.ToolUseID)
				}
				key := "output"
				if b.IsError {
					key = "error"
				}
				parts = append(parts, &genai.Part{FunctionResponse: &genai.FunctionResponse{
					ID:       apiCallID(b.ToolUseID),
					Name:     name,
					Response: map[string]any{key: b.Content},
				}})
			case api.BlockThinking:
				// Only thoughts Gemini signed can be replayed; the rest are dropped.
				sig, err := decodeGeminiSignature(b.Signature)
				if err != nil {
					return nil, err
				}
				if sig == nil {
					continue
				}
				parts = append(parts, &genai.Part{Text: b.Text, Thought: true, ThoughtSignature: sig})
			default:
				return nil, fmt.Errorf("unsupported block type %q", b.Type)
			}
		}
		switch m.Role {
		case api.RoleUser:
			out = append(out, &genai.Content{Role: "user", Parts: parts})
		case api.RoleAssistant:
			out = append(out, &genai.Content{Role: "model", Parts: parts})
		default:
			return nil, fmt.Errorf("unsupported role %q", m.Role)
		}
	}
	return out, nil
}

// apiCallID returns the id to send to the API, or empty for an id groot made up.
func apiCallID(id string) string {
	if strings.HasPrefix(id, geminiSyntheticIDPrefix) {
		return ""
	}
	return id
}

// appendGeminiPart folds one streamed part into the turn. Consecutive text or
// thought deltas merge into one block; a function call is always its own block.
func appendGeminiPart(blocks []api.Block, p *genai.Part, onText func(string)) []api.Block {
	sig := encodeGeminiSignature(p.ThoughtSignature)
	last := len(blocks) - 1
	switch {
	case p.FunctionCall != nil:
		id := p.FunctionCall.ID
		if id == "" {
			id = newSyntheticCallID()
		}
		args := p.FunctionCall.Args
		if args == nil {
			args = map[string]any{}
		}
		input, _ := json.Marshal(args)
		return append(blocks, api.Block{Type: api.BlockToolUse, ID: id, Name: p.FunctionCall.Name, Input: input, Signature: sig})
	case p.Thought:
		if p.Text == "" && sig == "" {
			return blocks
		}
		if last >= 0 && blocks[last].Type == api.BlockThinking {
			blocks[last].Text += p.Text
			if sig != "" {
				blocks[last].Signature = sig
			}
			return blocks
		}
		return append(blocks, api.Block{Type: api.BlockThinking, Text: p.Text, Signature: sig})
	default:
		if p.Text == "" && sig == "" {
			return blocks
		}
		if p.Text != "" && onText != nil {
			onText(p.Text)
		}
		if last >= 0 && blocks[last].Type == api.BlockText {
			blocks[last].Text += p.Text
			if sig != "" {
				blocks[last].Signature = sig
			}
			return blocks
		}
		return append(blocks, api.Block{Type: api.BlockText, Text: p.Text, Signature: sig})
	}
}

func toGeminiTools(defs []api.ToolDef) ([]*genai.Tool, error) {
	if len(defs) == 0 {
		return nil, nil
	}
	decls := make([]*genai.FunctionDeclaration, 0, len(defs))
	for _, d := range defs {
		schema := json.RawMessage(`{"type":"object","properties":{}}`)
		if len(d.InputSchema) > 0 {
			if !json.Valid(d.InputSchema) {
				return nil, fmt.Errorf("tool %s: invalid input schema", d.Name)
			}
			schema = d.InputSchema
		}
		decls = append(decls, &genai.FunctionDeclaration{
			Name:                 d.Name,
			Description:          d.Description,
			ParametersJsonSchema: schema,
		})
	}
	return []*genai.Tool{{FunctionDeclarations: decls}}, nil
}

func geminiUsageOf(u *genai.GenerateContentResponseUsageMetadata) api.Usage {
	input := int64(u.PromptTokenCount) - int64(u.CachedContentTokenCount)
	if input < 0 {
		input = 0
	}
	return api.Usage{
		InputTokens:     input,
		OutputTokens:    int64(u.CandidatesTokenCount) + int64(u.ThoughtsTokenCount),
		CacheReadTokens: int64(u.CachedContentTokenCount),
	}
}

func mapGeminiStop(f genai.FinishReason, blocks []api.Block) api.StopReason {
	switch f {
	case genai.FinishReasonStop:
		for _, b := range blocks {
			if b.Type == api.BlockToolUse {
				return api.StopToolUse
			}
		}
		return api.StopEndTurn
	case genai.FinishReasonMaxTokens:
		return api.StopMaxTokens
	case genai.FinishReasonSafety, genai.FinishReasonProhibitedContent, genai.FinishReasonBlocklist:
		return api.StopRefusal
	}
	return api.StopOther
}

func encodeGeminiSignature(sig []byte) string {
	if len(sig) == 0 {
		return ""
	}
	return geminiSignaturePrefix + base64.StdEncoding.EncodeToString(sig)
}

// decodeGeminiSignature returns the raw signature for a Gemini-issued value,
// nil for an empty or foreign one.
func decodeGeminiSignature(s string) ([]byte, error) {
	if !strings.HasPrefix(s, geminiSignaturePrefix) {
		return nil, nil
	}
	raw, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(s, geminiSignaturePrefix))
	if err != nil {
		return nil, fmt.Errorf("gemini thought signature: %w", err)
	}
	return raw, nil
}

func newSyntheticCallID() string {
	return geminiSyntheticIDPrefix + crand.Text()
}

// classifyGemini turns SDK errors into typed provider errors.
func classifyGemini(err error) error {
	if errors.Is(err, context.Canceled) {
		return err
	}
	var apiErr genai.APIError
	if errors.As(err, &apiErr) && apiErr.Code != 0 {
		kind := KindStatus
		switch apiErr.Code {
		case http.StatusTooManyRequests:
			kind = KindRateLimit
		case http.StatusUnauthorized, http.StatusForbidden:
			kind = KindAuth
		}
		return &Error{Kind: kind, Status: apiErr.Code, Message: err.Error(), Err: err}
	}
	return &Error{Kind: KindConnection, Message: err.Error(), Err: err}
}
