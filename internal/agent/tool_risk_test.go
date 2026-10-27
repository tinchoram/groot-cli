package agent

import (
	"fmt"
	"testing"

	"github.com/tinchoram/groot-cli/internal/gate"
	"github.com/tinchoram/groot-cli/internal/provider"
	"github.com/tinchoram/groot-cli/internal/tool"
)

func TestOnToolRiskReportsWhetherTheCallIsReadOnly(t *testing.T) {
	cases := []struct {
		risk tool.Risk
		want string
	}{
		{tool.RiskRead, "echo|true"},
		{tool.RiskWrite, "echo|false"},
	}
	for _, c := range cases {
		mock := provider.NewMock(toolUseResponse("tu_1", "echo", `{}`), textResponse("ok"))
		a := newAgent(mock, registry(t, &echoTool{risk: c.risk}), gate.AutoApprove{})
		var got []string
		a.OnToolRisk = func(name string, readOnly bool) { got = append(got, fmt.Sprintf("%s|%v", name, readOnly)) }
		if _, err := a.Send(t.Context(), "go"); err != nil {
			t.Fatalf("Send: %v", err)
		}
		if len(got) != 1 || got[0] != c.want {
			t.Fatalf("risk %v: OnToolRisk got %v, want [%s]", c.risk, got, c.want)
		}
	}
}

// A denied call never ran, so it is not activity: the hook stays silent.
func TestOnToolRiskIsSilentForADeniedCall(t *testing.T) {
	mock := provider.NewMock(toolUseResponse("tu_1", "echo", `{}`), textResponse("ok"))
	a := newAgent(mock, registry(t, &echoTool{risk: tool.RiskWrite}), gate.ReadOnly{})
	called := false
	a.OnToolRisk = func(string, bool) { called = true }
	if _, err := a.Send(t.Context(), "go"); err != nil {
		t.Fatalf("Send: %v", err)
	}
	if called {
		t.Fatal("OnToolRisk fired for a call the gate denied")
	}
}
