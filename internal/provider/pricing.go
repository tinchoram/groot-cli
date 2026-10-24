package provider

import "github.com/tinchoram/groot-cli/internal/api"

// Price is USD per million tokens.
type Price struct {
	Input  float64
	Output float64
}

// prices holds the list prices used for the session cost estimate. Cache
// writes cost 1.25x the input price and cache reads cost 0.1x.
var prices = map[string]Price{
	"claude-opus-5-5":   {Input: 4, Output: 20},
	"claude-sonnet-5-5": {Input: 2, Output: 10},
	"claude-haiku-5-5":  {Input: 0.10, Output: 0.50},
}

const (
	cacheWriteMultiplier = 1.25
	cacheReadMultiplier  = 0.1
	tokensPerMillion     = 1_000_000.0
)

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
		float64(u.CacheReadTokens)*p.Input*cacheReadMultiplier
	return cost / tokensPerMillion, true
}
