package provider

import (
	"errors"
	"fmt"
	"testing"
)

func TestContextWindowKnownModelsAndDefault(t *testing.T) {
	if got := ContextWindow("claude-haiku-5-5", nil); got != 200_000 {
		t.Fatalf("known model window = %d, want 200000", got)
	}
	if got := ContextWindow("some-unknown-model", nil); got != DefaultContextWindow {
		t.Fatalf("unknown model window = %d, want %d", got, DefaultContextWindow)
	}
	if DefaultContextWindow != 200_000 {
		t.Fatalf("DefaultContextWindow = %d, want 200000", DefaultContextWindow)
	}
}

func TestContextWindowMatchesByPrefix(t *testing.T) {
	overrides := map[string]int{"claude-sonnet": 1_000_000}
	if got := ContextWindow("claude-sonnet-5-5-20261001", overrides); got != 1_000_000 {
		t.Fatalf("prefix override = %d, want 1000000", got)
	}
	if got := ContextWindow("claude-haiku-5-5", overrides); got != 200_000 {
		t.Fatalf("model without a matching override = %d, want 200000", got)
	}
}

func TestContextWindowLongestPrefixWins(t *testing.T) {
	overrides := map[string]int{"claude-sonnet": 1, "claude-sonnet-5-5": 2}
	if got := ContextWindow("claude-sonnet-5-5", overrides); got != 2 {
		t.Fatalf("longest prefix = %d, want 2", got)
	}
}

func TestContextWindowOverrideWinsOverTheTable(t *testing.T) {
	overrides := map[string]int{"claude-haiku-5-5": 128_000}
	if got := ContextWindow("claude-haiku-5-5", overrides); got != 128_000 {
		t.Fatalf("override = %d, want 128000", got)
	}
}

func TestContextPercentAndRounding(t *testing.T) {
	cases := []struct {
		tokens int64
		window int
		want   int
	}{
		{0, 200_000, 0},
		{84_000, 200_000, 42},
		{199_999, 200_000, 99},
		{200_000, 200_000, 100},
		{10, 0, 0},
	}
	for _, c := range cases {
		if got := ContextPercent(c.tokens, c.window); got != c.want {
			t.Errorf("ContextPercent(%d, %d) = %d, want %d", c.tokens, c.window, got, c.want)
		}
	}
}

func TestIsContextOverflowDetectsPromptTooLong(t *testing.T) {
	tooLong := &Error{
		Kind:    KindStatus,
		Status:  400,
		Message: `400 Bad Request {"type":"error","error":{"type":"invalid_request_error","message":"prompt is too long: 201000 tokens > 200000 maximum"}}`,
	}
	if !IsContextOverflow(tooLong) {
		t.Fatal("a 400 prompt-too-long error must be an overflow")
	}
	if !IsContextOverflow(fmt.Errorf("send: %w", tooLong)) {
		t.Fatal("a wrapped overflow error must be an overflow")
	}

	other400 := &Error{Kind: KindStatus, Status: 400, Message: "400 Bad Request: invalid model"}
	if IsContextOverflow(other400) {
		t.Fatal("a 400 for another reason is not an overflow")
	}
	wrongStatus := &Error{Kind: KindStatus, Status: 500, Message: "prompt is too long"}
	if IsContextOverflow(wrongStatus) {
		t.Fatal("only a 400 is an overflow")
	}
	if IsContextOverflow(errors.New("prompt is too long")) {
		t.Fatal("a plain error without the provider type is not an overflow")
	}
	if IsContextOverflow(nil) {
		t.Fatal("nil is not an overflow")
	}
}
