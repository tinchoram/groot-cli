package config

import (
	"os"
	"path/filepath"
	"testing"
)

func writeLayer(t *testing.T, dir, name, content string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestLoadLayersLaterFileOverridesScalarsOverDefaults(t *testing.T) {
	dir := t.TempDir()
	global := writeLayer(t, dir, "global.yaml", "gate:\n  policy: readonly\nprovider:\n  model: global-model\n")
	project := writeLayer(t, dir, "project.yaml", "provider:\n  model: project-model\n")

	cfg, err := LoadLayers(global, project)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Provider.Model != "project-model" {
		t.Fatalf("project scalar must win, got %q", cfg.Provider.Model)
	}
	if cfg.Gate.Policy != "readonly" {
		t.Fatalf("global value must survive when the project does not set it, got %q", cfg.Gate.Policy)
	}
	if cfg.MaxTurns != Default().MaxTurns {
		t.Fatalf("defaults must apply under both files, got max_turns %d", cfg.MaxTurns)
	}
}

func TestLoadLayersReplacesListsWhole(t *testing.T) {
	dir := t.TempDir()
	global := writeLayer(t, dir, "global.yaml", "kubectl:\n  namespaces: [a, b]\n")
	project := writeLayer(t, dir, "project.yaml", "kubectl:\n  namespaces: [c]\n")

	cfg, err := LoadLayers(global, project)
	if err != nil {
		t.Fatal(err)
	}
	if got := cfg.Kubectl.Namespaces; len(got) != 1 || got[0] != "c" {
		t.Fatalf("a list in the project file replaces the global list whole, got %v", got)
	}
}

func TestLoadLayersSkipsMissingFiles(t *testing.T) {
	dir := t.TempDir()
	project := writeLayer(t, dir, "project.yaml", "gate:\n  policy: auto\n")

	cfg, err := LoadLayers(filepath.Join(dir, "absent.yaml"), project)
	if err != nil {
		t.Fatalf("a missing layer is not an error: %v", err)
	}
	if cfg.Gate.Policy != "auto" {
		t.Fatalf("got policy %q", cfg.Gate.Policy)
	}
}

func TestLoadLayersExpandsEnvPerFile(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("GROOT_LAYER_TEST", "from-env")
	global := writeLayer(t, dir, "global.yaml", "provider:\n  model: ${GROOT_LAYER_TEST}\n")

	cfg, err := LoadLayers(global)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Provider.Model != "from-env" {
		t.Fatalf("env expansion must apply to each layer, got %q", cfg.Provider.Model)
	}
}

func TestLoadLayersValidatesTheMergedResult(t *testing.T) {
	dir := t.TempDir()
	global := writeLayer(t, dir, "global.yaml", "gate:\n  policy: yolo\n")

	if _, err := LoadLayers(global); err == nil {
		t.Fatal("an invalid policy in any layer must fail validation")
	}
}

func TestLoadIsOneLayer(t *testing.T) {
	dir := t.TempDir()
	path := writeLayer(t, dir, "one.yaml", "max_turns: 7\n")
	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.MaxTurns != 7 {
		t.Fatalf("got %d", cfg.MaxTurns)
	}
}

func TestDotEnvValueReadsOneKeyWithoutTouchingTheEnvironment(t *testing.T) {
	dir := t.TempDir()
	path := writeLayer(t, dir, ".env", "# c\nexport ANTHROPIC_API_KEY=\"sk-file\"\nOTHER=1\n")
	t.Setenv("ANTHROPIC_API_KEY", "")
	os.Unsetenv("ANTHROPIC_API_KEY")

	got, found, err := DotEnvValue(path, "ANTHROPIC_API_KEY")
	if err != nil {
		t.Fatal(err)
	}
	if !found || got != "sk-file" {
		t.Fatalf("got %q found=%v", got, found)
	}
	if _, set := os.LookupEnv("ANTHROPIC_API_KEY"); set {
		t.Fatal("DotEnvValue must not set the environment")
	}
	if _, found, _ := DotEnvValue(path, "MISSING"); found {
		t.Fatal("a key the file does not set is not found")
	}
	if _, found, err := DotEnvValue(filepath.Join(dir, "absent"), "X"); found || err != nil {
		t.Fatalf("a missing file is not an error: found=%v err=%v", found, err)
	}
}
