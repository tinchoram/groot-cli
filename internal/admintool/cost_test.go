package admintool

import (
	"context"
	"strings"
	"testing"

	"github.com/tinchoram/groot-cli/internal/tool"
)

// CostReport is the fake's default; costManager overrides it.
func (f *fakeManager) CostReport(context.Context, string) (string, error) { return "", nil }

type costManager struct {
	fakeManager
	report string
	asked  []string
}

func (m *costManager) CostReport(_ context.Context, space string) (string, error) {
	m.asked = append(m.asked, space)
	return m.report, nil
}

func TestGetCostReturnsTheSpendOfTheNamedSpace(t *testing.T) {
	m := &costManager{report: "space default: $0.50 of $2.00"}
	out, isErr := NewGetCost(m).Execute(context.Background(), `{"space":"default"}`)
	if isErr || !strings.Contains(out, "$0.50") {
		t.Fatalf("get_cost = %q, error=%v", out, isErr)
	}
	if len(m.asked) != 1 || m.asked[0] != "default" {
		t.Fatalf("manager asked for %v, want the named space", m.asked)
	}
}

func TestGetCostIsReadOnly(t *testing.T) {
	if r := NewGetCost(&costManager{}).Risk(); r != tool.RiskRead {
		t.Fatalf("get_cost risk = %v, want read", r)
	}
}
