package ui

import (
	"context"
	"fmt"
	"strings"

	"github.com/tinchoram/groot-cli/internal/workspace"
)

// mcpUsage is the reply to a /mcp command that does not parse.
const mcpUsage = "usage: /mcp | /mcp connect <name>"

// mcpNone is the reply to /mcp when the config defines no server.
const mcpNone = "no MCP servers configured: add mcp.servers to ~/.groot/groot.yaml"

// runMCPCommand handles /mcp: the servers of the active space, and /mcp connect
// <name>, which connects one server now and reports what it offers.
func (m *Model) runMCPCommand(arg string) {
	fields := strings.Fields(arg)
	switch {
	case len(fields) == 0:
		m.showMCP()
	case fields[0] == "connect" && len(fields) == 2:
		m.connectMCP(fields[1])
	default:
		m.append(styleError.Render(mcpUsage))
	}
}

// showMCP lists each server with its state, then the allowlisted tools of each
// connected server with its read-only flag and the agents that hold it.
func (m *Model) showMCP() {
	servers, err := m.ws.MCPServers(context.Background())
	if err != nil {
		m.append(styleError.Render("error: " + err.Error()))
		return
	}
	if len(servers) == 0 {
		m.append(styleDim.Render(mcpNone))
		return
	}
	var sb strings.Builder
	for _, s := range servers {
		sb.WriteString(mcpServerLine(s) + "\n")
		for _, t := range s.Tools {
			mode := "rw"
			if t.ReadOnly {
				mode = "ro"
			}
			holders := "none"
			if len(t.Agents) > 0 {
				holders = strings.Join(t.Agents, ", ")
			}
			fmt.Fprintf(&sb, "  %-28s %s  held by: %s\n", t.Name, mode, holders)
		}
	}
	m.append(strings.TrimRight(sb.String(), "\n"))
}

// connectMCP connects one server now and reports the tools it admits.
func (m *Model) connectMCP(name string) {
	n, err := m.ws.MCPConnect(context.Background(), name)
	if err != nil {
		m.append(styleError.Render("mcp " + name + ": " + err.Error()))
		return
	}
	m.append(styleDim.Render(fmt.Sprintf("mcp %s: connected · %s", name, toolCount(n))))
}

// mcpServerLine is the summary line of one server:
// name · transport · state · N tools · missing: a, b.
func mcpServerLine(s workspace.MCPServerInfo) string {
	state := "not connected yet"
	switch {
	case s.Connected:
		state = "connected"
	case s.Err != "":
		state = "failed: " + s.Err
	}
	parts := []string{s.Name, s.Transport, state, toolCount(len(s.Tools))}
	if len(s.Missing) > 0 {
		parts = append(parts, "missing: "+strings.Join(s.Missing, ", "))
	}
	return strings.Join(parts, " · ")
}

func toolCount(n int) string {
	if n == 1 {
		return "1 tool"
	}
	return fmt.Sprintf("%d tools", n)
}
