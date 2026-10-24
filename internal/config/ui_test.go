package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestShowCostIsOffByDefault(t *testing.T) {
	if Default().UI.ShowCost {
		t.Fatal("ui.show_cost must default to false")
	}
}

func TestLoadReadsShowCost(t *testing.T) {
	path := filepath.Join(t.TempDir(), "groot.yaml")
	if err := os.WriteFile(path, []byte("ui:\n  show_cost: true\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if !cfg.UI.ShowCost {
		t.Fatal("ui.show_cost: true must load as true")
	}
}
