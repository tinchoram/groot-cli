package provider

import (
	"math"
	"testing"

	"github.com/tinchoram/groot-cli/internal/api"
)

// TestCachedReadMultiplierIsPerModel pins the cached-read discount per row:
// Gemini 4 Argon reads cache at 5% of input, Gemini 3.8 Flash at 10%, and
// Anthropic rows keep 10%.
func TestCachedReadMultiplierIsPerModel(t *testing.T) {
	cases := map[string]float64{
		"gemini-4-argon":    0.05,
		"gemini-3.8-flash":  0.10,
		"claude-sonnet-5-5": 0.10,
	}
	for model, mult := range cases {
		p, ok := PriceOf(model)
		if !ok {
			t.Fatalf("%s has no price row", model)
		}
		u := api.Usage{CacheReadTokens: 1_000_000}
		got, ok := CostUSD(model, u)
		if !ok {
			t.Fatalf("CostUSD(%s) not priced", model)
		}
		want := p.Input * mult
		if math.Abs(got-want) > 1e-9 {
			t.Fatalf("%s: 1M cached tokens cost %v, want %v (input %v x %v)", model, got, want, p.Input, mult)
		}
	}
}

func TestGeminiCostNonZeroWithCachedTokens(t *testing.T) {
	got, ok := CostUSD("gemini-4-argon", api.Usage{InputTokens: 100, CacheReadTokens: 1000})
	if !ok || got <= 0 {
		t.Fatalf("CostUSD = %v, %v; want a positive priced cost", got, ok)
	}
}
