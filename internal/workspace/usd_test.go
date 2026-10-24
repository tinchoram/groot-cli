package workspace

import "testing"

func TestFormatUSDKeepsSubCentAmountsVisible(t *testing.T) {
	cases := []struct {
		in   float64
		want string
	}{
		{0, "$0.00"},
		{2, "$2.00"},
		{0.9, "$0.90"},
		{0.01, "$0.01"},
		{0.000046, "$<0.0001"},
		{0.0046, "$0.0046"},
		{0.00123, "$0.0012"},
	}
	for _, c := range cases {
		if got := FormatUSD(c.in); got != c.want {
			t.Errorf("FormatUSD(%v) = %q, want %q", c.in, got, c.want)
		}
	}
}
