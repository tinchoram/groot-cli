package config

import (
	"fmt"
	"net/url"
	"regexp"
	"strings"
	"time"
)

// DefaultMCPTimeout bounds one MCP tool call when the server sets no timeout.
const DefaultMCPTimeout = 30 * time.Second

// maxMCPTimeout is the upper bound of a server timeout.
const maxMCPTimeout = 5 * time.Minute

// MCPConfig holds the MCP servers groot can call. Servers are defined only in
// the global config: a project file cannot start processes. A project file may
// set Enable to narrow which global servers its space uses.
type MCPConfig struct {
	Enable  []string             `yaml:"enable"`
	Servers map[string]MCPServer `yaml:"servers"`
}

// MCPServer is one MCP server. Transport is stdio (a local process started with
// Command and Args) or http (a streamable HTTP endpoint at URL). Values may use
// ${VAR}; they are expanded at connect time from the process environment, never
// at load, so a secret stays out of the config file's parsed values.
type MCPServer struct {
	Transport string            `yaml:"transport"`
	Command   string            `yaml:"command"`
	Args      []string          `yaml:"args"`
	Env       map[string]string `yaml:"env"`
	URL       string            `yaml:"url"`
	Headers   map[string]string `yaml:"headers"`
	Tools     []string          `yaml:"tools"`
	ReadOnly  []string          `yaml:"read_only"`
	Timeout   time.Duration     `yaml:"timeout"`
}

var mcpServerName = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,31}$`)

// EnabledServers returns the servers this space uses: every global server when
// Enable is empty, otherwise only the named ones.
func (c MCPConfig) EnabledServers() map[string]MCPServer {
	out := make(map[string]MCPServer, len(c.Servers))
	if len(c.Enable) == 0 {
		for name, s := range c.Servers {
			out[name] = s
		}
		return out
	}
	for _, name := range c.Enable {
		if s, ok := c.Servers[name]; ok {
			out[name] = s
		}
	}
	return out
}

// validateMCP checks the names, transports, allowlists and timeouts of every
// server, and that Enable names only defined servers. Errors name the server.
func validateMCP(c MCPConfig) error {
	for name, s := range c.Servers {
		if err := validateMCPServer(name, s); err != nil {
			return fmt.Errorf("mcp.servers.%s: %w", name, err)
		}
	}
	for _, name := range c.Enable {
		if _, ok := c.Servers[name]; !ok {
			return fmt.Errorf("mcp.enable: %q is not a server in the global config", name)
		}
	}
	return nil
}

func validateMCPServer(name string, s MCPServer) error {
	if !mcpServerName.MatchString(name) {
		return fmt.Errorf("the name must match %s", mcpServerName.String())
	}
	switch s.Transport {
	case "stdio":
		if strings.TrimSpace(s.Command) == "" {
			return fmt.Errorf("stdio needs a command")
		}
	case "http":
		if !isHTTPSOrLocalhost(s.URL) {
			return fmt.Errorf("http needs an https:// url, or http://localhost")
		}
	default:
		return fmt.Errorf("transport %q is invalid (use stdio or http)", s.Transport)
	}
	if len(s.Tools) == 0 {
		return fmt.Errorf("tools must list at least one tool (an explicit allowlist is required)")
	}
	allowed := make(map[string]bool, len(s.Tools))
	for _, tool := range s.Tools {
		if strings.TrimSpace(tool) == "" {
			return fmt.Errorf("tools must not contain an empty name")
		}
		allowed[tool] = true
	}
	for _, tool := range s.ReadOnly {
		if !allowed[tool] {
			return fmt.Errorf("read_only names %q, which is not in tools", tool)
		}
	}
	if s.Timeout < 0 || s.Timeout > maxMCPTimeout {
		return fmt.Errorf("timeout must be between 0 and %s, got %s", maxMCPTimeout, s.Timeout)
	}
	return nil
}

// isHTTPSOrLocalhost accepts https URLs, and plain http only for localhost.
func isHTTPSOrLocalhost(raw string) bool {
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" {
		return false
	}
	switch u.Scheme {
	case "https":
		return true
	case "http":
		host := u.Hostname()
		return host == "localhost"
	}
	return false
}

// ExpandStrict is Expand for secrets: a reference to an unset or empty variable
// without a default is an error, so an empty token is never sent silently. The
// error names the variable.
func ExpandStrict(s string, lookup func(string) (string, bool)) (string, error) {
	var missing string
	out := envRef.ReplaceAllStringFunc(s, func(m string) string {
		sub := envRef.FindStringSubmatch(m)
		name, hasDefault, def := sub[1], sub[2] != "", sub[3]
		if v, ok := lookup(name); ok && v != "" {
			return v
		}
		if hasDefault {
			return def
		}
		if missing == "" {
			missing = name
		}
		return ""
	})
	if missing != "" {
		return "", fmt.Errorf("${%s} is not set", missing)
	}
	return out, nil
}
