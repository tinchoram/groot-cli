// Package api defines the provider-neutral conversation model used by the
// agent loop. Nothing in this package depends on a vendor SDK.
package api

import (
	"encoding/json"
	"fmt"
	"strings"
)

// Role identifies who authored a message.
type Role string

const (
	RoleUser      Role = "user"
	RoleAssistant Role = "assistant"
)

// BlockType discriminates the payload of a Block.
type BlockType string

const (
	BlockText       BlockType = "text"
	BlockToolUse    BlockType = "tool_use"
	BlockToolResult BlockType = "tool_result"
	// BlockThinking carries model reasoning. Providers that use tools with
	// thinking require these blocks to be sent back unmodified, so the agent
	// keeps them in history.
	BlockThinking BlockType = "thinking"
)

// Block is one content element of a message.
type Block struct {
	Type BlockType `json:"type"`

	// Text is set for BlockText.
	Text string `json:"text,omitempty"`

	// ID, Name and Input are set for BlockToolUse.
	ID    string          `json:"id,omitempty"`
	Name  string          `json:"name,omitempty"`
	Input json.RawMessage `json:"input,omitempty"`

	// ToolUseID, Content and IsError are set for BlockToolResult.
	ToolUseID string `json:"tool_use_id,omitempty"`
	Content   string `json:"content,omitempty"`
	IsError   bool   `json:"is_error,omitempty"`

	// Signature is set for BlockThinking (Text holds the reasoning text).
	Signature string `json:"signature,omitempty"`
}

// Message is one turn in the conversation.
type Message struct {
	Role    Role    `json:"role"`
	Content []Block `json:"content"`
}

// ToolDef describes a tool to the model. InputSchema is a JSON Schema object.
type ToolDef struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	InputSchema json.RawMessage `json:"input_schema"`
}

// StopReason explains why the model stopped generating.
type StopReason string

const (
	StopEndTurn   StopReason = "end_turn"
	StopToolUse   StopReason = "tool_use"
	StopMaxTokens StopReason = "max_tokens"
	StopRefusal   StopReason = "refusal"
	StopOther     StopReason = "other"
)

// Usage counts tokens for one or more model calls.
type Usage struct {
	InputTokens         int64 `json:"input_tokens"`
	OutputTokens        int64 `json:"output_tokens"`
	CacheCreationTokens int64 `json:"cache_creation_tokens"`
	CacheReadTokens     int64 `json:"cache_read_tokens"`
}

// Add accumulates other into u.
func (u *Usage) Add(other Usage) {
	u.InputTokens += other.InputTokens
	u.OutputTokens += other.OutputTokens
	u.CacheCreationTokens += other.CacheCreationTokens
	u.CacheReadTokens += other.CacheReadTokens
}

// Response is one model answer.
type Response struct {
	Content    []Block    `json:"content"`
	StopReason StopReason `json:"stop_reason"`
	Usage      Usage      `json:"usage"`
	Model      string     `json:"model"`
}

// ToolUses returns the tool_use blocks of a response in order.
func (r Response) ToolUses() []Block {
	var out []Block
	for _, b := range r.Content {
		if b.Type == BlockToolUse {
			out = append(out, b)
		}
	}
	return out
}

// Text concatenates the text blocks of a response.
func (r Response) Text() string {
	var sb strings.Builder
	for _, b := range r.Content {
		if b.Type == BlockText {
			sb.WriteString(b.Text)
		}
	}
	return sb.String()
}

// RenderTranscript formats messages as plain text, for debugging and logs.
func RenderTranscript(messages []Message) string {
	var sb strings.Builder
	for _, m := range messages {
		fmt.Fprintf(&sb, "[%s]\n", m.Role)
		for _, b := range m.Content {
			switch b.Type {
			case BlockText:
				fmt.Fprintf(&sb, "%s\n", b.Text)
			case BlockToolUse:
				fmt.Fprintf(&sb, "tool_use %s %s\n", b.Name, string(b.Input))
			case BlockToolResult:
				tag := "tool_result"
				if b.IsError {
					tag = "tool_result(error)"
				}
				fmt.Fprintf(&sb, "%s %s: %s\n", tag, b.ToolUseID, b.Content)
			}
		}
	}
	return sb.String()
}
