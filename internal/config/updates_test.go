package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestUpdatesCheckIsOnByDefault(t *testing.T) {
	if !Default().Updates.Check {
		t.Fatal("updates.check must default to true")
	}
}

func TestLoadReadsUpdatesCheckOff(t *testing.T) {
	path := filepath.Join(t.TempDir(), "groot.yaml")
	if err := os.WriteFile(path, []byte("updates:\n  check: false\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Updates.Check {
		t.Fatal("updates.check: false must load as false, and keep the other defaults")
	}
}
