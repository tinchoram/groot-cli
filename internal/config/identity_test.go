package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestNormalizeUserName(t *testing.T) {
	tests := []struct {
		name    string
		in      string
		want    string
		wantErr bool
	}{
		{"plain", "Tinchoram", "Tinchoram", false},
		{"with at sign", "@Tinchoram", "Tinchoram", false},
		{"surrounding spaces", "  @ops_team-1 ", "ops_team-1", false},
		{"minimum length", "ab", "ab", false},
		{"maximum length", strings.Repeat("a", 32), strings.Repeat("a", 32), false},
		{"too short", "a", "", true},
		{"too long", strings.Repeat("a", 33), "", true},
		{"dot is not allowed", "tin.cho", "", true},
		{"space inside", "Martin Ramos", "", true},
		{"empty", "", "", true},
		{"only at sign", "@", "", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := NormalizeUserName(tt.in)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("NormalizeUserName(%q) accepted an invalid name as %q", tt.in, got)
				}
				if !strings.Contains(err.Error(), "user.name must be 2-32 letters, digits, _ or -") {
					t.Fatalf("error = %q, want the user.name rule", err)
				}
				return
			}
			if err != nil {
				t.Fatalf("NormalizeUserName(%q) error: %v", tt.in, err)
			}
			if got != tt.want {
				t.Fatalf("NormalizeUserName(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

func TestUserNameFromGlobalConfigIsNormalized(t *testing.T) {
	dir := t.TempDir()
	global := writeLayer(t, dir, "groot.yaml", "user:\n  name: \"@Tinchoram\"\n")

	cfg, err := LoadWithUser(global, filepath.Join(dir, "user.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.User.Name != "Tinchoram" {
		t.Fatalf("user.name = %q, want the name without @", cfg.User.Name)
	}
}

func TestUserYAMLOverridesGlobalUserName(t *testing.T) {
	dir := t.TempDir()
	global := writeLayer(t, dir, "groot.yaml", "user:\n  name: Martin\nprovider:\n  model: global-model\n")
	user := writeLayer(t, dir, "user.yaml", "name: Tinchoram\n")
	project := writeLayer(t, dir, "project.yaml", "provider:\n  model: project-model\n")

	cfg, err := LoadWithUser(global, user, project)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.User.Name != "Tinchoram" {
		t.Fatalf("user.yaml must override groot.yaml, got %q", cfg.User.Name)
	}
	if cfg.Provider.Model != "project-model" {
		t.Fatalf("project files still override the global ones, got %q", cfg.Provider.Model)
	}
}

func TestMissingUserYAMLKeepsGlobalUserName(t *testing.T) {
	dir := t.TempDir()
	global := writeLayer(t, dir, "groot.yaml", "user:\n  name: Tinchoram\n")

	cfg, err := LoadWithUser(global, filepath.Join(dir, "absent.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.User.Name != "Tinchoram" {
		t.Fatalf("a missing user.yaml must not clear the name, got %q", cfg.User.Name)
	}
}

func TestInvalidUserYAMLNameIsRejected(t *testing.T) {
	dir := t.TempDir()
	global := writeLayer(t, dir, "groot.yaml", "")
	user := writeLayer(t, dir, "user.yaml", "name: \"no way\"\n")

	if _, err := LoadWithUser(global, user); err == nil || !strings.Contains(err.Error(), "user.name must be 2-32 letters") {
		t.Fatalf("invalid name in user.yaml: error = %v", err)
	}
}

func TestProjectFileCannotSetUserName(t *testing.T) {
	dir := t.TempDir()
	global := writeLayer(t, dir, "groot.yaml", "")
	user := writeLayer(t, dir, "user.yaml", "")
	project := writeLayer(t, dir, "project.yaml", "user:\n  name: Intruder\n")

	_, err := LoadWithUser(global, user, project)
	if err == nil {
		t.Fatal("a project file must not set user.name")
	}
	if !strings.Contains(err.Error(), "user.name can only be set in the global config") {
		t.Fatalf("error = %q", err)
	}
}

func TestProjectPathThatIsTheGlobalFileIsNotRejected(t *testing.T) {
	// Running groot from the groot home makes the project file the global file.
	dir := t.TempDir()
	global := writeLayer(t, dir, "groot.yaml", "user:\n  name: Tinchoram\n")
	user := writeLayer(t, dir, "user.yaml", "")

	cfg, err := LoadWithUser(global, user, global)
	if err != nil {
		t.Fatalf("the global file read as the project must not be refused: %v", err)
	}
	if cfg.User.Name != "Tinchoram" {
		t.Fatalf("user.name = %q", cfg.User.Name)
	}
}

func TestSaveUserNameWritesOnlyTheName(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "user.yaml")

	if err := SaveUserName(path, "@Tinchoram"); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(raw) != "name: Tinchoram\n" {
		t.Fatalf("user.yaml = %q, want only the name", raw)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Fatalf("mode = %o, want 0600", perm)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		t.Fatalf("atomic write must leave no temp file behind, found %d entries", len(entries))
	}
}

func TestSaveUserNameReplacesThePreviousValue(t *testing.T) {
	dir := t.TempDir()
	path := writeLayer(t, dir, "user.yaml", "name: Martin\n")

	if err := SaveUserName(path, "Tinchoram"); err != nil {
		t.Fatal(err)
	}
	cfg, err := LoadWithUser(writeLayer(t, dir, "groot.yaml", ""), path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.User.Name != "Tinchoram" {
		t.Fatalf("name = %q after save", cfg.User.Name)
	}
}

func TestSaveUserNameRefusesInvalidNameAndKeepsFile(t *testing.T) {
	dir := t.TempDir()
	path := writeLayer(t, dir, "user.yaml", "name: Martin\n")

	if err := SaveUserName(path, "x"); err == nil {
		t.Fatal("an invalid name must not be saved")
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(raw) != "name: Martin\n" {
		t.Fatalf("file changed after a refused save: %q", raw)
	}
}

func TestUserFileSitsNextToGlobalConfig(t *testing.T) {
	if got := UserFile("/home/u/.groot/groot.yaml"); got != filepath.Join("/home/u/.groot", "user.yaml") {
		t.Fatalf("UserFile = %q", got)
	}
}
