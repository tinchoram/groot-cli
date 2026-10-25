package config

import (
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestBashTimeoutDefaults(t *testing.T) {
	cfg, err := Load(filepath.Join(t.TempDir(), "missing.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Bash.Timeout != 60*time.Second || cfg.Bash.MaxTimeout != 10*time.Minute {
		t.Fatalf("timeouts = %v / %v, want 60s / 10m", cfg.Bash.Timeout, cfg.Bash.MaxTimeout)
	}
}

func TestBashTimeoutReadsFromYAML(t *testing.T) {
	cfg, err := loadYAML(t, "bash:\n  timeout: 2m\n  max_timeout: 30m\n")
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Bash.Timeout != 2*time.Minute || cfg.Bash.MaxTimeout != 30*time.Minute {
		t.Fatalf("timeouts = %v / %v, want 2m / 30m", cfg.Bash.Timeout, cfg.Bash.MaxTimeout)
	}
}

func TestBashTimeoutValidation(t *testing.T) {
	cases := []struct {
		name string
		body string
		want string
	}{
		{"timeout below one second", "bash:\n  timeout: 500ms\n", "bash.timeout"},
		{"zero timeout", "bash:\n  timeout: 0s\n", "bash.timeout"},
		{"max below timeout", "bash:\n  timeout: 2m\n  max_timeout: 1m\n", "bash.max_timeout"},
		{"max above one hour", "bash:\n  max_timeout: 2h\n", "bash.max_timeout"},
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
