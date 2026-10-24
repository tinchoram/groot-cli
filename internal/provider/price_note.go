package provider

import "fmt"

// PriceNote is the list price of a model per million tokens, or "price unknown"
// when the model is not in the price table.
func PriceNote(model string) string {
	p, ok := PriceOf(model)
	if !ok {
		return "price unknown"
	}
	return fmt.Sprintf("$%.2f in / $%.2f out per 1M", p.Input, p.Output)
}
