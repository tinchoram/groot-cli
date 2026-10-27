package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestREADMEMCPExampleLoads loads the MCP example of docs/INTEGRATIONS.md through the
// real loader, so the documented config cannot drift from the parser.
func TestREADMEMCPExampleLoads(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("..", "..", "docs", "INTEGRATIONS.md"))
	if err != nil {
		t.Fatal(err)
	}
	const start, end = "<!-- mcp-example:start -->", "<!-- mcp-example:end -->"
	text := string(raw)
	i := strings.Index(text, start)
	j := strings.Index(text, end)
	if i < 0 || j < i {
		t.Fatal("docs/INTEGRATIONS.md lacks the mcp-example markers")
	}
	block := text[i+len(start) : j]
	block = strings.TrimPrefix(strings.TrimSpace(block), "```yaml")
	block = strings.TrimSuffix(strings.TrimSpace(block), "```")

	path := filepath.Join(t.TempDir(), "groot.yaml")
	if err := os.WriteFile(path, []byte(block), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("docs/INTEGRATIONS.md example does not load: %v\n%s", err, block)
	}
	if len(cfg.MCP.Servers) != 2 {
		t.Fatalf("example defines %d servers, want 2", len(cfg.MCP.Servers))
	}
	if got := cfg.MCP.EnabledServers(); len(got) != 2 {
		t.Fatalf("without mcp.enable every server is enabled, got %d", len(got))
	}
}
