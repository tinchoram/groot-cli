package main

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func noEnv(string) string { return "" }

func keyEnv(k string) string {
	if k == "ANTHROPIC_API_KEY" {
		return "test-key-not-real"
	}
	return ""
}

func TestVersionFlagPrintsAndExitsZero(t *testing.T) {
	var out, errOut bytes.Buffer
	code := run([]string{"--version"}, &out, &errOut, noEnv)
	if code != 0 {
		t.Fatalf("exit %d, stderr %q", code, errOut.String())
	}
	if !strings.HasPrefix(out.String(), "groot ") {
		t.Fatalf("unexpected version output %q", out.String())
	}
}

func TestMissingAPIKeyGivesHintAndExitsNonZero(t *testing.T) {
	var out, errOut bytes.Buffer
	code := run([]string{"--config", "does-not-exist.yaml"}, &out, &errOut, noEnv)
	if code == 0 {
		t.Fatal("missing key must fail")
	}
	msg := errOut.String()
	if !strings.Contains(msg, "ANTHROPIC_API_KEY") || !strings.Contains(msg, "export") {
		t.Fatalf("hint missing: %q", msg)
	}
	if strings.Contains(msg, "panic") {
		t.Fatal("must not panic")
	}
}

func TestExplicitConfigMustExist(t *testing.T) {
	var out, errOut bytes.Buffer
	code := run([]string{"--config", "/nonexistent/groot.yaml"}, &out, &errOut, keyEnv)
	if code == 0 || !strings.Contains(errOut.String(), "groot.yaml") {
		t.Fatalf("explicit missing config must fail clearly: code=%d err=%q", code, errOut.String())
	}
}

func TestInvalidPolicyFlagIsRejected(t *testing.T) {
	var out, errOut bytes.Buffer
	code := run([]string{"--policy", "yolo", "--config", "/nonexistent.yaml"}, &out, &errOut, keyEnv)
	if code == 0 {
		t.Fatal("invalid policy must fail")
	}
}

func TestBuildGateByPolicy(t *testing.T) {
	cases := map[string]string{
		"interactive": "gate.Interactive",
		"readonly":    "gate.ReadOnly",
		"auto":        "gate.AutoApprove",
	}
	for policy, want := range cases {
		g, err := buildGate(policy, nil)
		if err != nil {
			t.Fatalf("%s: %v", policy, err)
		}
		if got := typeName(g); got != want {
			t.Errorf("policy %s built %s, want %s", policy, got, want)
		}
	}
	if _, err := buildGate("nope", nil); err == nil {
		t.Fatal("unknown policy must error")
	}
}

func typeName(v any) string { return fmt.Sprintf("%T", v) }

func TestDotEnvInWorkingDirSuppliesTheAPIKey(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, ".env"), []byte("ANTHROPIC_API_KEY=from-dotenv\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	wd, _ := os.Getwd()
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chdir(wd) })
	t.Setenv("ANTHROPIC_API_KEY", "")
	os.Unsetenv("ANTHROPIC_API_KEY")

	var stderr bytes.Buffer
	run([]string{"--config", filepath.Join(dir, "missing.yaml")}, &bytes.Buffer{}, &stderr, os.Getenv)
	if strings.Contains(stderr.String(), "ANTHROPIC_API_KEY is not set") {
		t.Fatalf("the key from .env was not loaded:\n%s", stderr.String())
	}
}
