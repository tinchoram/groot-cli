package auth

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// fakeKeychain stands in for the macOS keychain. It never touches the real one.
type fakeKeychain struct {
	key    string
	getErr error
	setErr error
	delErr error
}

func (f *fakeKeychain) Get() (string, error) {
	if f.getErr != nil {
		return "", f.getErr
	}
	if f.key == "" {
		return "", ErrNotFound
	}
	return f.key, nil
}

func (f *fakeKeychain) Set(key string) error {
	if f.setErr != nil {
		return f.setErr
	}
	f.key = key
	return nil
}

func (f *fakeKeychain) Delete() error {
	if f.delErr != nil {
		return f.delErr
	}
	if f.key == "" {
		return ErrNotFound
	}
	f.key = ""
	return nil
}

func writeText(t *testing.T, path, content string, mode os.FileMode) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), mode); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, mode); err != nil {
		t.Fatal(err)
	}
}

func TestResolveEnvironmentWinsOverEverything(t *testing.T) {
	dir := t.TempDir()
	home := filepath.Join(dir, "home")
	cwd := filepath.Join(dir, "cwd")
	writeText(t, filepath.Join(cwd, ".env"), "ANTHROPIC_API_KEY=from-dotenv\n", 0o600)
	writeText(t, filepath.Join(home, "credentials"), "anthropic_api_key: from-file\n", 0o600)

	c, err := Resolve(Options{EnvKey: "from-env", Cwd: cwd, Home: home, Keychain: &fakeKeychain{key: "from-keychain"}})
	if err != nil {
		t.Fatal(err)
	}
	if c.Key != "from-env" || c.Source != SourceEnvironment {
		t.Fatalf("got %q from %q", c.Key, c.Source)
	}
}

func TestResolveDotEnvComesAfterEnvironment(t *testing.T) {
	dir := t.TempDir()
	cwd := filepath.Join(dir, "cwd")
	writeText(t, filepath.Join(cwd, ".env"), "ANTHROPIC_API_KEY=from-dotenv\n", 0o600)

	c, err := Resolve(Options{Cwd: cwd, Home: filepath.Join(dir, "home"), Keychain: &fakeKeychain{key: "from-keychain"}})
	if err != nil {
		t.Fatal(err)
	}
	if c.Key != "from-dotenv" || c.Source != SourceDotEnv {
		t.Fatalf("got %q from %q", c.Key, c.Source)
	}
	if got := Label(c); got != ".env in "+cwd {
		t.Fatalf("label %q", got)
	}
}

func TestResolveKeychainBeforeCredentialsFile(t *testing.T) {
	dir := t.TempDir()
	home := filepath.Join(dir, "home")
	writeText(t, filepath.Join(home, "credentials"), "anthropic_api_key: from-file\n", 0o600)

	c, err := Resolve(Options{Cwd: dir, Home: home, Keychain: &fakeKeychain{key: "from-keychain"}})
	if err != nil {
		t.Fatal(err)
	}
	if c.Key != "from-keychain" || c.Source != SourceKeychain {
		t.Fatalf("got %q from %q", c.Key, c.Source)
	}
}

func TestResolveCredentialsFileWhenNoKeychain(t *testing.T) {
	dir := t.TempDir()
	home := filepath.Join(dir, "home")
	writeText(t, filepath.Join(home, "credentials"), "anthropic_api_key: from-file\n", 0o600)

	c, err := Resolve(Options{Cwd: dir, Home: home, Keychain: &fakeKeychain{getErr: ErrUnavailable}})
	if err != nil {
		t.Fatal(err)
	}
	if c.Key != "from-file" || c.Source != SourceCredentials {
		t.Fatalf("got %q from %q", c.Key, c.Source)
	}
	if len(c.Warnings) != 0 {
		t.Fatalf("an unavailable keychain is not worth a warning: %v", c.Warnings)
	}
}

