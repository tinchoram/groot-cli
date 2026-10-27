package provider

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/option"

	"github.com/tinchoram/groot-cli/internal/api"
)

const (
	// DefaultAnthropicModel is used when no model is configured.
	DefaultAnthropicModel = "claude-sonnet-5-5"
	// DefaultMaxTokens is the per-response output limit.
	DefaultMaxTokens int64 = 16000
)

// AnthropicOptions configures the Anthropic adapter. Zero values select defaults.
type AnthropicOptions struct {
	// APIKey, when empty, is read by the SDK from ANTHROPIC_API_KEY.
	APIKey string
	// BaseURL overrides the API endpoint (tests and proxies).
	BaseURL string
	Model   string
	// MaxTokens caps output per response; zero means DefaultMaxTokens.
	MaxTokens int64
	// MaxRetries: zero keeps the SDK default, negative disables retries.
	MaxRetries int
	// Effort is sent as output_config.effort when set (see api.Efforts).
	Effort string
}

// Anthropic is the Provider backed by the Anthropic Messages API.
type Anthropic struct {
	client    anthropic.Client
	maxTokens int64
	effort    string

	mu    sync.Mutex
	model string
	usage tracker
}

// NewAnthropic builds the adapter. It does not contact the API.
func NewAnthropic(o AnthropicOptions) *Anthropic {
	var opts []option.RequestOption
	if o.APIKey != "" {
		opts = append(opts, option.WithAPIKey(o.APIKey))
	}
	if o.BaseURL != "" {
		opts = append(opts, option.WithBaseURL(o.BaseURL))
	}
	switch {
	case o.MaxRetries < 0:
		opts = append(opts, option.WithMaxRetries(0))
	case o.MaxRetries > 0:
		opts = append(opts, option.WithMaxRetries(o.MaxRetries))
	}
	model := o.Model
	if model == "" {
		model = DefaultAnthropicModel
	}
	maxTokens := o.MaxTokens
	if maxTokens <= 0 {
		maxTokens = DefaultMaxTokens
	}
	return &Anthropic{
		client:    anthropic.NewClient(opts...),
		maxTokens: maxTokens,
		effort:    o.Effort,
		model:     model,
	}
}

// Model returns the model used for the next call.
func (a *Anthropic) Model() string {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.model
}

// SetModel changes the model for subsequent calls.
func (a *Anthropic) SetModel(name string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.model = name
}

// Usage returns the accumulated token usage of this provider.
func (a *Anthropic) Usage() api.Usage {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.usage.total
}

// EstimatedCostUSD returns the accumulated estimated cost.
func (a *Anthropic) EstimatedCostUSD() float64 {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.usage.cost
}

// Send streams one Messages API call. Text deltas go to req.OnText as they
// arrive; the returned response is the fully accumulated message.
func (a *Anthropic) Send(ctx context.Context, req Request) (api.Response, error) {
	model := a.Model()
	if req.Effort == "" {
		req.Effort = a.effort
	}
	params, err := buildParams(req, model, a.maxTokens)
	if err != nil {
		return api.Response{}, err
	}

	stream := a.client.Messages.NewStreaming(ctx, params)
	var msg anthropic.Message
	for stream.Next() {
		event := stream.Current()
		if req.OnText != nil {
			if delta, ok := event.AsAny().(anthropic.ContentBlockDeltaEvent); ok {
				if td, ok := delta.Delta.AsAny().(anthropic.TextDelta); ok && td.Text != "" {
					req.OnText(td.Text)
				}
			}
		}
		if err := msg.Accumulate(event); err != nil {
			return api.Response{}, &Error{Kind: KindOther, Message: err.Error(), Err: err}
		}
	}
	if err := stream.Err(); err != nil {
		return api.Response{}, classify(err)
	}

	resp := fromSDKMessage(msg)
	a.mu.Lock()
	a.usage.add(resp.Usage, model)
	a.mu.Unlock()
	return resp, nil
}

func buildParams(req Request, model string, maxTokens int64) (anthropic.MessageNewParams, error) {
	msgs, err := toSDKMessages(req.Messages)
	if err != nil {
		return anthropic.MessageNewParams{}, err
	}
	tools, err := toSDKTools(req.Tools)
	if err != nil {
		return anthropic.MessageNewParams{}, err
	}
	params := anthropic.MessageNewParams{
		Model:     model,
		MaxTokens: maxTokens,
		Messages:  msgs,
		Tools:     tools,
		Thinking:  anthropic.ThinkingConfigParamUnion{OfAdaptive: &anthropic.ThinkingConfigAdaptiveParam{}},
	}
	if req.Effort != "" {
		params.OutputConfig.Effort = anthropic.OutputConfigEffort(req.Effort)
	}
	if req.System != "" {
		// Caching the system block caches tools and system together as one prefix.
		params.System = []anthropic.TextBlockParam{{
			Text:         req.System,
			CacheControl: anthropic.NewCacheControlEphemeralParam(),
		}}
	}
	return params, nil
}

