package config

import (
	"sort"
	"strings"
	"testing"
	"time"
)

const mcpGlobalYAML = `mcp:
  servers:
    grafana:
      transport: http
      url: https://grafana.internal/mcp
      headers: { Authorization: "Bearer ${GRAFANA_TOKEN}" }
      tools: [query_prometheus, search_dashboards]
      read_only: [query_prometheus, search_dashboards]
      timeout: 30s
    k8s:
      transport: stdio
      command: mcp-kubernetes
      args: ["--read-only"]
      env: { KUBECONFIG: "${HOME}/.kube/config" }
      tools: [list_pods, get_logs]
      read_only: [list_pods, get_logs]
`

func enabledNames(c MCPConfig) []string {
	var names []string
	for name := range c.EnabledServers() {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

func TestMCPServersAreAcceptedFromTheGlobalConfig(t *testing.T) {
	dir := t.TempDir()
	global := writeLayer(t, dir, "global.yaml", mcpGlobalYAML)

	cfg, err := LoadLayers(global)
	if err != nil {
		t.Fatal(err)
	}
	grafana, ok := cfg.MCP.Servers["grafana"]
	if !ok {
		t.Fatal("grafana server missing")
	}
	if grafana.Transport != "http" || grafana.URL != "https://grafana.internal/mcp" {
		t.Fatalf("grafana decoded wrong: %+v", grafana)
	}
	if grafana.Timeout != 30*time.Second {
		t.Fatalf("explicit timeout must decode, got %v", grafana.Timeout)
	}
	k8s := cfg.MCP.Servers["k8s"]
	if k8s.Command != "mcp-kubernetes" || len(k8s.Args) != 1 || k8s.Args[0] != "--read-only" {
		t.Fatalf("k8s decoded wrong: %+v", k8s)
	}
	if k8s.Timeout != DefaultMCPTimeout {
		t.Fatalf("a missing timeout must default to %v, got %v", DefaultMCPTimeout, k8s.Timeout)
	}
}

func TestMCPSecretReferencesStayLiteralWhenLoaded(t *testing.T) {
	dir := t.TempDir()
	global := writeLayer(t, dir, "global.yaml", mcpGlobalYAML)
	t.Setenv("GRAFANA_TOKEN", "from-env")

	cfg, err := LoadLayers(global)
	if err != nil {
		t.Fatalf("an unset secret must not fail the load: %v", err)
	}
	if got := cfg.MCP.Servers["grafana"].Headers["Authorization"]; got != "Bearer ${GRAFANA_TOKEN}" {
		t.Fatalf("mcp values expand at connect time, not at load; got %q", got)
	}
	if got := cfg.MCP.Servers["k8s"].Env["KUBECONFIG"]; got != "${HOME}/.kube/config" {
		t.Fatalf("mcp env must stay literal at load, got %q", got)
	}
}

func TestProjectFileCannotDefineMCPServers(t *testing.T) {
	dir := t.TempDir()
	global := writeLayer(t, dir, "global.yaml", mcpGlobalYAML)
	project := writeLayer(t, dir, "project.yaml", "mcp:\n  servers:\n    evil:\n      transport: stdio\n      command: sh\n      tools: [run]\n")

	_, err := LoadLayers(global, project)
	const want = "mcp.servers can only be set in the global config (~/.groot/groot.yaml): a project file cannot start processes"
	if err == nil || !strings.Contains(err.Error(), want) {
		t.Fatalf("want error containing %q, got %v", want, err)
	}
}

func TestProjectFileNarrowsGlobalServersWithEnable(t *testing.T) {
	dir := t.TempDir()
	global := writeLayer(t, dir, "global.yaml", mcpGlobalYAML)
	project := writeLayer(t, dir, "project.yaml", "mcp:\n  enable: [k8s]\n")

	cfg, err := LoadLayers(global, project)
	if err != nil {
		t.Fatal(err)
	}
	if got := enabledNames(cfg.MCP); len(got) != 1 || got[0] != "k8s" {
		t.Fatalf("enable must narrow to k8s, got %v", got)
	}
	if len(cfg.MCP.Servers) != 2 {
		t.Fatalf("narrowing must not drop global definitions, got %d servers", len(cfg.MCP.Servers))
	}
}

func TestEnabledServersWithoutEnableMeansEveryGlobalServer(t *testing.T) {
	dir := t.TempDir()
	global := writeLayer(t, dir, "global.yaml", mcpGlobalYAML)

	cfg, err := LoadLayers(global)
	if err != nil {
		t.Fatal(err)
	}
	if got := enabledNames(cfg.MCP); len(got) != 2 {
		t.Fatalf("no enable list must activate all global servers, got %v", got)
	}
}

func TestMCPEnableRejectsUnknownServer(t *testing.T) {
	dir := t.TempDir()
	global := writeLayer(t, dir, "global.yaml", mcpGlobalYAML)
	project := writeLayer(t, dir, "project.yaml", "mcp:\n  enable: [nope]\n")

	_, err := LoadLayers(global, project)
	if err == nil || !strings.Contains(err.Error(), "nope") {
		t.Fatalf("enable naming an unknown server must fail, got %v", err)
	}
}

func TestMCPServerValidation(t *testing.T) {
	const head = "mcp:\n  servers:\n"
	tests := []struct {
		name    string
		body    string
		wantErr string
	}{
		{
			name: "valid stdio",
			body: "    k8s:\n      transport: stdio\n      command: mcp-kubernetes\n      tools: [list_pods]\n",
		},
		{
			name: "valid http on localhost",
			body: "    local:\n      transport: http\n      url: http://localhost:8080/mcp\n      tools: [a]\n",
		},
		{
			name:    "server name with underscore",
			body:    "    Grafana_1:\n      transport: stdio\n      command: x\n      tools: [a]\n",
			wantErr: "mcp.servers.Grafana_1",
		},
		{
			name:    "unknown transport",
			body:    "    s1:\n      transport: sse\n      command: x\n      tools: [a]\n",
			wantErr: "mcp.servers.s1",
		},
		{
			name:    "stdio without command",
			body:    "    s2:\n      transport: stdio\n      tools: [a]\n",
			wantErr: "mcp.servers.s2",
		},
		{
			name:    "http over plain http to a remote host",
			body:    "    s3:\n      transport: http\n      url: http://example.com/mcp\n      tools: [a]\n",
			wantErr: "mcp.servers.s3",
		},
		{
			name:    "empty tools",
			body:    "    s4:\n      transport: stdio\n      command: x\n",
			wantErr: "mcp.servers.s4",
		},
		{
			name:    "read_only outside tools",
			body:    "    s5:\n      transport: stdio\n      command: x\n      tools: [a]\n      read_only: [b]\n",
			wantErr: "mcp.servers.s5",
		},
		{
			name:    "timeout above five minutes",
			body:    "    s6:\n      transport: stdio\n      command: x\n      tools: [a]\n      timeout: 10m\n",
			wantErr: "mcp.servers.s6",
		},
		{
			name:    "negative timeout",
			body:    "    s7:\n      transport: stdio\n      command: x\n      tools: [a]\n      timeout: -1s\n",
			wantErr: "mcp.servers.s7",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			global := writeLayer(t, dir, "global.yaml", head+tt.body)
			_, err := LoadLayers(global)
			if tt.wantErr == "" {
				if err != nil {
					t.Fatalf("valid server rejected: %v", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("want error naming %q, got %v", tt.wantErr, err)
			}
		})
	}
}

func TestExpandStrictRequiresEverySecretReference(t *testing.T) {
	env := map[string]string{"SET": "value", "EMPTY": ""}
	lookup := func(k string) (string, bool) { v, ok := env[k]; return v, ok }

	if got, err := ExpandStrict("Bearer ${SET}", lookup); err != nil || got != "Bearer value" {
		t.Fatalf("got %q, %v", got, err)
	}
	if _, err := ExpandStrict("Bearer ${MISSING_VAR}", lookup); err == nil || err.Error() != "${MISSING_VAR} is not set" {
		t.Fatalf("an unset variable must be an error naming it, got %v", err)
	}
	if _, err := ExpandStrict("${EMPTY}", lookup); err == nil {
		t.Fatal("an empty variable is as unusable as an unset one")
	}
	if got, err := ExpandStrict("${MISSING_VAR:-fallback}", lookup); err != nil || got != "fallback" {
		t.Fatalf("the default form still applies, got %q, %v", got, err)
	}
	if got, _ := ExpandStrict("no refs", lookup); got != "no refs" {
		t.Fatalf("plain text must pass through, got %q", got)
	}
}
