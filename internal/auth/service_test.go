package auth

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestGeminiResolvesFromItsOwnEnvironmentVariable(t *testing.T) {
	c, err := Resolve(Options{Service: Gemini, EnvKey: "gem-env", Cwd: t.TempDir(), Home: t.TempDir(), Keychain: &fakeKeychain{key: "anthropic-looking"}})
	if err != nil {
		t.Fatal(err)
	}
	if c.Key != "gem-env" || c.Source != SourceEnvironment {
		t.Fatalf("got %+v, want the GEMINI_API_KEY value from the environment", c)
	}
}

func TestGeminiDotEnvReadsGeminiVariableNotAnthropic(t *testing.T) {
	dir := t.TempDir()
	writeText(t, filepath.Join(dir, ".env"), "ANTHROPIC_API_KEY=ant-key\nGEMINI_API_KEY=gem-key\n", 0o600)
	c, err := Resolve(Options{Service: Gemini, Cwd: dir, Home: filepath.Join(dir, "home")})
	if err != nil {
		t.Fatal(err)
	}
	if c.Key != "gem-key" || c.Source != SourceDotEnv {
		t.Fatalf("got %+v, want gem-key from .env", c)
	}
}

func TestSystemKeychainForUsesTheServiceItem(t *testing.T) {
	var calls [][]string
	run := func(name string, args ...string) ([]byte, int, error) {
		calls = append(calls, append([]string{name}, args...))
		return []byte("gem-key\n"), 0, nil
	}
	key, err := SystemKeychainFor(Gemini, "darwin", run).Get()
	if err != nil || key != "gem-key" {
		t.Fatalf("Get = %q, %v", key, err)
	}
	if got := strings.Join(calls[0], " "); !strings.Contains(got, "-s groot.gemini") {
		t.Fatalf("security call %q does not address the Gemini item", got)
	}
}

func TestGeminiLoginKeepsTheAnthropicFieldInTheSameFile(t *testing.T) {
	home := t.TempDir()
	if _, err := LoginFor(Anthropic, home, nil, "ant-key-123"); err != nil {
		t.Fatal(err)
	}
	where, err := LoginFor(Gemini, home, nil, "gem-key-456")
	if err != nil {
		t.Fatal(err)
	}
	if where.Store != StoreFile || where.Path != CredentialsPath(home) {
		t.Fatalf("where = %+v, want the credentials file", where)
	}
	raw, err := os.ReadFile(CredentialsPath(home))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), "anthropic_api_key: ant-key-123") || !strings.Contains(string(raw), "gemini_api_key: gem-key-456") {
		t.Fatalf("credentials file lost a field:\n%s", raw)
	}
	fi, err := os.Stat(CredentialsPath(home))
	if err != nil {
		t.Fatal(err)
	}
	if fi.Mode().Perm() != 0o600 {
		t.Fatalf("mode = %v, want 0600", fi.Mode().Perm())
	}
}

func TestGeminiLogoutRemovesOnlyItsField(t *testing.T) {
	home := t.TempDir()
	if _, err := LoginFor(Anthropic, home, nil, "ant-key-123"); err != nil {
		t.Fatal(err)
	}
	if _, err := LoginFor(Gemini, home, nil, "gem-key-456"); err != nil {
		t.Fatal(err)
	}
	removed, err := LogoutFor(Gemini, home, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(removed) != 1 {
		t.Fatalf("removed = %v, want the credentials file once", removed)
	}
	c, err := Resolve(Options{Cwd: t.TempDir(), Home: home})
	if err != nil {
		t.Fatal(err)
	}
	if c.Key != "ant-key-123" {
		t.Fatalf("Anthropic key = %q after Gemini logout, want it kept", c.Key)
	}
	c, err = Resolve(Options{Service: Gemini, Cwd: t.TempDir(), Home: home})
	if err != nil {
		t.Fatal(err)
	}
	if c.Key != "" {
		t.Fatalf("Gemini key %q still resolves after logout", c.Key)
	}
}

func TestAnthropicResolveDefaultsWhenServiceIsZero(t *testing.T) {
	c, err := Resolve(Options{EnvKey: "from-env", Cwd: t.TempDir(), Home: t.TempDir()})
	if err != nil || c.Key != "from-env" {
		t.Fatalf("zero Service must mean Anthropic: %+v, %v", c, err)
	}
}
