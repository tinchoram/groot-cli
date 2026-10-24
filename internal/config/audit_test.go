package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeConfig(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "groot.yaml")
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestAuditSinksLoadAndExpandSecretsFromEnv(t *testing.T) {
	t.Setenv("AUDIT_TOKEN", "tok-from-env")
	path := writeConfig(t, `
audit:
  path: .groot/audit.jsonl
  sinks:
    - type: file
      path: ~/groot-audit/all.jsonl
    - type: http
      url: https://collector.example/ingest
      headers: {Authorization: "Bearer ${AUDIT_TOKEN}"}
      batch: 50
      flush_seconds: 5
    - type: syslog
      tag: groot
`)
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Audit.Path != ".groot/audit.jsonl" || len(cfg.Audit.Sinks) != 3 {
		t.Fatalf("audit = %+v", cfg.Audit)
	}
	h := cfg.Audit.Sinks[1]
	if h.Headers["Authorization"] != "Bearer tok-from-env" || h.Batch != 50 || h.FlushSeconds != 5 {
		t.Fatalf("http sink = %+v", h)
	}
	if cfg.Audit.Sinks[2].Tag != "groot" {
		t.Fatalf("syslog tag = %q", cfg.Audit.Sinks[2].Tag)
	}
}

func TestAuditDefaultsHaveNoSinks(t *testing.T) {
	cfg := Default()
	if cfg.Audit.Path != ".groot/audit.jsonl" || len(cfg.Audit.Sinks) != 0 {
		t.Fatalf("defaults = %+v", cfg.Audit)
	}
}

func TestAuditSinkValidation(t *testing.T) {
	cases := []struct {
		name    string
		sink    SinkConfig
		wantErr string // substring; empty means valid
	}{
		{"file ok", SinkConfig{Type: "file", Path: "/var/log/groot.jsonl"}, ""},
		{"file path empty", SinkConfig{Type: "file"}, "path is empty"},
		{"file path blank", SinkConfig{Type: "file", Path: "   "}, "path is empty"},
		{"http ok", SinkConfig{Type: "http", URL: "https://collector.example/ingest", Batch: 10, FlushSeconds: 1}, ""},
		{"http without url", SinkConfig{Type: "http"}, "needs a url"},
		{"http bad scheme", SinkConfig{Type: "http", URL: "ftp://x"}, "http(s) url"},
		{"http negative batch", SinkConfig{Type: "http", URL: "https://x", Batch: -1}, "batch"},
		{"http negative flush", SinkConfig{Type: "http", URL: "https://x", FlushSeconds: -2}, "flush_seconds"},
		{"syslog ok", SinkConfig{Type: "syslog", Tag: "groot"}, ""},
		{"syslog without tag", SinkConfig{Type: "syslog"}, ""},
		{"unknown type", SinkConfig{Type: "kafka"}, `unknown type "kafka"`},
		{"missing type", SinkConfig{}, "unknown type"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			cfg := Default()
			cfg.Audit.Sinks = []SinkConfig{c.sink}
			err := cfg.Validate()
			if c.wantErr == "" {
				if err != nil {
					t.Fatalf("valid sink rejected: %v", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), c.wantErr) {
				t.Fatalf("error = %v, want %q", err, c.wantErr)
			}
		})
	}
}

func TestAuditSinkErrorsNeverEchoHeaderValues(t *testing.T) {
	const secret = "tok-VALIDATION-SECRET"
	cfg := Default()
	cfg.Audit.Sinks = []SinkConfig{{Type: "http", Headers: map[string]string{"Authorization": "Bearer " + secret}}}
	err := cfg.Validate()
	if err == nil {
		t.Fatal("http sink without url must fail")
	}
	if strings.Contains(err.Error(), secret) {
		t.Fatalf("validation error leaks the header value: %v", err)
	}
}

func TestAuditSinkRequiresEnvVarToBeSetOrDefaulted(t *testing.T) {
	os.Unsetenv("AUDIT_MISSING_TOKEN")
	path := writeConfig(t, `
audit:
  sinks:
    - type: http
      url: https://collector.example/ingest
      headers: {Authorization: "Bearer ${AUDIT_MISSING_TOKEN:-none}"}
`)
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got := cfg.Audit.Sinks[0].Headers["Authorization"]; got != "Bearer none" {
		t.Fatalf("default not applied: %q", got)
	}
}
