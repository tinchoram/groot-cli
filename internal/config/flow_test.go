package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestFlowDefaultsAreStrictWithOneModelPerSize(t *testing.T) {
	cfg := Default()
	if cfg.Flow.Mode != "strict" {
		t.Fatalf("mode = %q, want strict", cfg.Flow.Mode)
	}
	if cfg.Flow.Routes.Small.Model != "claude-haiku-5-5" ||
		cfg.Flow.Routes.Medium.Model != "claude-sonnet-5-5" ||
		cfg.Flow.Routes.Large.Model != "claude-opus-5-5" {
		t.Fatalf("routes = %+v", cfg.Flow.Routes)
	}
}

func TestFlowFileOverridesOnlyWhatItNames(t *testing.T) {
	path := filepath.Join(t.TempDir(), "groot.yaml")
	doc := "flow:\n  mode: advisory\n  routes:\n    large:\n      model: claude-opus-6\n"
	if err := os.WriteFile(path, []byte(doc), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Flow.Mode != "advisory" || cfg.Flow.Routes.Large.Model != "claude-opus-6" {
		t.Fatalf("flow = %+v", cfg.Flow)
	}
	if cfg.Flow.Routes.Medium.Model != "claude-sonnet-5-5" {
		t.Fatal("unnamed routes keep their defaults")
	}
}

func TestFlowModeIsValidated(t *testing.T) {
	cfg := Default()
	cfg.Flow.Mode = "lenient"
	if err := cfg.Validate(); err == nil || !strings.Contains(err.Error(), "flow.mode") {
		t.Fatalf("err = %v", err)
	}
}
