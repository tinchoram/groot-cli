package main

import (
	"bytes"
	"context"
	"errors"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/tinchoram/groot-cli/internal/sandbox"
	"github.com/tinchoram/groot-cli/internal/space"
)

// fakeRunner is a sandbox.Runner that only reports a name.
type fakeRunner struct{ name string }

func (f fakeRunner) Name() string { return f.name }
func (f fakeRunner) Wrap(context.Context, sandbox.Spec) (*exec.Cmd, error) {
	return nil, errors.New("not used")
}

// fakeDoctorEnv is a doctor environment with nothing installed: no kubectl, no
// sandbox backend, an empty keychain and a Fedora-like os-release.
func fakeDoctorEnv(t *testing.T, home string) doctorEnv {
	t.Helper()
	cwd := t.TempDir()
	return doctorEnv{
		Version:    "9.9.9-test",
		Cwd:        cwd,
		Home:       home,
		ConfigPath: filepath.Join(cwd, "groot.yaml"),
		GlobalPath: filepath.Join(home, "groot.yaml"),
		Goos:       "linux",
		OSRelease:  "ID=fedora\n",
		Keychain:   &fakeKeychain{},
		LookPath:   func(string) (string, error) { return "", exec.ErrNotFound },
		Detect: func(sandbox.Config) (sandbox.Runner, sandbox.Warning) {
			return fakeRunner{name: sandbox.BackendNone}, "bash runs without a sandbox"
		},
	}
}

func runDoctorWith(t *testing.T, env doctorEnv) (string, int) {
	t.Helper()
	var out bytes.Buffer
	code := doctor(&out, env)
	return out.String(), code
}

func lineWith(out, prefix string) string {
	for _, l := range strings.Split(out, "\n") {
		if strings.HasPrefix(l, prefix) {
			return l
		}
	}
	return ""
}

func TestDoctorPrintsEverySectionOnItsOwnLine(t *testing.T) {
	home := t.TempDir()
	env := fakeDoctorEnv(t, home)
	env.EnvKey = "sk-ant-doctor-test-1234"

	out, code := runDoctorWith(t, env)
	if code != 0 {
		t.Fatalf("exit %d with a key present:\n%s", code, out)
	}
	for _, prefix := range []string{"version:", "home:", "config global:", "config project:", "api key:", "sandbox:", "kubectl:", "spaces:", "inbox:"} {
		if lineWith(out, prefix) == "" {
			t.Errorf("no line starting with %q in:\n%s", prefix, out)
		}
	}
	if l := lineWith(out, "version:"); !strings.Contains(l, "9.9.9-test") {
		t.Errorf("version line %q", l)
	}
	if l := lineWith(out, "home:"); !strings.Contains(l, home) {
		t.Errorf("home line %q should name %s", l, home)
	}
	if l := lineWith(out, "api key:"); !strings.Contains(l, "environment") || !strings.Contains(l, "…1234") {
		t.Errorf("api key line %q", l)
	}
	if strings.Contains(out, "sk-ant-doctor-test") {
		t.Fatal("doctor must never print the full key")
	}
}

func TestDoctorBlocksWithoutAKeyAndNamesLogin(t *testing.T) {
	env := fakeDoctorEnv(t, t.TempDir())

	out, code := runDoctorWith(t, env)
	if code != 1 {
		t.Fatalf("a missing key blocks startup: exit %d", code)
	}
	if l := lineWith(out, "api key:"); !strings.Contains(l, "missing: run groot auth login") {
		t.Fatalf("api key line %q", l)
	}
}

func TestDoctorReadsTheKeyFromTheKeychain(t *testing.T) {
	env := fakeDoctorEnv(t, t.TempDir())
	env.Keychain = &fakeKeychain{key: "sk-ant-keychain-test-9876"}

	out, code := runDoctorWith(t, env)
	if code != 0 {
		t.Fatalf("exit %d", code)
	}
	if l := lineWith(out, "api key:"); !strings.Contains(l, "keychain") {
		t.Fatalf("api key line %q", l)
	}
}

func TestDoctorSandboxNamesTheDistroInstallCommand(t *testing.T) {
	env := fakeDoctorEnv(t, t.TempDir())
	env.EnvKey = "sk-ant-doctor-test-1234"
	env.OSRelease = "ID=ubuntu\nID_LIKE=debian\n"

	out, _ := runDoctorWith(t, env)
	l := lineWith(out, "sandbox:")
	if !strings.Contains(l, "none") || !strings.Contains(l, "sudo apt install bubblewrap") {
		t.Fatalf("sandbox line should name none and the apt command: %q", l)
	}
}

