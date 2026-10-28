package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestProviderEffortIsUnsetByDefault(t *testing.T) {
	if got := Default().Provider.Effort; got != "" {
		t.Fatalf("provider.effort default = %q, want unset (not sent)", got)
	}
}

func TestProviderEffortLoadsFromFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "groot.yaml")
	if err := os.WriteFile(path, []byte("provider:\n  effort: xhigh\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Provider.Effort != "xhigh" {
		t.Fatalf("provider.effort = %q, want xhigh", cfg.Provider.Effort)
	}
}

func TestProviderEffortIsValidatedAtLoad(t *testing.T) {
	path := filepath.Join(t.TempDir(), "groot.yaml")
	if err := os.WriteFile(path, []byte("provider:\n  effort: turbo\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := Load(path)
	if err == nil || !strings.Contains(err.Error(), "provider.effort") || !strings.Contains(err.Error(), "low, medium, high, xhigh or max") {
		t.Fatalf("err = %v, want a clear provider.effort error naming the levels", err)
	}
}

func TestCompactionIsSummarizeByDefault(t *testing.T) {
	if got := Default().Compaction.Strategy; got != "summarize" {
		t.Fatalf("compaction.strategy default = %q, want summarize", got)
	}
}

func TestCompactionNoneStillDisablesCompaction(t *testing.T) {
	path := filepath.Join(t.TempDir(), "groot.yaml")
	if err := os.WriteFile(path, []byte("compaction:\n  strategy: none\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Compaction.Strategy != "none" {
		t.Fatalf("strategy = %q, want none", cfg.Compaction.Strategy)
	}
}

func TestProviderNameAcceptsAnthropicAndGemini(t *testing.T) {
	for _, name := range []string{"anthropic", "gemini"} {
		path := filepath.Join(t.TempDir(), "groot.yaml")
		if err := os.WriteFile(path, []byte("provider:\n  name: "+name+"\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		cfg, err := Load(path)
		if err != nil {
			t.Fatalf("provider.name %s: %v", name, err)
		}
		if cfg.Provider.Name != name {
			t.Fatalf("provider.name = %q, want %q", cfg.Provider.Name, name)
		}
	}
}

func TestProviderNameRefusesUnknownProviders(t *testing.T) {
	path := filepath.Join(t.TempDir(), "groot.yaml")
	if err := os.WriteFile(path, []byte("provider:\n  name: bedrock\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := Load(path)
	if err == nil || !strings.Contains(err.Error(), "anthropic, gemini") {
		t.Fatalf("err = %v, want the supported names listed", err)
	}
}
