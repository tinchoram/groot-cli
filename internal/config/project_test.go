package config

import (
	"strings"
	"testing"
)

func TestProjectFileAllowsListedKeys(t *testing.T) {
	dir := t.TempDir()
	global := writeLayer(t, dir, "global.yaml", "provider:\n  model: global-model\n")
	project := writeLayer(t, dir, "project.yaml", strings.Join([]string{
		"max_turns: 9",
		"provider:",
		"  model: project-model",
		"memory:",
		"  path: .groot/project.db",
		"flow:",
		"  mode: advisory",
		"work:",
		"  max_usd: 2",
		"ui:",
		"  show_cost: true",
		"",
	}, "\n"))

	cfg, err := LoadLayers(global, project)
	if err != nil {
		t.Fatalf("allowed keys must load from a project file: %v", err)
	}
	if cfg.MaxTurns != 9 || cfg.Provider.Model != "project-model" || cfg.Memory.Path != ".groot/project.db" {
		t.Fatalf("allowed scalars must apply, got max_turns=%d model=%q memory=%q", cfg.MaxTurns, cfg.Provider.Model, cfg.Memory.Path)
	}
	if cfg.Flow.Mode != "advisory" || cfg.Work.MaxUSD != 2 || !cfg.UI.ShowCost {
		t.Fatalf("allowed sections must apply, got flow=%q work=%v ui=%v", cfg.Flow.Mode, cfg.Work.MaxUSD, cfg.UI.ShowCost)
	}
}

func TestProjectFileRejectsSecuritySettings(t *testing.T) {
	tests := []struct {
		name string
		body string
		key  string
	}{
		{name: "audit sinks", body: "audit:\n  sinks:\n    - type: http\n      url: https://collector.example/x\n", key: "audit.sinks"},
		{name: "bash sandbox mode", body: "bash:\n  sandbox:\n    mode: off\n", key: "bash.sandbox.mode"},
		{name: "fetch allow_private", body: "fetch:\n  allow_private: true\n", key: "fetch.allow_private"},
		{name: "guard mode", body: "guard:\n  mode: off\n", key: "guard.mode"},
		{name: "updates check", body: "updates:\n  check: false\n", key: "updates.check"},
		{name: "work always_ask", body: "work:\n  always_ask: []\n", key: "work.always_ask"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			project := writeLayer(t, dir, "project.yaml", tt.body)

			_, err := LoadLayers(writeLayer(t, dir, "global.yaml", ""), project)
			want := project + ": " + tt.key + " can only be set in the global config (~/.groot/groot.yaml): a project file cannot change security settings"
			if err == nil || err.Error() != want {
				t.Fatalf("want error %q, got %v", want, err)
			}
		})
	}
}

func TestProjectFileRejectsUnknownKeys(t *testing.T) {
	tests := []struct {
		name string
		body string
		key  string
	}{
		{name: "top level", body: "frobnicate: 1\n", key: "frobnicate"},
		{name: "inside an allowed section", body: "work:\n  max_turnz: 3\n", key: "work.max_turnz"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			project := writeLayer(t, dir, "project.yaml", tt.body)

			_, err := LoadLayers(writeLayer(t, dir, "global.yaml", ""), project)
			want := project + ": unknown key " + tt.key + " in a project config"
			if err == nil || err.Error() != want {
				t.Fatalf("want error %q, got %v", want, err)
			}
		})
	}
}

func TestProjectFileCannotReferenceEnvironment(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("GROOT_PROJECT_ENV_TEST", "from-env")
	global := writeLayer(t, dir, "global.yaml", "memory:\n  path: ${GROOT_PROJECT_ENV_TEST}/global.db\n")
	project := writeLayer(t, dir, "project.yaml", "memory:\n  path: ${HOME}/x\n")

	_, err := LoadLayers(global, project)
	want := project + ": memory.path: project configs cannot reference environment variables"
	if err == nil || err.Error() != want {
		t.Fatalf("want error %q, got %v", want, err)
	}
}

func TestGlobalFileStillExpandsEnvironment(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("GROOT_PROJECT_ENV_TEST", "from-env")
	global := writeLayer(t, dir, "global.yaml", "memory:\n  path: ${GROOT_PROJECT_ENV_TEST}/global.db\n")

	cfg, err := LoadLayers(global)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Memory.Path != "from-env/global.db" {
		t.Fatalf("the global file must expand env references, got %q", cfg.Memory.Path)
	}
}
