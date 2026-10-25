package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeFile(t *testing.T, name, content string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestDefaultsWhenFileMissing(t *testing.T) {
	cfg, err := Load(filepath.Join(t.TempDir(), "groot.yaml"))
	if err != nil {
		t.Fatalf("missing file must not fail: %v", err)
	}
	if cfg.Provider.Name != "anthropic" || cfg.Provider.Model != "" {
		t.Errorf("provider defaults wrong: %+v", cfg.Provider)
	}
	if cfg.MaxTurns != 40 {
		t.Errorf("max_turns default = %d", cfg.MaxTurns)
	}
	if cfg.Compaction.Strategy != "none" {
		t.Errorf("compaction default = %q", cfg.Compaction.Strategy)
	}
	if cfg.Memory.Path != ".groot/groot.db" || cfg.Audit.Path != ".groot/audit.jsonl" {
		t.Errorf("path defaults wrong: memory=%q audit=%q", cfg.Memory.Path, cfg.Audit.Path)
	}
	if cfg.Gate.Policy != "interactive" {
		t.Errorf("gate default = %q", cfg.Gate.Policy)
	}
	if len(cfg.Kubectl.Namespaces) != 0 {
		t.Errorf("namespaces should default to empty (any)")
	}
	if cfg.EffectiveSystemPrompt() != DefaultSystemPrompt {
		t.Errorf("effective prompt should be the default")
	}
}

func TestFileOverridesDefaults(t *testing.T) {
	p := writeFile(t, "groot.yaml", `
provider:
  name: anthropic
  model: claude-haiku-5-5
max_turns: 7
compaction:
  strategy: window
  keep: 12
gate:
  policy: readonly
kubectl:
  namespaces: [prod, staging]
system_prompt: You are a custom SRE assistant.
`)
	cfg, err := Load(p)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Provider.Model != "claude-haiku-5-5" || cfg.MaxTurns != 7 {
		t.Errorf("overrides not applied: %+v", cfg)
	}
	if cfg.Compaction.Strategy != "window" || cfg.Compaction.Keep != 12 {
		t.Errorf("compaction not applied: %+v", cfg.Compaction)
	}
	if cfg.Gate.Policy != "readonly" {
		t.Errorf("gate not applied")
	}
	if strings.Join(cfg.Kubectl.Namespaces, ",") != "prod,staging" {
		t.Errorf("namespaces = %v", cfg.Kubectl.Namespaces)
	}
	if cfg.EffectiveSystemPrompt() != "You are a custom SRE assistant." {
		t.Errorf("system prompt override not used")
	}
	// Untouched fields keep their defaults.
	if cfg.Audit.Path != ".groot/audit.jsonl" {
		t.Errorf("audit default lost: %q", cfg.Audit.Path)
	}
}

func TestEnvExpansion(t *testing.T) {
	t.Setenv("GROOT_TEST_NS", "prod")
	t.Setenv("GROOT_TEST_EMPTY", "")
	os.Unsetenv("GROOT_TEST_MISSING")

	p := writeFile(t, "groot.yaml", `
kubectl:
  namespaces:
    - ${GROOT_TEST_NS}
    - ${GROOT_TEST_MISSING:-kube-system}
    - ${GROOT_TEST_EMPTY:-fallback}
memory:
  path: ${GROOT_TEST_MISSING:-.groot/custom.db}
provider:
  model: ${GROOT_TEST_NS}-model
`)
	cfg, err := Load(p)
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(cfg.Kubectl.Namespaces, ","); got != "prod,kube-system,fallback" {
		t.Errorf("namespaces = %q", got)
	}
	if cfg.Memory.Path != ".groot/custom.db" {
		t.Errorf("memory path = %q", cfg.Memory.Path)
	}
	if cfg.Provider.Model != "prod-model" {
		t.Errorf("model = %q", cfg.Provider.Model)
	}
}

func TestExpandHelper(t *testing.T) {
	lookup := func(k string) (string, bool) {
		if k == "A" {
			return "alpha", true
		}
		return "", false
	}
	cases := map[string]string{
		"${A}":               "alpha",
		"${B:-beta}":         "beta",
		"${B}":               "",
		"x-${A}-${B:-y}-z":   "x-alpha-y-z",
		"no vars":            "no vars",
		"$A is not expanded": "$A is not expanded",
	}
	for in, want := range cases {
		if got := Expand(in, lookup); got != want {
			t.Errorf("Expand(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestValidationRejectsBadValues(t *testing.T) {
	bad := map[string]string{
		"unknown gate policy":     "gate:\n  policy: yolo\n",
		"unknown compaction":      "compaction:\n  strategy: magic\n",
		"unsupported provider":    "provider:\n  name: openai\n",
		"zero max turns":          "max_turns: 0\n",
		"window without keep":     "compaction:\n  strategy: window\n  keep: 0\n",
		"summarize without limit": "compaction:\n  strategy: summarize\n  threshold: 0\n",
	}
	for name, content := range bad {
		t.Run(name, func(t *testing.T) {
			if _, err := Load(writeFile(t, "groot.yaml", content)); err == nil {
				t.Fatal("expected a validation error")
			}
		})
	}
}

func TestInvalidYAMLFails(t *testing.T) {
	if _, err := Load(writeFile(t, "groot.yaml", "provider: [unclosed\n")); err == nil {
		t.Fatal("expected a YAML error")
	}
}

func TestLoadProjectContext(t *testing.T) {
	dir := t.TempDir()
	if got, err := LoadProjectContext(dir); err != nil || got != "" {
		t.Fatalf("missing AGENTS.md should yield empty context, got %q %v", got, err)
	}
	if err := os.WriteFile(filepath.Join(dir, "AGENTS.md"), []byte("Cluster: prod-eu\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := LoadProjectContext(dir)
	if err != nil || got != "Cluster: prod-eu" {
		t.Fatalf("got %q %v", got, err)
	}
}

func TestComposeSystemPrompt(t *testing.T) {
	out := ComposeSystemPrompt("BASE", "- [fact] db is pg-1\n", "Cluster: prod-eu")
	for _, want := range []string{"BASE", "db is pg-1", "## Project context", "Cluster: prod-eu"} {
		if !strings.Contains(out, want) {
			t.Errorf("composed prompt missing %q:\n%s", want, out)
		}
	}
	if strings.Contains(ComposeSystemPrompt("BASE", "", ""), "Project context") {
		t.Error("empty project context must not add a header")
	}
}

func TestEmptyNamespaceExpansionMeansAnyNamespace(t *testing.T) {
	os.Unsetenv("GROOT_TEST_UNSET_NS")
	p := writeFile(t, "groot.yaml", "kubectl:\n  namespaces: [\"${GROOT_TEST_UNSET_NS:-}\"]\n")
	cfg, err := Load(p)
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.Kubectl.Namespaces) != 0 {
		t.Fatalf("an empty expansion must not become an allowlist entry: %q", cfg.Kubectl.Namespaces)
	}
}

func TestShippedExampleLoads(t *testing.T) {
	cfg, err := Load(filepath.Join("..", "..", "groot.yaml.example"))
	if err != nil {
		t.Fatalf("groot.yaml.example must stay valid: %v", err)
	}
	if cfg.Provider.Model == "" || cfg.Gate.Policy != "interactive" {
		t.Fatalf("unexpected example values: %+v", cfg)
	}
}

func TestLibrarySourcesParse(t *testing.T) {
	p := writeFile(t, "groot.yaml", `
library:
  sources:
    - name: "@tinchoram"
      url: https://example.com/index.yaml
`)
	cfg, err := Load(p)
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.Library.Sources) != 1 || cfg.Library.Sources[0].Name != "@tinchoram" || cfg.Library.Sources[0].URL != "https://example.com/index.yaml" {
		t.Fatalf("sources = %+v", cfg.Library.Sources)
	}
}

func TestLibrarySourcesDefaultEmpty(t *testing.T) {
	cfg, err := Load(filepath.Join(t.TempDir(), "absent.yaml"))
	if err != nil || len(cfg.Library.Sources) != 0 {
		t.Fatalf("default sources = %+v (%v)", cfg.Library.Sources, err)
	}
}

func TestLibrarySourcesValidation(t *testing.T) {
	bad := map[string]string{
		"missing name":   "library:\n  sources:\n    - url: https://example.com/i.yaml\n",
		"missing url":    "library:\n  sources:\n    - name: \"@x\"\n",
		"file scheme":    "library:\n  sources:\n    - name: \"@x\"\n      url: file:///etc/passwd\n",
		"duplicate name": "library:\n  sources:\n    - name: \"@x\"\n      url: https://a.example/i.yaml\n    - name: \"@x\"\n      url: https://b.example/i.yaml\n",
	}
	for label, content := range bad {
		if _, err := Load(writeFile(t, "groot.yaml", content)); err == nil {
			t.Errorf("%s: expected a validation error", label)
		}
	}
}

func TestFetchConfigParses(t *testing.T) {
	cfg, err := Load(writeFile(t, "groot.yaml", "fetch:\n  allow: [docs.example.com]\n  deny: [internal.example.com]\n  allow_private: true\n"))
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.Fetch.Allow) != 1 || cfg.Fetch.Allow[0] != "docs.example.com" ||
		len(cfg.Fetch.Deny) != 1 || cfg.Fetch.Deny[0] != "internal.example.com" || !cfg.Fetch.AllowPrivate {
		t.Fatalf("fetch = %+v", cfg.Fetch)
	}
	def, err := Load(filepath.Join(t.TempDir(), "absent.yaml"))
	if err != nil || def.Fetch.AllowPrivate || len(def.Fetch.Allow) != 0 || len(def.Fetch.Deny) != 0 {
		t.Fatalf("fetch defaults = %+v (%v)", def.Fetch, err)
	}
}