func TestDoctorSandboxBackendFromRunner(t *testing.T) {
	env := fakeDoctorEnv(t, t.TempDir())
	env.EnvKey = "sk-ant-doctor-test-1234"
	env.Detect = func(sandbox.Config) (sandbox.Runner, sandbox.Warning) {
		return fakeRunner{name: sandbox.BackendBwrap}, ""
	}

	out, _ := runDoctorWith(t, env)
	if l := lineWith(out, "sandbox:"); !strings.Contains(l, "bwrap") || strings.Contains(l, "install") {
		t.Fatalf("an active backend needs no install hint: %q", l)
	}
}

func TestSandboxBackendNames(t *testing.T) {
	cases := []struct {
		runner, mode, want string
	}{
		{sandbox.BackendSandboxExec, "auto", "seatbelt"},
		{sandbox.BackendBwrap, "auto", "bwrap"},
		{sandbox.BackendNone, "auto", "none"},
		{sandbox.BackendNone, "strict", "refused"},
		{sandbox.BackendOff, "off", "off"},
	}
	for _, tc := range cases {
		if got := sandboxBackend(fakeRunner{name: tc.runner}, tc.mode); got != tc.want {
			t.Errorf("runner %s mode %s: got %q want %q", tc.runner, tc.mode, got, tc.want)
		}
	}
}

func TestDoctorKubectlFoundOrOptionalMissing(t *testing.T) {
	env := fakeDoctorEnv(t, t.TempDir())
	env.EnvKey = "sk-ant-doctor-test-1234"

	out, code := runDoctorWith(t, env)
	if code != 0 {
		t.Fatalf("kubectl is optional: exit %d", code)
	}
	if l := lineWith(out, "kubectl:"); !strings.Contains(l, "not found") {
		t.Fatalf("kubectl line %q", l)
	}

	env.LookPath = func(string) (string, error) { return "/usr/local/bin/kubectl", nil }
	out, _ = runDoctorWith(t, env)
	if l := lineWith(out, "kubectl:"); !strings.Contains(l, "/usr/local/bin/kubectl") {
		t.Fatalf("kubectl line %q", l)
	}
}

func TestDoctorCountsSpacesAndNamesTheInbox(t *testing.T) {
	home := t.TempDir()
	store, err := space.Open(filepath.Join(home, "spaces.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Add("root", home, time.Now()); err != nil {
		t.Fatal(err)
	}
	env := fakeDoctorEnv(t, home)
	env.EnvKey = "sk-ant-doctor-test-1234"

	out, _ := runDoctorWith(t, env)
	if l := lineWith(out, "spaces:"); !strings.HasPrefix(l, "spaces: 1") {
		t.Fatalf("spaces line %q", l)
	}
	if l := lineWith(out, "inbox:"); !strings.Contains(l, filepath.Join(home, "inbox.db")) {
		t.Fatalf("inbox line %q", l)
	}
}

func TestDoctorShowsWhichConfigFilesLoaded(t *testing.T) {
	home := t.TempDir()
	env := fakeDoctorEnv(t, home)
	env.EnvKey = "sk-ant-doctor-test-1234"
	writeTestFile(t, env.GlobalPath, "max_turns: 5\n")

	out, _ := runDoctorWith(t, env)
	if l := lineWith(out, "config global:"); !strings.Contains(l, "found") {
		t.Fatalf("global line %q", l)
	}
	if l := lineWith(out, "config project:"); !strings.Contains(l, "missing") {
		t.Fatalf("project line %q", l)
	}
}

func TestDoctorBlocksOnABrokenConfigFile(t *testing.T) {
	env := fakeDoctorEnv(t, t.TempDir())
	env.EnvKey = "sk-ant-doctor-test-1234"
	writeTestFile(t, env.ConfigPath, "gate: [unclosed\n")

	out, code := runDoctorWith(t, env)
	if code != 1 {
		t.Fatalf("a config that does not load blocks startup: exit %d", code)
	}
	if lineWith(out, "config:") == "" {
		t.Fatalf("no config error line in:\n%s", out)
	}
}

func TestRunDoctorExitsZeroWithAKeyInTheEnvironment(t *testing.T) {
	getenv, _ := isolate(t, "sk-ant-run-test-4321")
	var out, errOut bytes.Buffer
	if code := run([]string{"doctor"}, nil, &out, &errOut, getenv); code != 0 {
		t.Fatalf("exit %d: stderr %q", code, errOut.String())
	}
	if !strings.Contains(out.String(), "sandbox:") {
		t.Fatalf("doctor output:\n%s", out.String())
	}
}
