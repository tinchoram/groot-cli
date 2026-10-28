package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestWorkDefaultsAreBoundedAndListTheRiskyCommands(t *testing.T) {
	cfg := Default()
	if cfg.Work.MaxTurns != 50 || cfg.Work.MaxUSD != 5.0 {
		t.Fatalf("work limits = %+v", cfg.Work)
	}
	if cfg.Work.MaxAutoTurns != 50 {
		t.Fatalf("max_auto_turns default = %d, want 50", cfg.Work.MaxAutoTurns)
	}
	if len(cfg.Work.AlwaysAsk) != len(DefaultAlwaysAsk()) || len(cfg.Work.AlwaysAsk) == 0 {
		t.Fatalf("always_ask defaults = %v", cfg.Work.AlwaysAsk)
	}
}

func TestWorkFileOverridesLimits(t *testing.T) {
	path := filepath.Join(t.TempDir(), "groot.yaml")
	doc := "work:\n  max_turns: 10\n  max_usd: 1.5\n"
	if err := os.WriteFile(path, []byte(doc), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Work.MaxTurns != 10 || cfg.Work.MaxUSD != 1.5 {
		t.Fatalf("work = %+v", cfg.Work)
	}
}

func TestWorkRefusesBadValues(t *testing.T) {
	cfg := Default()
	cfg.Work.AlwaysAsk = []string{"("}
	if err := cfg.Validate(); err == nil || !strings.Contains(err.Error(), "always_ask") {
		t.Fatalf("broken pattern: err = %v", err)
	}
	cfg = Default()
	cfg.Work.MaxUSD = -1
	if err := cfg.Validate(); err == nil {
		t.Fatal("a negative budget must be refused")
	}
}
