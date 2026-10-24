package config

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func loadYAML(t *testing.T, body string) (Config, error) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "groot.yaml")
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return Load(path)
}

func TestSandboxDefaults(t *testing.T) {
	cfg := Default()
	if cfg.Bash.Sandbox.Mode != "auto" {
		t.Fatalf("default mode = %q, want auto", cfg.Bash.Sandbox.Mode)
	}
	if cfg.Bash.Sandbox.Network {
		t.Fatal("network must be off by default")
	}
	if len(cfg.Bash.Sandbox.ExtraWritable) != 0 {
		t.Fatalf("extra_writable must be empty by default, got %v", cfg.Bash.Sandbox.ExtraWritable)
	}
}

func TestSandboxScalarFormSetsTheMode(t *testing.T) {
	cfg, err := loadYAML(t, "bash:\n  sandbox: strict\n")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Bash.Sandbox.Mode != "strict" {
		t.Fatalf("mode = %q, want strict", cfg.Bash.Sandbox.Mode)
	}
	if cfg.Bash.Sandbox.Network {
		t.Fatal("the scalar form must keep the other defaults")
	}
}

func TestSandboxMappingForm(t *testing.T) {
	cfg, err := loadYAML(t, `
bash:
  sandbox:
    mode: off
    network: true
    extra_writable:
      - /opt/cache
`)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	sb := cfg.Bash.Sandbox
	if sb.Mode != "off" || !sb.Network || !slices.Equal(sb.ExtraWritable, []string{"/opt/cache"}) {
		t.Fatalf("sandbox = %+v", sb)
	}
}

func TestSandboxMappingKeepsDefaultMode(t *testing.T) {
	cfg, err := loadYAML(t, "bash:\n  sandbox:\n    network: true\n")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Bash.Sandbox.Mode != "auto" || !cfg.Bash.Sandbox.Network {
		t.Fatalf("sandbox = %+v, want mode auto and network on", cfg.Bash.Sandbox)
	}
}

func TestSandboxValidation(t *testing.T) {
	cases := []struct {
		name string
		body string
		want string
	}{
		{"unknown mode", "bash:\n  sandbox: maybe\n", "bash.sandbox.mode"},
		{"relative extra path", "bash:\n  sandbox:\n    extra_writable: [relative/cache]\n", "bash.sandbox.extra_writable"},
		{"blank extra path", "bash:\n  sandbox:\n    extra_writable: [\"  \"]\n", "bash.sandbox.extra_writable"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := loadYAML(t, tc.body)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error = %v, want it to name %q", err, tc.want)
			}
		})
	}
}
