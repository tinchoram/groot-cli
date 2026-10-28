package gate

import (
	"context"
	"strings"
	"testing"

	"github.com/tinchoram/groot-cli/internal/tool"
)

var defaultAsk = []string{
	`\brm\s+-[a-zA-Z]*r[a-zA-Z]*\s+(/|~|\.\.)`,
	`(^|[\s;&|])sudo\b`,
	`\bgit\s+push\b.*(--force\b|\s-f\b)`,
	`\bgit\s+reset\s+--hard\b`,
	`\bgit\s+clean\b`,
	`\bkubectl\s+(apply|delete|patch|edit|scale|rollout)\b`,
	`\bdocker\s+(rm|rmi)\b|\bdocker\s+system\s+prune\b`,
	`\bterraform\s+(apply|destroy)\b`,
	`\b(curl|wget)\b.*(\s-o\b|\s-O\b|--output\b)`,
}

// asker records what the user's gate was asked.
type asker struct{ calls int }

func (a *asker) Decide(context.Context, Request) Decision {
	a.calls++
	return Deny
}

func newWork(t *testing.T, on bool, cost func() float64, maxTurns int, maxUSD float64) (*WorkState, *[]string) {
	t.Helper()
	var warnings []string
	s, err := NewWorkState(WorkConfig{
		Space: "/work/payments", Ask: defaultAsk, MaxTurns: maxTurns, MaxUSD: maxUSD,
		Cost: cost, Warn: func(msg string) { warnings = append(warnings, msg) },
	})
	if err != nil {
		t.Fatal(err)
	}
	s.Set(on)
	return s, &warnings
}

func TestWorkApprovesInsideTheSpaceAndAsksOutside(t *testing.T) {
	s, _ := newWork(t, true, nil, 0, 0)
	user := &asker{}
	g := Work{Inner: user, State: s}
	ctx := context.Background()

	inside := Request{Tool: "write_file", Input: `{"path":"internal/x.go"}`, Risk: tool.RiskWrite}
	outside := Request{Tool: "write_file", Input: `{"path":"../other/x.go"}`, Risk: tool.RiskWrite}
	if g.Decide(ctx, inside) != Allow {
		t.Fatal("a write inside the space is approved in work mode")
	}
	if g.Decide(ctx, outside) != Deny || user.calls != 1 {
		t.Fatal("a write outside the space goes to the user")
	}
}

func TestWorkAlwaysAskOverridesApproval(t *testing.T) {
	s, _ := newWork(t, true, nil, 0, 0)
	user := &asker{}
	g := Work{Inner: user, State: s}
	ctx := context.Background()

	tests := []struct {
		command string
		asks    bool
	}{
		{"go test ./...", false},
		{"rm -rf build", false},
		{"rm -rf /tmp/cache", true},
		{"rm -rf ~/work", true},
		{"sudo systemctl restart x", true},
		{"git push --force origin main", true},
		{"git push -f", true},
		{"git push origin main", false},
		{"git reset --hard HEAD~1", true},
		{"git clean -fd", true},
		{"kubectl get pods -n prod", false},
		{"kubectl delete pod x", true},
		{"kubectl apply -f a.yaml", true},
		{"docker rm web", true},
		{"docker ps", false},
		{"terraform destroy", true},
		{"terraform plan", false},
		{"curl -o out.bin https://x", true},
		{"curl https://x", false},
		{"wget -O x https://x", true},
	}
	for _, tt := range tests {
		t.Run(tt.command, func(t *testing.T) {
			before := user.calls
			got := g.Decide(ctx, Request{Tool: "bash", Input: `{"command":"` + tt.command + `"}`, Risk: tool.RiskExec})
			asked := user.calls > before
			if asked != tt.asks {
				t.Fatalf("asked the user = %v, want %v (decision %v)", asked, tt.asks, got)
			}
		})
	}
}

func TestWorkLeavesOtherCallsToTheUser(t *testing.T) {
	s, _ := newWork(t, true, nil, 0, 0)
	user := &asker{}
	g := Work{Inner: user, State: s}
	ctx := context.Background()
	for _, name := range []string{"send_message", "feedback_report", "create_space", "install_agent"} {
		g.Decide(ctx, Request{Tool: name, Risk: tool.RiskWrite})
	}
	if user.calls != 2 {
		t.Fatalf("user consulted %d times, want 2 (the admin tools only)", user.calls)
	}
	if g.Decide(ctx, Request{Tool: "task_open", Risk: tool.RiskWrite}) != Allow {
		t.Fatal("task_open is part of the flow and is approved in work mode")
	}
}

