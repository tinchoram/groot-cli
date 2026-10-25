package workspace

import (
	"os"
	"path/filepath"
	"testing"
)

// Writes from the admin tools and /model --default go to the project file when
// it exists, otherwise to the global file in the groot home.
func TestConfigFileTargetsProjectWhenItExistsElseGlobal(t *testing.T) {
	dir := t.TempDir()
	global := filepath.Join(dir, "home", "groot.yaml")
	project := filepath.Join(dir, "project", "groot.yaml")
	w := &Workspace{opts: Options{ConfigPath: project, GlobalConfigPath: global}}

	if got := w.ConfigFile(); got != global {
		t.Fatalf("no project file: want the global file %s, got %s", global, got)
	}

	if err := os.MkdirAll(filepath.Dir(project), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(project, []byte("max_turns: 3\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := w.ConfigFile(); got != project {
		t.Fatalf("project file exists: want %s, got %s", project, got)
	}
}

// Without a global path (tests and embedded use), the project path is kept.
func TestConfigFileWithoutGlobalKeepsProjectPath(t *testing.T) {
	project := filepath.Join(t.TempDir(), "groot.yaml")
	w := &Workspace{opts: Options{ConfigPath: project}}
	if got := w.ConfigFile(); got != project {
		t.Fatalf("got %s", got)
	}
}
