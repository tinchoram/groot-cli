package update

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// fakeGroot is the groot binary of the fake release. It prints its arguments,
// so the test can see that the installer ran `groot doctor`.
var fakeGroot = []byte("#!/bin/sh\necho \"fake groot $*\"\n")

// installVersion is the version of the fake release, without the v.
const installVersion = "0.9.1"

// installScript is install.sh at the repository root.
func installScript(t *testing.T) string {
	t.Helper()
	p, err := filepath.Abs(filepath.Join("..", "..", "install.sh"))
	if err != nil {
		t.Fatal(err)
	}
	return p
}

// supportedArchName is the archive for this host. Hosts the installer does not
// support are skipped.
func supportedArchName(t *testing.T) string {
	t.Helper()
	switch runtime.GOOS {
	case "darwin", "linux":
	default:
		t.Skipf("install.sh does not run on %s", runtime.GOOS)
	}
	switch runtime.GOARCH {
	case "amd64", "arm64":
	default:
		t.Skipf("install.sh does not run on %s", runtime.GOARCH)
	}
	return AssetName(installVersion, runtime.GOOS, runtime.GOARCH)
}

// installShells is sh, and dash when it is installed.
func installShells(t *testing.T) []string {
	t.Helper()
	if _, err := exec.LookPath("sh"); err != nil {
		t.Skip("sh is not on PATH")
	}
	shells := []string{"sh"}
	if _, err := exec.LookPath("dash"); err == nil {
		shells = append(shells, "dash")
	}
	return shells
}

