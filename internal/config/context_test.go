package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestContextConfigDefaults(t *testing.T) {
	cfg := Default()
	if cfg.Compaction.MaxContextPercent != 80 {
		t.Fatalf("default compaction.max_context_percent = %d, want 80", cfg.Compaction.MaxContextPercent)
	}
	if err := cfg.Validate(); err != nil {
		t.Fatalf("defaults must validate: %v", err)
	}
}

func TestLoadReadsContextKeys(t *testing.T) {
	path := filepath.Join(t.TempDir(), "groot.yaml")
	raw := "compaction:\n  strategy: window\n  keep: 30\n  max_context_percent: 0\nmodels:\n  context_window:\n    claude-haiku-5-5: 128000\n"
	if err := os.WriteFile(path, []byte(raw), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Compaction.MaxContextPercent != 0 {
		t.Fatalf("max_context_percent = %d, want 0 (disabled)", cfg.Compaction.MaxContextPercent)
	}
	if cfg.Models.ContextWindow["claude-haiku-5-5"] != 128000 {
		t.Fatalf("context_window = %v", cfg.Models.ContextWindow)
	}
}

func TestValidateRefusesBadContextValues(t *testing.T) {
	cfg := Default()
	cfg.Compaction.MaxContextPercent = 101
	if err := cfg.Validate(); err == nil || !strings.Contains(err.Error(), "max_context_percent") {
		t.Fatalf("percent above 100: err = %v", err)
	}
	cfg = Default()
	cfg.Compaction.MaxContextPercent = -1
	if err := cfg.Validate(); err == nil {
		t.Fatal("negative percent must be refused")
	}
	cfg = Default()
	cfg.Models.ContextWindow = map[string]int{"claude-haiku-5-5": 0}
	if err := cfg.Validate(); err == nil || !strings.Contains(err.Error(), "context_window") {
		t.Fatalf("zero window: err = %v", err)
	}
}

func TestSetContextWindowEditsTheFileInPlace(t *testing.T) {
	path := filepath.Join(t.TempDir(), "groot.yaml")
	raw := "# my settings\nprovider:\n  name: anthropic\n  model: claude-sonnet-5-5\n"
	if err := os.WriteFile(path, []byte(raw), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := SetContextWindow(path, "claude-haiku-5-5", 128000); err != nil {
		t.Fatal(err)
	}
	if err := SetContextWindow(path, "claude-haiku-5-5", 150000); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Models.ContextWindow["claude-haiku-5-5"] != 150000 {
		t.Fatalf("saved window = %v, want 150000", cfg.Models.ContextWindow)
	}
	if cfg.Provider.Model != "claude-sonnet-5-5" {
		t.Fatalf("other keys changed: provider.model = %q", cfg.Provider.Model)
	}
	b, _ := os.ReadFile(path)
	if !strings.Contains(string(b), "# my settings") {
		t.Fatal("comments must survive the edit")
	}
}

func TestSetContextWindowCreatesMissingFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "groot.yaml")
	if err := SetContextWindow(path, "claude-opus-5-5", 300000); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Models.ContextWindow["claude-opus-5-5"] != 300000 {
		t.Fatalf("created file window = %v", cfg.Models.ContextWindow)
	}
}
