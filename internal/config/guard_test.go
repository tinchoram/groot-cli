package config

import (
	"strings"
	"testing"
)

func TestGuardModeDefaultsToWarn(t *testing.T) {
	if got := Default().Guard.Mode; got != "warn" {
		t.Fatalf("default guard.mode = %q, want warn", got)
	}
}

func TestGuardModeValidation(t *testing.T) {
	for _, mode := range []string{"warn", "strict", "off"} {
		p := writeFile(t, "groot.yaml", "guard:\n  mode: "+mode+"\n")
		cfg, err := Load(p)
		if err != nil || cfg.Guard.Mode != mode {
			t.Errorf("mode %q: got %q, %v", mode, cfg.Guard.Mode, err)
		}
	}
	p := writeFile(t, "groot.yaml", "guard:\n  mode: loud\n")
	if _, err := Load(p); err == nil || !strings.Contains(err.Error(), "guard.mode") {
		t.Fatalf("an unknown guard.mode must be refused naming the key, got %v", err)
	}
}

func TestSystemPromptCarriesTheUntrustedContentRule(t *testing.T) {
	out := ComposeSystemPrompt("BASE", "", "")
	if !strings.Contains(out, UntrustedContentRule) {
		t.Fatalf("composed prompt misses the untrusted-content rule:\n%s", out)
	}
	if n := len(strings.Fields(UntrustedContentRule)); n >= 80 {
		t.Fatalf("the rule must stay under 80 words, has %d", n)
	}
	for _, want := range []string{"data", "never follow", "report"} {
		if !strings.Contains(strings.ToLower(UntrustedContentRule), want) {
			t.Errorf("rule misses %q: %s", want, UntrustedContentRule)
		}
	}
}