// newInstallRelease serves a fake GitHub release with the archive and the
// checksums file. A private release answers the API and the asset api urls only
// with testToken, and its public download urls always answer 404.
func newInstallRelease(t *testing.T, private bool, archName string, archive, sums []byte) *httptest.Server {
	t.Helper()
	files := map[string][]byte{archName: archive, ChecksumsName: sums}
	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hidden := private && r.Header.Get("Authorization") != "Bearer "+testToken
		send := func(name string) {
			body, ok := files[name]
			if !ok {
				http.NotFound(w, r)
				return
			}
			_, _ = w.Write(body)
		}
		switch {
		case !hidden && (r.URL.Path == "/repos/"+Repo+"/releases/latest" || r.URL.Path == "/repos/"+Repo+"/releases/tags/v"+installVersion):
			type asset struct {
				URL                string `json:"url"`
				Name               string `json:"name"`
				BrowserDownloadURL string `json:"browser_download_url"`
				Size               int    `json:"size"`
			}
			rel := struct {
				TagName string  `json:"tag_name"`
				Assets  []asset `json:"assets"`
			}{TagName: "v" + installVersion}
			for name, body := range files {
				rel.Assets = append(rel.Assets, asset{
					URL:                srv.URL + "/repos/" + Repo + "/releases/assets/" + name,
					Name:               name,
					BrowserDownloadURL: srv.URL + "/dl/" + name,
					Size:               len(body),
				})
			}
			enc := json.NewEncoder(w)
			enc.SetIndent("", "  ")
			_ = enc.Encode(rel)
		case !hidden && strings.HasPrefix(r.URL.Path, "/repos/"+Repo+"/releases/assets/") && r.Header.Get("Accept") == "application/octet-stream":
			send(strings.TrimPrefix(r.URL.Path, "/repos/"+Repo+"/releases/assets/"))
		case !private && strings.HasPrefix(r.URL.Path, "/dl/"):
			send(strings.TrimPrefix(r.URL.Path, "/dl/"))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

// installFixture makes the temporary HOME, the scratch TMPDIR and the install
// directory of one run. The install directory is not created.
func installFixture(t *testing.T) (home, scratch, bin string) {
	t.Helper()
	root := t.TempDir()
	home = filepath.Join(root, "home")
	scratch = filepath.Join(root, "scratch")
	bin = filepath.Join(root, "bin")
	for _, d := range []string{home, scratch} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	return home, scratch, bin
}

// installEnv is the whole environment of one run, so the developer's own
// GROOT_ or token variables never reach the installer.
func installEnv(home, scratch, bin, base string, extra ...string) []string {
	env := []string{
		"PATH=" + os.Getenv("PATH"),
		"HOME=" + home,
		"TMPDIR=" + scratch,
		"GROOT_INSTALL_DIR=" + bin,
		"GROOT_BASE_URL=" + base,
		"GROOT_DOWNLOAD_BASE=" + base,
	}
	return append(env, extra...)
}

// runInstall runs install.sh under shell and returns its combined output.
func runInstall(t *testing.T, shell string, env []string, args ...string) (string, error) {
	t.Helper()
	cmd := exec.Command(shell, append([]string{installScript(t)}, args...)...)
	cmd.Env = env
	out, err := cmd.CombinedOutput()
	return string(out), err
}

// assertEmptyDir fails when the directory still holds files, such as a
// download or a temporary directory the installer should have removed.
func assertEmptyDir(t *testing.T, dir string) {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatalf("%s is not empty after the run: %d entries", dir, len(entries))
	}
}

func TestInstallScriptInstallsTheReleaseBinary(t *testing.T) {
	archName := supportedArchName(t)
	archive := targz(t, "groot", fakeGroot)
	srv := newInstallRelease(t, false, archName, archive, []byte(sumLine(archive, archName)))
	for _, shell := range installShells(t) {
		t.Run(shell, func(t *testing.T) {
			home, scratch, bin := installFixture(t)
			out, err := runInstall(t, shell, installEnv(home, scratch, bin, srv.URL))
			if err != nil {
				t.Fatalf("install.sh: %v\n%s", err, out)
			}
			exe := filepath.Join(bin, "groot")
			fi, err := os.Stat(exe)
			if err != nil {
				t.Fatalf("groot was not installed: %v\n%s", err, out)
			}
			if fi.Mode().Perm() != 0o755 {
				t.Fatalf("mode = %v, want 0755", fi.Mode().Perm())
			}
			body, err := os.ReadFile(exe)
			if err != nil {
				t.Fatal(err)
			}
			if string(body) != string(fakeGroot) {
				t.Fatalf("installed binary = %q", body)
			}
			if !strings.Contains(out, "fake groot doctor") {
				t.Errorf("groot doctor did not run:\n%s", out)
			}
			assertEmptyDir(t, scratch)
		})
	}
}

func TestInstallScriptPinsAVersionWithGrootVersion(t *testing.T) {
	archName := supportedArchName(t)
	archive := targz(t, "groot", fakeGroot)
	srv := newInstallRelease(t, false, archName, archive, []byte(sumLine(archive, archName)))
	shell := installShells(t)[0]
	home, scratch, bin := installFixture(t)
	out, err := runInstall(t, shell, installEnv(home, scratch, bin, srv.URL, "GROOT_VERSION=v"+installVersion))
	if err != nil {
		t.Fatalf("install.sh: %v\n%s", err, out)
	}
	if _, err := os.Stat(filepath.Join(bin, "groot")); err != nil {
		t.Fatalf("groot was not installed: %v", err)
	}
}

func TestInstallScriptRefusesATamperedArchive(t *testing.T) {
	archName := supportedArchName(t)
	archive := targz(t, "groot", fakeGroot)
	wrong := fmt.Sprintf("%064d  %s\n", 0, archName)
	srv := newInstallRelease(t, false, archName, archive, []byte(wrong))
	for _, shell := range installShells(t) {
		t.Run(shell, func(t *testing.T) {
			home, scratch, bin := installFixture(t)
			out, err := runInstall(t, shell, installEnv(home, scratch, bin, srv.URL))
			if err == nil {
				t.Fatalf("install.sh succeeded with a wrong checksum:\n%s", out)
			}
			if !strings.Contains(out, "checksum mismatch") {
				t.Errorf("output does not name the checksum mismatch:\n%s", out)
			}
			if _, err := os.Stat(bin); !os.IsNotExist(err) {
				t.Errorf("the install directory was created or used after a mismatch: %v", err)
			}
			assertEmptyDir(t, scratch)
		})
	}
}

func TestInstallScriptUsesTheTokenForAPrivateRepository(t *testing.T) {
	archName := supportedArchName(t)
	archive := targz(t, "groot", fakeGroot)
	srv := newInstallRelease(t, true, archName, archive, []byte(sumLine(archive, archName)))
	shell := installShells(t)[0]
	home, scratch, bin := installFixture(t)
	out, err := runInstall(t, shell, installEnv(home, scratch, bin, srv.URL, "GITHUB_TOKEN="+testToken))
	if err != nil {
		t.Fatalf("install.sh with a token: %v\n%s", err, out)
	}
	if _, err := os.Stat(filepath.Join(bin, "groot")); err != nil {
		t.Fatalf("groot was not installed: %v", err)
	}
}

func TestInstallScriptNamesTheTokenForAPrivateRepository(t *testing.T) {
	archName := supportedArchName(t)
	archive := targz(t, "groot", fakeGroot)
	srv := newInstallRelease(t, true, archName, archive, []byte(sumLine(archive, archName)))
	shell := installShells(t)[0]
	home, scratch, bin := installFixture(t)
	out, err := runInstall(t, shell, installEnv(home, scratch, bin, srv.URL))
	if err == nil {
		t.Fatalf("install.sh succeeded without a token for a private repository:\n%s", out)
	}
	if !strings.Contains(out, "GITHUB_TOKEN") || !strings.Contains(out, "GH_TOKEN") {
		t.Fatalf("the hint should name both variables:\n%s", out)
	}
	if _, err := os.Stat(bin); !os.IsNotExist(err) {
		t.Errorf("the install directory was created without a release: %v", err)
	}
}

func TestInstallScriptHelpNamesTheKnobs(t *testing.T) {
	shell := installShells(t)[0]
	home, scratch, bin := installFixture(t)
	out, err := runInstall(t, shell, installEnv(home, scratch, bin, "http://127.0.0.1:1"), "--help")
	if err != nil {
		t.Fatalf("--help: %v\n%s", err, out)
	}
	for _, want := range []string{"GROOT_VERSION", "GROOT_INSTALL_DIR", "GITHUB_TOKEN"} {
		if !strings.Contains(out, want) {
			t.Errorf("--help does not mention %s:\n%s", want, out)
		}
	}
}