func TestResolveKeychainErrorFallsThroughWithWarning(t *testing.T) {
	dir := t.TempDir()
	home := filepath.Join(dir, "home")
	writeText(t, filepath.Join(home, "credentials"), "anthropic_api_key: from-file\n", 0o600)

	c, err := Resolve(Options{Cwd: dir, Home: home, Keychain: &fakeKeychain{getErr: errors.New("security broke")}})
	if err != nil {
		t.Fatal(err)
	}
	if c.Source != SourceCredentials {
		t.Fatalf("got source %q", c.Source)
	}
	if len(c.Warnings) != 1 {
		t.Fatalf("want one warning, got %v", c.Warnings)
	}
}

func TestResolveNoneWhenNothingIsStored(t *testing.T) {
	dir := t.TempDir()
	c, err := Resolve(Options{Cwd: dir, Home: filepath.Join(dir, "home"), Keychain: &fakeKeychain{}})
	if err != nil {
		t.Fatal(err)
	}
	if c.Key != "" || c.Source != SourceNone {
		t.Fatalf("got %q from %q", c.Key, c.Source)
	}
	if Label(c) != "none" {
		t.Fatalf("label %q", Label(c))
	}
}

func TestCredentialsFileWiderThanOwnerIsIgnoredWithWarning(t *testing.T) {
	dir := t.TempDir()
	home := filepath.Join(dir, "home")
	writeText(t, filepath.Join(home, "credentials"), "anthropic_api_key: leaked\n", 0o644)

	c, err := Resolve(Options{Cwd: dir, Home: home})
	if err != nil {
		t.Fatal(err)
	}
	if c.Key != "" || c.Source != SourceNone {
		t.Fatalf("a world-readable file must not be used, got %q from %q", c.Key, c.Source)
	}
	if len(c.Warnings) != 1 || !strings.Contains(c.Warnings[0], "0644") {
		t.Fatalf("want a permissions warning, got %v", c.Warnings)
	}
}

func TestSaveWritesCredentialsFileWith0600(t *testing.T) {
	home := filepath.Join(t.TempDir(), "home")
	path, err := storeInFile(home, Anthropic, "sk-ant-test-1234")
	if err != nil {
		t.Fatal(err)
	}
	if path != filepath.Join(home, "credentials") {
		t.Fatalf("path %q", path)
	}
	fi, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if fi.Mode().Perm() != 0o600 {
		t.Fatalf("mode %o, want 600", fi.Mode().Perm())
	}
	c, err := Resolve(Options{Cwd: t.TempDir(), Home: home})
	if err != nil {
		t.Fatal(err)
	}
	if c.Key != "sk-ant-test-1234" {
		t.Fatalf("round trip lost the key: %q", c.Key)
	}
}

func TestLoginPrefersKeychainAndFallsBackToFile(t *testing.T) {
	home := filepath.Join(t.TempDir(), "home")

	kc := &fakeKeychain{}
	where, err := Login(home, kc, "sk-ant-one-0001")
	if err != nil {
		t.Fatal(err)
	}
	if where.Store != StoreKeychain || kc.key != "sk-ant-one-0001" {
		t.Fatalf("want the keychain, got %+v", where)
	}

	failing := &fakeKeychain{setErr: errors.New("denied")}
	where, err = Login(home, failing, "sk-ant-two-0002")
	if err != nil {
		t.Fatal(err)
	}
	if where.Store != StoreFile || where.Path != filepath.Join(home, "credentials") {
		t.Fatalf("want the file fallback, got %+v", where)
	}
}

