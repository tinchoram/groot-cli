package workspace

import "fmt"

// FormatUSD renders an amount for people: cents when the amount is at least
// one cent, four decimals below that, and a floor marker for amounts that
// would otherwise round to nothing. Cheap models spend fractions of a cent per
// turn, and "$0.00 of $0.00" hides what happened.
func FormatUSD(usd float64) string {
	switch {
	case usd <= 0:
		return "$0.00"
	case usd >= 0.01:
		return fmt.Sprintf("$%.2f", usd)
	case usd < 0.0001:
		return "$<0.0001"
	default:
		return fmt.Sprintf("$%.4f", usd)
	}
}
