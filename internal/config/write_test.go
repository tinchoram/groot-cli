package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSetProviderModelCreatesMissingFileFromDefaults(t *testing.T) {
	path := filepath.Join(t.TempDir(), "groot.yaml")
	created, err := SetProviderModel(path, "claude-x")
	if err != nil || !created {
		t.Fatalf("created=%v err=%v", created, err)
	}
	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Provider.Model != "claude-x" || cfg.Gate.Policy != "interactive" || cfg.MaxTurns != Default().MaxTurns {
		t.Fatalf("the new file must hold the defaults plus the model: %+v", cfg)
	}
}

func TestSetProviderModelEditsOnlyThatKey(t *testing.T) {
	path := writeFile(t, "groot.yaml", "# keep me\nprovider:\n  name: anthropic\n  model: old-model\nmax_turns: 7\n")
	created, err := SetProviderModel(path, "claude-x")
	if err != nil || created {
		t.Fatalf("created=%v err=%v", created, err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	text := string(raw)
	if !strings.Contains(text, "# keep me") {
		t.Fatalf("the comment must survive the rewrite:\n%s", text)
	}
	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Provider.Model != "claude-x" || cfg.Provider.Name != "anthropic" || cfg.MaxTurns != 7 {
		t.Fatalf("only provider.model may change: %+v", cfg)
	}
}

func TestSetProviderModelAddsMissingProvider(t *testing.T) {
	path := writeFile(t, "groot.yaml", "max_turns: 9\n")
	if _, err := SetProviderModel(path, "claude-x"); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Provider.Model != "claude-x" || cfg.MaxTurns != 9 {
		t.Fatalf("missing provider section must be added: %+v", cfg)
	}
}
