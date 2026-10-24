package workspace

import (
	"context"
	"strings"
	"testing"
)

func TestCostReportListsSpendAndBudgets(t *testing.T) {
	f := newFixture(t, text("ok"))
	if err := f.ws.SetAgentBudget("reader", 2); err != nil {
		t.Fatal(err)
	}
	seedSpend(t, f, "reader", 0.3)

	out, err := f.ws.CostReport(context.Background(), "")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"default", "reader", "$0.30", "$2.00"} {
		if !strings.Contains(out, want) {
			t.Fatalf("cost report lacks %q:\n%s", want, out)
		}
	}
}

func TestCostReportNamesAnUnknownSpace(t *testing.T) {
	f := newFixture(t, text("ok"))
	if _, err := f.ws.CostReport(context.Background(), "missing"); err == nil || !strings.Contains(err.Error(), "missing") {
		t.Fatalf("unknown space: err = %v", err)
	}
}
