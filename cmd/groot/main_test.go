package main

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tinchoram/groot-cli/internal/auth"
)

// fakeKeychain stands in for the OS keychain. Tests never touch the real one.
type fakeKeychain struct{ key string }

func (f *fakeKeychain) Get() (string, error) {
	if f.key == "" {
		return "", auth.ErrNotFound
	}
	return f.key, nil
}

func (f *fakeKeychain) Set(key string) error { f.key = key; return nil }

func (f *fakeKeychain) Delete() error {
	if f.key == "" {
		return auth.ErrNotFound
	}
	f.key = ""
	return nil
}

// isolate points groot at a temporary home (GROOT_HOME), a fake keychain, and a
// temporary working directory with no .env. key, when set, is the environment
// variable ANTHROPIC_API_KEY.
func isolate(t *testing.T, key string) (func(string) string, *fakeKeychain) {
	t.Helper()
	home := t.TempDir()
	kc := &fakeKeychain{}
	prev := newKeychain
	newKeychain = func() auth.Keychain { return kc }
	t.Cleanup(func() { newKeychain = prev })
	t.Chdir(t.TempDir())
	getenv := func(k string) string {
		switch k {
		case "GROOT_HOME":
			return home
		case auth.EnvVar:
			return key
		}
		return ""
	}
	return getenv, kc
}

func TestVersionFlagPrintsAndExitsZero(t *testing.T) {
	getenv, _ := isolate(t, "")
	var out, errOut bytes.Buffer
	code := run([]string{"--version"}, nil, &out, &errOut, getenv)
	if code != 0 {
		t.Fatalf("exit %d, stderr %q", code, errOut.String())
	}
	if !strings.HasPrefix(out.String(), "groot ") {
		t.Fatalf("unexpected version output %q", out.String())
	}
}

func TestMissingAPIKeyNamesLoginFirstAndExitsNonZero(t *testing.T) {
	getenv, _ := isolate(t, "")
	var out, errOut bytes.Buffer
	code := run([]string{"--config", "does-not-exist.yaml"}, nil, &out, &errOut, getenv)
	if code == 0 {
		t.Fatal("missing key must fail")
	}
	msg := errOut.String()
	for _, want := range []string{"groot auth login", "ANTHROPIC_API_KEY", "export"} {
		if !strings.Contains(msg, want) {
			t.Fatalf("hint should name %q: %q", want, msg)
		}
	}
	if strings.Contains(msg, "panic") {
		t.Fatal("must not panic")
	}
}

func TestExplicitConfigMustExist(t *testing.T) {
	getenv, _ := isolate(t, "test-key-not-real")
	var out, errOut bytes.Buffer
	code := run([]string{"--config", "/nonexistent/groot.yaml"}, nil, &out, &errOut, getenv)
	if code == 0 || !strings.Contains(errOut.String(), "groot.yaml") {
		t.Fatalf("explicit missing config must fail clearly: code=%d err=%q", code, errOut.String())
	}
}

func TestInvalidPolicyFlagIsRejected(t *testing.T) {
	getenv, _ := isolate(t, "test-key-not-real")
	var out, errOut bytes.Buffer
	code := run([]string{"--policy", "yolo", "--config", "/nonexistent.yaml"}, nil, &out, &errOut, getenv)
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
	getenv, _ := isolate(t, "")
	dir, _ := os.Getwd()
	if err := os.WriteFile(filepath.Join(dir, ".env"), []byte("ANTHROPIC_API_KEY=from-dotenv\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	var stderr bytes.Buffer
	run([]string{"--config", filepath.Join(dir, "missing.yaml")}, nil, &bytes.Buffer{}, &stderr, getenv)
	if strings.Contains(stderr.String(), "no Anthropic API key") {
		t.Fatalf("the key from .env was not used:\n%s", stderr.String())
	}
}

// The project file overrides the global file, which overrides the defaults.
func TestLoadConfigLayersGlobalUnderProject(t *testing.T) {
	dir := t.TempDir()
	global := filepath.Join(dir, "home", "groot.yaml")
	project := filepath.Join(dir, "project", "groot.yaml")
	writeTestFile(t, global, "gate:\n  policy: readonly\nprovider:\n  model: global-model\n")
	writeTestFile(t, project, "provider:\n  model: project-model\n")

	cfg, err := loadConfig(false, project, global)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Provider.Model != "project-model" || cfg.Gate.Policy != "readonly" {
		t.Fatalf("got model %q policy %q", cfg.Provider.Model, cfg.Gate.Policy)
	}
}

// Without a project file, the global file alone applies.
func TestLoadConfigGlobalOnlyWhenNoProjectFile(t *testing.T) {
	dir := t.TempDir()
	global := filepath.Join(dir, "home", "groot.yaml")
	writeTestFile(t, global, "gate:\n  policy: auto\n")

	cfg, err := loadConfig(false, filepath.Join(dir, "groot.yaml"), global)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Gate.Policy != "auto" {
		t.Fatalf("got policy %q", cfg.Gate.Policy)
	}
}

func writeTestFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}
