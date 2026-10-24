package provider

import "github.com/tinchoram/groot-cli/internal/api"

// tracker accumulates token usage and estimated cost across calls. Callers
// hold their own lock around add.
type tracker struct {
	total api.Usage
	cost  float64
}

// add records u. model selects the price; an empty or unknown model adds
// tokens but no cost.
func (t *tracker) add(u api.Usage, model string) {
	t.total.Add(u)
	if c, ok := CostUSD(model, u); ok {
		t.cost += c
	}
}
