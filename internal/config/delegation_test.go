package config

import (
	"strings"
	"testing"
)

func TestDelegationDefaultsAndValidation(t *testing.T) {
	cfg := Default()
	if cfg.Flow.Delegation != "auto" || cfg.Flow.Manager != "space-manager" {
		t.Fatalf("delegation = %q, manager = %q", cfg.Flow.Delegation, cfg.Flow.Manager)
	}
	if err := cfg.Validate(); err != nil {
		t.Fatalf("defaults must validate: %v", err)
	}
	cfg.Flow.Delegation = "sometimes"
	if err := cfg.Validate(); err == nil || !strings.Contains(err.Error(), "flow.delegation") {
		t.Fatalf("an unknown delegation mode must be refused, got %v", err)
	}
	cfg.Flow.Delegation = "hint"
	if err := cfg.Validate(); err != nil {
		t.Fatalf("hint is a valid mode: %v", err)
	}
}
