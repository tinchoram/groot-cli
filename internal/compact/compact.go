// Package compact shortens conversation history before it is sent to the model.
package compact

import (
	"context"
	"fmt"

	"github.com/tinchoram/groot-cli/internal/api"
	"github.com/tinchoram/groot-cli/internal/provider"
)

// Strategy rewrites a history to fit the context budget.
type Strategy interface {
	Compact(ctx context.Context, msgs []api.Message) ([]api.Message, error)
}

// None keeps the whole history.
type None struct{}

func (None) Compact(_ context.Context, msgs []api.Message) ([]api.Message, error) {
	return msgs, nil
}

// SlidingWindow keeps only the last Keep messages, moved to a safe split point.
type SlidingWindow struct {
	Keep int
}

func (s SlidingWindow) Compact(_ context.Context, msgs []api.Message) ([]api.Message, error) {
	if s.Keep >= len(msgs) {
		return msgs, nil
	}
	k := SafeSplitPoint(msgs, len(msgs)-s.Keep)
	if k == 0 {
		return msgs, nil
	}
	return msgs[k:], nil
}

// Summarize replaces the oldest part of a long history with a model-written
// summary. It triggers when the history exceeds Threshold messages and keeps
// roughly the newest Threshold/2 messages verbatim.
type Summarize struct {
	Provider  provider.Provider
	Threshold int
}

const summarizeSystem = "You summarize operational conversations between an SRE and an AI assistant. " +
	"Keep facts, resource names, decisions, open problems and commands that were run. Be concise."

func (s Summarize) Compact(ctx context.Context, msgs []api.Message) ([]api.Message, error) {
	if s.Threshold <= 0 || len(msgs) <= s.Threshold {
		return msgs, nil
	}
	tail := s.Threshold / 2
	if tail < 1 {
		tail = 1
	}
	k := SafeSplitPoint(msgs, len(msgs)-tail)
	if k == 0 {
		return msgs, nil
	}

	prompt := "Summarize this earlier part of the conversation in at most 12 bullet points:\n\n" +
		api.RenderTranscript(msgs[:k])
	resp, err := s.Provider.Send(ctx, provider.Request{
		System: summarizeSystem,
		Messages: []api.Message{{
			Role:    api.RoleUser,
			Content: []api.Block{{Type: api.BlockText, Text: prompt}},
		}},
	})
	if err != nil {
		return nil, fmt.Errorf("summarize history: %w", err)
	}

	out := []api.Message{
		{Role: api.RoleUser, Content: []api.Block{{Type: api.BlockText, Text: "Summary of earlier conversation:\n" + resp.Text()}}},
		{Role: api.RoleAssistant, Content: []api.Block{{Type: api.BlockText, Text: "Understood. I will use that context."}}},
	}
	return append(out, msgs[k:]...), nil
}

// SafeSplitPoint returns the largest index k in [1, desired] at which msgs can
// be cut so that msgs[k:] starts with a plain-text user message. Such a cut
// never separates a tool_use from its tool_result. It returns 0 when no safe
// point exists.
func SafeSplitPoint(msgs []api.Message, desired int) int {
	if desired >= len(msgs) {
		desired = len(msgs) - 1
	}
	for k := desired; k >= 1; k-- {
		if isPlainUserText(msgs[k]) {
			return k
		}
	}
	return 0
}

func isPlainUserText(m api.Message) bool {
	if m.Role != api.RoleUser || len(m.Content) == 0 {
		return false
	}
	for _, b := range m.Content {
		if b.Type != api.BlockText {
			return false
		}
	}
	return true
}
