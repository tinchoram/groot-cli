package provider

import "github.com/tinchoram/groot-cli/internal/api"

// Price is USD per million tokens.
type Price struct {
	Input  float64
	Output float64
	// CacheRead is the cache-read price as a multiplier of Input. Zero means
	// defaultCacheReadMultiplier (Anthropic's 0.1).
	CacheRead float64
}

// prices holds the list prices used for the session cost estimate. Cache
// writes cost 1.25x the input price. Cache reads are per row: Anthropic reads
// cost 0.1x, Gemini 4 Argon 0.05x and Gemini 3.8 Flash 0.1x.
//
// Gemini rows checked 2026-10-28 against https://ai.google.dev/gemini-api/docs/pricing.
// Gemini 4 Argon is introductory until 2026-11-30; from 2026-12-01 it is
// 4.00 input / 20.00 output (cache read 0.20). Gemini 3.8 Flash is introductory
// until 2026-12-31; from 2027-01-01 it is 1.50 input / 7.50 output (cache read
// 0.15). Re-check both before relying on them after those dates.
var prices = map[string]Price{
	"claude-opus-5-5":   {Input: 4, Output: 20},
	"claude-sonnet-5-5": {Input: 2, Output: 10},
	"claude-haiku-5-5":  {Input: 0.10, Output: 0.50},
	"gemini-4-argon":    {Input: 2.00, Output: 10.00, CacheRead: 0.05},
	"gemini-3.8-flash":  {Input: 0.75, Output: 3.75, CacheRead: 0.10},
}

const (
	cacheWriteMultiplier       = 1.25
	defaultCacheReadMultiplier = 0.1
	tokensPerMillion           = 1_000_000.0
)

func (p Price) cacheReadMultiplier() float64 {
	if p.CacheRead > 0 {
		return p.CacheRead
	}
	return defaultCacheReadMultiplier
}

// CostUSD estimates the cost of usage on model. The boolean is false when the
// model has no price in the table, in which case the cost is zero.
func CostUSD(model string, u api.Usage) (float64, bool) {
	p, ok := prices[model]
	if !ok {
		return 0, false
	}
	cost := float64(u.InputTokens)*p.Input +
		float64(u.OutputTokens)*p.Output +
		float64(u.CacheCreationTokens)*p.Input*cacheWriteMultiplier +
		float64(u.CacheReadTokens)*p.Input*p.cacheReadMultiplier()
	return cost / tokensPerMillion, true
}
