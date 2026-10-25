package main

import (
	"context"
	"fmt"
	"strings"

	"github.com/tinchoram/groot-cli/internal/config"
	"github.com/tinchoram/groot-cli/internal/mcp"
)

// probeMCP connects each enabled server with its own timeout and lists its
// allowlist. A server that fails reports its error and no tools. Servers are
// listed in name order.
func probeMCP(servers map[string]config.MCPServer, cwd, version string) []mcpCheck {
	m := mcp.New(servers, mcp.Options{Dir: cwd, Version: version})
	defer func() { _ = m.Close() }()
	var out []mcpCheck
	for _, name := range m.Names() {
		timeout := servers[name].Timeout
		if timeout <= 0 {
			timeout = config.DefaultMCPTimeout
		}
		ctx, cancel := context.WithTimeout(context.Background(), timeout)
		remotes, err := m.Tools(ctx, name)
		cancel()
		c := mcpCheck{Name: name}
		if err != nil {
			c.Err = err.Error()
		} else {
			c.Tools = len(remotes)
			c.Missing = m.Missing(name)
		}
		out = append(out, c)
	}
	return out
}

// mcpLines writes the MCP section of the report. A failed server blocks, so the
// exit status is non-zero, as for any other configured part that does not work.
func mcpLines(add func(name, value string, blocking bool), in doctorInput) {
	if in.MCPNone {
		add("mcp", "none configured", false)
		return
	}
	for _, c := range in.MCP {
		name := "mcp " + c.Name
		if c.Err != "" {
			add(name, "failed: "+c.Err, true)
			continue
		}
		value := fmt.Sprintf("ok · %s", toolsWord(c.Tools))
		if len(c.Missing) > 0 {
			value += " · missing: " + strings.Join(c.Missing, ", ")
		}
		add(name, value, false)
	}
}

func toolsWord(n int) string {
	if n == 1 {
		return "1 tool"
	}
	return fmt.Sprintf("%d tools", n)
}
