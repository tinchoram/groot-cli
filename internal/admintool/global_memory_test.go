package admintool

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/tinchoram/groot-cli/internal/tool"
)

// globalMemoryManager records the global memory switches made through the tools.
type globalMemoryManager struct {
	fakeManager
	switches []string
	err      error
}

func (g *globalMemoryManager) SetAgentGlobalMemory(name string, on bool) error {
	g.switches = append(g.switches, fmt.Sprintf("%s=%v", name, on))
	return g.err
}

func TestGrantGlobalMemoryEnablesTheAgent(t *testing.T) {
	g := &globalMemoryManager{}
	out, isErr := NewGrantGlobalMemoryTool(g).Execute(context.Background(), `{"agent":"developer"}`)
	if isErr || !strings.Contains(out, "global memory enabled for developer") {
		t.Fatalf("got (%q, %v)", out, isErr)
	}
	if len(g.switches) != 1 || g.switches[0] != "developer=true" {
		t.Fatalf("switches = %v", g.switches)
	}
	if NewGrantGlobalMemoryTool(g).Risk() != tool.RiskWrite {
		t.Fatal("grant_global_memory is a write")
	}
}

func TestRevokeGlobalMemoryDisablesTheAgent(t *testing.T) {
	g := &globalMemoryManager{}
	out, isErr := NewRevokeGlobalMemoryTool(g).Execute(context.Background(), `{"agent":"developer"}`)
	if isErr || !strings.Contains(out, "global memory disabled for developer") {
		t.Fatalf("got (%q, %v)", out, isErr)
	}
	if len(g.switches) != 1 || g.switches[0] != "developer=false" {
		t.Fatalf("switches = %v", g.switches)
	}
}

func TestGlobalMemoryToolsNeedAnAgentAndReportRefusals(t *testing.T) {
	if _, isErr := NewGrantGlobalMemoryTool(&globalMemoryManager{}).Execute(context.Background(), `{}`); !isErr {
		t.Fatal("a grant without an agent is refused")
	}
	g := &globalMemoryManager{err: errString("groot always reads global memory")}
	out, isErr := NewRevokeGlobalMemoryTool(g).Execute(context.Background(), `{"agent":"groot"}`)
	if !isErr || !strings.Contains(out, "groot") {
		t.Fatalf("got (%q, %v)", out, isErr)
	}
}

// listAgentsManager answers ListAgents with a fixed catalog.
type listAgentsManager struct {
	fakeManager
}

func (*listAgentsManager) ListAgents() []AgentInfo {
	return []AgentInfo{
		{Name: "payments", Description: "pays", Model: "m", Tools: []string{"recall"}, GlobalMemory: true},
		{Name: "plain", Description: "plain", Model: "m", Tools: []string{"recall"}},
	}
}

func TestListAgentsShowsGlobalMemoryForEnabledAgents(t *testing.T) {
	out, isErr := NewListAgents(&listAgentsManager{}).Execute(context.Background(), `{}`)
	if isErr {
		t.Fatalf("list_agents failed: %s", out)
	}
	var payments, plain string
	for _, line := range strings.Split(out, "\n") {
		switch {
		case strings.HasPrefix(line, "payments "):
			payments = line
		case strings.HasPrefix(line, "plain "):
			plain = line
		}
	}
	if !strings.Contains(payments, "global memory: yes") {
		t.Fatalf("enabled agent line = %q", payments)
	}
	if strings.Contains(plain, "global memory") {
		t.Fatalf("plain agent line must not mention global memory: %q", plain)
	}
}