func TestLogoutRemovesKeychainEntryAndFile(t *testing.T) {
	home := filepath.Join(t.TempDir(), "home")
	if _, err := storeInFile(home, Anthropic, "sk-ant-three-0003"); err != nil {
		t.Fatal(err)
	}
	kc := &fakeKeychain{key: "sk-ant-four-0004"}

	removed, err := Logout(home, kc)
	if err != nil {
		t.Fatal(err)
	}
	if len(removed) != 2 {
		t.Fatalf("want keychain and file removed, got %v", removed)
	}
	if kc.key != "" {
		t.Fatal("keychain entry still there")
	}
	if _, err := os.Stat(filepath.Join(home, "credentials")); !os.IsNotExist(err) {
		t.Fatal("credentials file still there")
	}

	removed, err = Logout(home, &fakeKeychain{})
	if err != nil || len(removed) != 0 {
		t.Fatalf("nothing stored: want no error and nothing removed, got %v %v", removed, err)
	}
}

func TestSecurityKeychainBuildsTheCommands(t *testing.T) {
	var calls [][]string
	run := func(name string, args ...string) ([]byte, int, error) {
		calls = append(calls, append([]string{name}, args...))
		return []byte("sk-ant-stored-9999\n"), 0, nil
	}
	kc := securityKeychain{run: run, service: Anthropic.Keychain}

	if err := kc.Set("sk-ant-stored-9999"); err != nil {
		t.Fatal(err)
	}
	got, err := kc.Get()
	if err != nil || got != "sk-ant-stored-9999" {
		t.Fatalf("get: %q %v", got, err)
	}
	if err := kc.Delete(); err != nil {
		t.Fatal(err)
	}

	want := [][]string{
		{"security", "add-generic-password", "-U", "-a", "groot", "-s", "groot.anthropic", "-w", "sk-ant-stored-9999"},
		{"security", "find-generic-password", "-a", "groot", "-s", "groot.anthropic", "-w"},
		{"security", "delete-generic-password", "-a", "groot", "-s", "groot.anthropic"},
	}
	if strings.Join(flat(calls), "|") != strings.Join(flat(want), "|") {
		t.Fatalf("commands\n got %v\nwant %v", calls, want)
	}
}

func TestSecurityKeychainExitCode44IsNotFound(t *testing.T) {
	run := func(string, ...string) ([]byte, int, error) { return nil, 44, nil }
	kc := securityKeychain{run: run, service: Anthropic.Keychain}
	if _, err := kc.Get(); !errors.Is(err, ErrNotFound) {
		t.Fatalf("get: %v", err)
	}
	if err := kc.Delete(); !errors.Is(err, ErrNotFound) {
		t.Fatalf("delete: %v", err)
	}
}

func TestSecurityKeychainErrorsNeverCarryTheKey(t *testing.T) {
	run := func(string, ...string) ([]byte, int, error) { return nil, 1, nil }
	err := securityKeychain{run: run, service: Anthropic.Keychain}.Set("sk-ant-secret-7777")
	if err == nil || strings.Contains(err.Error(), "sk-ant-secret-7777") {
		t.Fatalf("error must fail without the key: %v", err)
	}
}

func TestUnavailableKeychainOnLinux(t *testing.T) {
	kc := SystemKeychain("linux", nil)
	if _, err := kc.Get(); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("get: %v", err)
	}
	if err := kc.Set("x"); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("set: %v", err)
	}
}

func TestMaskShowsOnlyTheLastFour(t *testing.T) {
	if got := Mask("sk-ant-api03-abcdef-ab12"); got != "…ab12" {
		t.Fatalf("got %q", got)
	}
	if got := Mask("short"); strings.Contains(got, "short") {
		t.Fatalf("a short key must not be shown: %q", got)
	}
}

func TestValidKeyShape(t *testing.T) {
	for _, bad := range []string{"", "   ", "has space", "tab\tinside"} {
		if err := ValidKey(bad); err == nil {
			t.Errorf("%q must be rejected", bad)
		}
	}
	if err := ValidKey("sk-ant-api03-ok"); err != nil {
		t.Fatal(err)
	}
}

func flat(calls [][]string) []string {
	var out []string
	for _, c := range calls {
		out = append(out, strings.Join(c, " "))
	}
	return out
}
