package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadDotEnvSetsMissingVarsOnly(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".env")
	content := "# comment\n" +
		"GROOT_TEST_A=alpha\n" +
		"export GROOT_TEST_B=\"quoted value\"\n" +
		"GROOT_TEST_C='single' # trailing comment\n" +
		"GROOT_TEST_D=already\n" +
		"\n" +
		"not a pair\n"
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("GROOT_TEST_D", "from-shell")
	for _, k := range []string{"GROOT_TEST_A", "GROOT_TEST_B", "GROOT_TEST_C"} {
		t.Setenv(k, "")
		os.Unsetenv(k)
	}

	if err := LoadDotEnv(path); err != nil {
		t.Fatalf("LoadDotEnv: %v", err)
	}

	want := map[string]string{
		"GROOT_TEST_A": "alpha",
		"GROOT_TEST_B": "quoted value",
		"GROOT_TEST_C": "single",
		"GROOT_TEST_D": "from-shell", // the shell wins over the file
	}
	for k, v := range want {
		if got := os.Getenv(k); got != v {
			t.Errorf("%s = %q, want %q", k, got, v)
		}
	}
}

func TestLoadDotEnvMissingFileIsNotAnError(t *testing.T) {
	if err := LoadDotEnv(filepath.Join(t.TempDir(), ".env")); err != nil {
		t.Fatalf("missing file should be ignored, got %v", err)
	}
}