func TestWorkOffAsksForEverything(t *testing.T) {
	s, _ := newWork(t, false, nil, 0, 0)
	user := &asker{}
	g := Work{Inner: user, State: s}
	g.Decide(context.Background(), Request{Tool: "write_file", Input: `{"path":"a"}`, Risk: tool.RiskWrite})
	if user.calls != 1 {
		t.Fatal("with work mode off the user decides")
	}
}

func TestWorkTurnLimitTurnsItOffWithAWarning(t *testing.T) {
	s, warnings := newWork(t, true, nil, 2, 0)
	user := &asker{}
	g := Work{Inner: user, State: s}
	ctx := context.Background()
	w := Request{Tool: "write_file", Input: `{"path":"a"}`, Risk: tool.RiskWrite}
	g.Decide(ctx, w)
	g.Decide(ctx, w)
	if g.Decide(ctx, w); s.On() {
		t.Fatal("work mode must turn off at the turn limit")
	}
	if len(*warnings) != 1 || !strings.Contains((*warnings)[0], "limit reached (turns)") || !strings.HasSuffix((*warnings)[0], "/work on turns it back on") {
		t.Fatalf("warnings = %v", *warnings)
	}
	if user.calls != 1 {
		t.Fatal("the call that hit the limit goes to the user")
	}
}

func TestWorkCostLimitTurnsItOff(t *testing.T) {
	spent := 0.0
	s, warnings := newWork(t, true, func() float64 { return spent }, 0, 5)
	user := &asker{}
	g := Work{Inner: user, State: s}
	ctx := context.Background()
	call := Request{Tool: "write_file", Input: `{"path":"a"}`, Risk: tool.RiskWrite}
	if g.Decide(ctx, call) != Allow {
		t.Fatal("under the budget work mode approves")
	}
	spent = 5.2
	g.Decide(ctx, call)
	if s.On() || len(*warnings) != 1 || !strings.Contains((*warnings)[0], "limit reached (budget)") {
		t.Fatalf("over the budget work mode must turn off: on=%v warnings=%v", s.On(), *warnings)
	}
}

func TestResetTurnsCountsAgainForANewTask(t *testing.T) {
	s, _ := newWork(t, true, nil, 1, 0)
	g := Work{Inner: &asker{}, State: s}
	g.Decide(context.Background(), Request{Tool: "write_file", Input: `{"path":"a"}`, Risk: tool.RiskWrite})
	s.Set(true)
	s.ResetTurns()
	if g.Decide(context.Background(), Request{Tool: "write_file", Input: `{"path":"b"}`, Risk: tool.RiskWrite}) != Allow || !s.On() {
		t.Fatal("a new task starts with the full turn budget")
	}
}

func TestInvalidAlwaysAskPatternIsRefused(t *testing.T) {
	if _, err := NewWorkState(WorkConfig{Space: "/w", Ask: []string{"("}}); err == nil {
		t.Fatal("a broken pattern must be reported")
	}
}

func TestWorkNeverAutoApprovesAWriteOfAnExternalTool(t *testing.T) {
	s, _ := newWork(t, true, nil, 0, 0)
	user := &asker{}
	g := Work{Inner: user, State: s}
	ctx := context.Background()

	write := Request{Tool: "grafana__set_alert", Input: `{}`, Risk: tool.RiskWrite, External: true}
	if g.Decide(ctx, write) != Deny || user.calls != 1 {
		t.Fatal("an external write must go to the user even in work mode")
	}
}

func TestWorkApprovesAReadOnlyExternalTool(t *testing.T) {
	s, _ := newWork(t, true, nil, 0, 0)
	user := &asker{}
	g := Work{Inner: user, State: s}

	read := Request{Tool: "grafana__echo", Input: `{}`, Risk: tool.RiskRead, External: true}
	if g.Decide(context.Background(), read) != Allow || user.calls != 0 {
		t.Fatal("a read_only external tool is approved in work mode like any read")
	}
	if !s.Would(read) {
		t.Fatal("the transcript must label the read as auto-approved")
	}
	write := Request{Tool: "grafana__set_alert", Input: `{}`, Risk: tool.RiskWrite, External: true}
	if s.Would(write) {
		t.Fatal("the transcript must not label an external write as auto-approved")
	}
}
