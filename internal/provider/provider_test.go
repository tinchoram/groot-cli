package provider

import (
	"context"
	"errors"
	"math"
	"strings"
	"testing"

	"github.com/tinchoram/groot-cli/internal/api"
)

func approx(a, b float64) bool { return math.Abs(a-b) < 1e-9 }

func TestCostUSDFromPricingTable(t *testing.T) {
	const million = 1_000_000
	cases := []struct {
		name  string
		model string
		usage api.Usage
		want  float64
	}{
		{"sonnet input and output", "claude-sonnet-5-5", api.Usage{InputTokens: million, OutputTokens: million}, 2 + 10},
		{"opus input and output", "claude-opus-5-5", api.Usage{InputTokens: million, OutputTokens: million}, 4 + 20},
		{"haiku input and output", "claude-haiku-5-5", api.Usage{InputTokens: million, OutputTokens: million}, 0.10 + 0.50},
		{"cache write is 1.25x input", "claude-sonnet-5-5", api.Usage{CacheCreationTokens: million}, 2 * 1.25},
		{"cache read is 0.1x input", "claude-sonnet-5-5", api.Usage{CacheReadTokens: million}, 2 * 0.1},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, ok := CostUSD(c.model, c.usage)
			if !ok {
				t.Fatalf("model %s should be priced", c.model)
			}
			if !approx(got, c.want) {
				t.Fatalf("cost = %v want %v", got, c.want)
			}
		})
	}
}

func TestCostUSDUnknownModel(t *testing.T) {
	if _, ok := CostUSD("gpt-unknown", api.Usage{InputTokens: 10}); ok {
		t.Fatal("unknown model must report not priced")
	}
}

func TestMockReturnsScriptedResponsesAndStreams(t *testing.T) {
	m := NewMock(
		Step{Response: api.Response{Content: []api.Block{{Type: api.BlockText, Text: "hello there"}}, StopReason: api.StopEndTurn, Usage: api.Usage{InputTokens: 3, OutputTokens: 2}}},
	)
	var streamed []string
	resp, err := m.Send(context.Background(), Request{
		System:   "sys",
		Messages: []api.Message{{Role: api.RoleUser, Content: []api.Block{{Type: api.BlockText, Text: "hi"}}}},
		OnText:   func(d string) { streamed = append(streamed, d) },
	})
	if err != nil {
		t.Fatal(err)
	}
	if resp.Text() != "hello there" {
		t.Fatalf("got %q", resp.Text())
	}
	if strings.Join(streamed, "") != "hello there" {
		t.Fatalf("OnText should receive the text, got %v", streamed)
	}
	if len(m.Calls) != 1 || m.Calls[0].System != "sys" {
		t.Fatalf("request not recorded: %+v", m.Calls)
	}
	if m.Usage().InputTokens != 3 {
		t.Fatalf("usage not accumulated: %+v", m.Usage())
	}
}

func TestMockErrorsWhenScriptIsExhausted(t *testing.T) {
	m := NewMock()
	if _, err := m.Send(context.Background(), Request{}); err == nil {
		t.Fatal("exhausted script must return an error")
	}
}

func TestMockScriptedError(t *testing.T) {
	m := NewMock(Step{Err: errors.New("boom")})
	if _, err := m.Send(context.Background(), Request{}); err == nil || err.Error() != "boom" {
		t.Fatalf("scripted error not returned: %v", err)
	}
}

func TestMockModelAndSetModel(t *testing.T) {
	m := NewMock()
	m.SetModel("claude-haiku-5-5")
	if m.Model() != "claude-haiku-5-5" {
		t.Fatalf("model = %s", m.Model())
	}
}

func TestErrorClassification(t *testing.T) {
	rl := &Error{Kind: KindRateLimit, Status: 429, Message: "slow down"}
	if !IsRateLimit(rl) {
		t.Fatal("429 must classify as rate limit")
	}
	wrapped := errors.Join(errors.New("ctx"), rl)
	if !IsRateLimit(wrapped) {
		t.Fatal("classification must survive wrapping")
	}
	if IsRateLimit(errors.New("plain")) {
		t.Fatal("plain errors are not rate limits")
	}
	if rl.Error() == "" || !strings.Contains(rl.Error(), "429") {
		t.Fatalf("error text should carry status: %q", rl.Error())
	}
}