func toSDKMessages(msgs []api.Message) ([]anthropic.MessageParam, error) {
	out := make([]anthropic.MessageParam, 0, len(msgs))
	for _, m := range msgs {
		blocks := make([]anthropic.ContentBlockParamUnion, 0, len(m.Content))
		for _, b := range m.Content {
			switch b.Type {
			case api.BlockText:
				blocks = append(blocks, anthropic.NewTextBlock(b.Text))
			case api.BlockToolUse:
				input := json.RawMessage(b.Input)
				if len(input) == 0 {
					input = json.RawMessage(`{}`)
				}
				blocks = append(blocks, anthropic.NewToolUseBlock(b.ID, input, b.Name))
			case api.BlockToolResult:
				blocks = append(blocks, anthropic.NewToolResultBlock(b.ToolUseID, b.Content, b.IsError))
			case api.BlockThinking:
				// Thinking must be replayed unmodified; a block without its signature would be rejected.
				if b.Signature != "" {
					blocks = append(blocks, anthropic.ContentBlockParamUnion{
						OfThinking: &anthropic.ThinkingBlockParam{Thinking: b.Text, Signature: b.Signature},
					})
				}
			default:
				return nil, fmt.Errorf("unsupported block type %q", b.Type)
			}
		}
		switch m.Role {
		case api.RoleUser:
			out = append(out, anthropic.NewUserMessage(blocks...))
		case api.RoleAssistant:
			out = append(out, anthropic.NewAssistantMessage(blocks...))
		default:
			return nil, fmt.Errorf("unsupported role %q", m.Role)
		}
	}
	return out, nil
}

func toSDKTools(defs []api.ToolDef) ([]anthropic.ToolUnionParam, error) {
	if len(defs) == 0 {
		// A non-nil empty slice would serialize as "tools": [], so send no key.
		return nil, nil
	}
	out := make([]anthropic.ToolUnionParam, 0, len(defs))
	for i, d := range defs {
		schema := map[string]any{"type": "object", "properties": map[string]any{}}
		if len(d.InputSchema) > 0 {
			if err := json.Unmarshal(d.InputSchema, &schema); err != nil {
				return nil, fmt.Errorf("tool %s: invalid input schema: %w", d.Name, err)
			}
		}
		is := anthropic.ToolInputSchemaParam{}
		if p, ok := schema["properties"]; ok {
			is.Properties = p
			delete(schema, "properties")
		}
		if req, ok := schema["required"].([]any); ok {
			for _, r := range req {
				if s, ok := r.(string); ok {
					is.Required = append(is.Required, s)
				}
			}
			delete(schema, "required")
		}
		delete(schema, "type")
		if len(schema) > 0 {
			is.ExtraFields = schema
		}

		tool := anthropic.ToolParam{
			Name:        d.Name,
			Description: anthropic.String(d.Description),
			InputSchema: is,
		}
		if i == len(defs)-1 {
			// The last tool carries the cache breakpoint for the whole tool list.
			tool.CacheControl = anthropic.NewCacheControlEphemeralParam()
		}
		out = append(out, anthropic.ToolUnionParam{OfTool: &tool})
	}
	return out, nil
}

func fromSDKMessage(m anthropic.Message) api.Response {
	resp := api.Response{
		Model:      string(m.Model),
		StopReason: mapStopReason(m.StopReason),
		Usage: api.Usage{
			InputTokens:         m.Usage.InputTokens,
			OutputTokens:        m.Usage.OutputTokens,
			CacheCreationTokens: m.Usage.CacheCreationInputTokens,
			CacheReadTokens:     m.Usage.CacheReadInputTokens,
		},
	}
	for _, cb := range m.Content {
		switch v := cb.AsAny().(type) {
		case anthropic.TextBlock:
			resp.Content = append(resp.Content, api.Block{Type: api.BlockText, Text: v.Text})
		case anthropic.ToolUseBlock:
			input := json.RawMessage(v.Input)
			if len(input) == 0 {
				input = json.RawMessage(`{}`)
			}
			resp.Content = append(resp.Content, api.Block{Type: api.BlockToolUse, ID: v.ID, Name: v.Name, Input: input})
		case anthropic.ThinkingBlock:
			resp.Content = append(resp.Content, api.Block{Type: api.BlockThinking, Text: v.Thinking, Signature: v.Signature})
		}
	}
	return resp
}

func mapStopReason(r anthropic.StopReason) api.StopReason {
	switch r {
	case anthropic.StopReasonEndTurn:
		return api.StopEndTurn
	case anthropic.StopReasonToolUse:
		return api.StopToolUse
	case anthropic.StopReasonMaxTokens:
		return api.StopMaxTokens
	case anthropic.StopReasonRefusal:
		return api.StopRefusal
	}
	return api.StopOther
}

// classify turns SDK errors into typed provider errors.
func classify(err error) error {
	if errors.Is(err, context.Canceled) {
		return err
	}
	var apiErr *anthropic.Error
	if errors.As(err, &apiErr) {
		kind := KindStatus
		switch apiErr.StatusCode {
		case 429:
			kind = KindRateLimit
		case 401, 403:
			kind = KindAuth
		}
		return &Error{Kind: kind, Status: apiErr.StatusCode, Message: apiErr.Error(), Err: err}
	}
	return &Error{Kind: KindConnection, Message: err.Error(), Err: err}
}
