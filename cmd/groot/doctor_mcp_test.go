package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/tinchoram/groot-cli/internal/config"
)

const grafanaGlobalConfig = `mcp:
  servers:
    grafana:
      transport: http
      url: https://grafana.internal/mcp
      tools: [echo, chart]
      read_only: [echo]
`

// writeGlobalConfig puts a global groot.yaml with the given text in the home.
func writeGlobalConfig(t *testing.T, env doctorEnv, text string) {
	t.Helper()
	if err := os.WriteFile(env.GlobalPath, []byte(text), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestDoctorMCPSectionSaysNoneConfiguredAndDoesNotProbe(t *testing.T) {
	env := fakeDoctorEnv(t, t.TempDir())
	env.EnvKey = "sk-ant-doctor-test-1234"
	env.ProbeMCP = func(map[string]config.MCPServer) []mcpCheck {
		t.Fatal("doctor must not probe when no server is configured")
		return nil
	}
	out, code := runDoctorWith(t, env)
	if code != 0 {
		t.Fatalf("exit %d with no MCP servers:\n%s", code, out)
	}
	if l := lineWith(out, "mcp:"); l != "mcp: none configured" {
		t.Fatalf("mcp line = %q", l)
	}
}

func TestDoctorMCPSectionReportsOKWithToolCountAndMissing(t *testing.T) {
	env := fakeDoctorEnv(t, t.TempDir())
	env.EnvKey = "sk-ant-doctor-test-1234"
	writeGlobalConfig(t, env, grafanaGlobalConfig)
	env.ProbeMCP = func(servers map[string]config.MCPServer) []mcpCheck {
		if _, ok := servers["grafana"]; !ok {
			t.Fatalf("probe got servers %v, want grafana", servers)
		}
		return []mcpCheck{{Name: "grafana", Tools: 2, Missing: []string{"chart"}}}
	}
	out, code := runDoctorWith(t, env)
	if code != 0 {
		t.Fatalf("a connected server must not change the exit code, got %d:\n%s", code, out)
	}
	if l := lineWith(out, "mcp grafana:"); l != "mcp grafana: ok · 2 tools · missing: chart" {
		t.Fatalf("mcp grafana line = %q", l)
	}
}

func TestDoctorMCPFailedServerIsReportedAndFailsTheExitCode(t *testing.T) {
	env := fakeDoctorEnv(t, t.TempDir())
	env.EnvKey = "sk-ant-doctor-test-1234"
	writeGlobalConfig(t, env, grafanaGlobalConfig)
	env.ProbeMCP = func(map[string]config.MCPServer) []mcpCheck {
		return []mcpCheck{{Name: "grafana", Err: "mcp server grafana: connection refused"}}
	}
	out, code := runDoctorWith(t, env)
	if code != 1 {
		t.Fatalf("a configured server that fails to connect must give exit 1, got %d:\n%s", code, out)
	}
	if l := lineWith(out, "mcp grafana:"); l != "mcp grafana: failed: mcp server grafana: connection refused" {
		t.Fatalf("mcp grafana line = %q", l)
	}
}

func TestDoctorMCPProbeUsesTheEnabledServersOnly(t *testing.T) {
	env := fakeDoctorEnv(t, t.TempDir())
	env.EnvKey = "sk-ant-doctor-test-1234"
	writeGlobalConfig(t, env, grafanaGlobalConfig+`    k8s:
      transport: stdio
      command: kube-mcp
      tools: [pods]
`)
	// The project file narrows the global servers for this space.
	if err := os.WriteFile(filepath.Join(env.Cwd, "groot.yaml"), []byte("mcp:\n  enable: [grafana]\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	var seen []string
	env.ProbeMCP = func(servers map[string]config.MCPServer) []mcpCheck {
		for name := range servers {
			seen = append(seen, name)
		}
		return nil
	}
	runDoctorWith(t, env)
	if len(seen) != 1 || seen[0] != "grafana" {
		t.Fatalf("probe covered %v, want only the enabled grafana", seen)
	}
}
