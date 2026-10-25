package ui

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/tinchoram/groot-cli/internal/workspace"
)

// MCPServers answers the fake's MCP servers.
func (f *fakeWorkspace) MCPServers(context.Context) ([]workspace.MCPServerInfo, error) {
	if f.mcpErr != nil {
		return nil, f.mcpErr
	}
	return f.mcpServers, nil
}

// MCPConnect records the connect and answers the fake's outcome for the name.
func (f *fakeWorkspace) MCPConnect(_ context.Context, name string) (int, error) {
	f.mcpConnects = append(f.mcpConnects, name)
	if err := f.mcpConnectErr[name]; err != nil {
		return 0, err
	}
	return f.mcpConnectTools, nil
}

func TestMCPCommandWithNoServersSaysHowToAddThem(t *testing.T) {
	m, _ := newTestModel(t, &fakeSession{model: "m"})
	m = sendLine(t, m, "/mcp")
	want := "no MCP servers configured: add mcp.servers to ~/.groot/groot.yaml"
	if out := stripANSI(m.transcriptText()); !strings.Contains(out, want) {
		t.Fatalf("/mcp lacks %q:\n%s", want, out)
	}
}

func TestMCPCommandListsConnectedServerToolsAndHolders(t *testing.T) {
	m, ws := newTestModel(t, &fakeSession{model: "m"})
	ws.mcpServers = []workspace.MCPServerInfo{{
		Name: "grafana", Transport: "http", Connected: true,
		Tools: []workspace.MCPToolInfo{
			{Name: "grafana__echo", ReadOnly: true, Agents: []string{"reader"}},
			{Name: "grafana__chart", ReadOnly: false},
		},
	}}
	m = sendLine(t, m, "/mcp")
	out := stripANSI(m.transcriptText())
	for _, want := range []string{
		"grafana · http · connected · 2 tools",
		"grafana__echo",
		"ro",
		"held by: reader",
		"grafana__chart",
		"rw",
		"held by: none",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("/mcp lacks %q:\n%s", want, out)
		}
	}
}

func TestMCPCommandReportsFailedAndIdleServers(t *testing.T) {
	m, ws := newTestModel(t, &fakeSession{model: "m"})
	ws.mcpServers = []workspace.MCPServerInfo{
		{Name: "grafana", Transport: "http", Err: "mcp server grafana: connection refused", Missing: []string{"drop_table"}},
		{Name: "k8s", Transport: "stdio"},
	}
	m = sendLine(t, m, "/mcp")
	out := stripANSI(m.transcriptText())
	for _, want := range []string{
		"grafana · http · failed: mcp server grafana: connection refused · 0 tools · missing: drop_table",
		"k8s · stdio · not connected yet · 0 tools",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("/mcp lacks %q:\n%s", want, out)
		}
	}
}

func TestMCPConnectForcesAConnectAndReports(t *testing.T) {
	m, ws := newTestModel(t, &fakeSession{model: "m"})
	ws.mcpConnectTools = 2
	m = sendLine(t, m, "/mcp connect grafana")
	if len(ws.mcpConnects) != 1 || ws.mcpConnects[0] != "grafana" {
		t.Fatalf("connects = %v, want [grafana]", ws.mcpConnects)
	}
	if out := stripANSI(m.transcriptText()); !strings.Contains(out, "mcp grafana: connected · 2 tools") {
		t.Fatalf("/mcp connect did not report:\n%s", out)
	}

	ws.mcpConnectErr = map[string]error{"grafana": errors.New("connection refused")}
	m = sendLine(t, m, "/mcp connect grafana")
	if out := stripANSI(m.transcriptText()); !strings.Contains(out, "mcp grafana: connection refused") {
		t.Fatalf("a failed connect must be reported:\n%s", out)
	}
}

func TestMCPCommandRejectsBadArgumentsAndIsInHelp(t *testing.T) {
	m, _ := newTestModel(t, &fakeSession{model: "m"})
	m = sendLine(t, m, "/mcp connect")
	if out := stripANSI(m.transcriptText()); !strings.Contains(out, "usage: /mcp") {
		t.Fatalf("missing usage line:\n%s", out)
	}
	m = sendLine(t, m, "/help")
	if out := m.transcriptText(); !strings.Contains(out, "/mcp connect <name>") {
		t.Fatalf("help lacks /mcp:\n%s", out)
	}
}
