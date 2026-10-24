//go:build linux

package sandbox

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

// realRunner returns the bwrap backend with the real filesystem and the given
// fake home directory. It skips when bwrap is missing or cannot create the
// user namespaces it needs on this host.
func realRunner(t *testing.T, home string) Runner {
	t.Helper()
	h := defaultHost(home)
	r, w := detect(h, Config{Mode: "auto"})
	if r.Name() != BackendBwrap {
		skipOrFail(t, "bwrap is not installed: install bubblewrap to run the sandbox tests")
	}
	if w != "" {
		t.Fatalf("unexpected warning %q", w)
	}
	probe := exec.Command("bwrap", "--ro-bind", "/", "/", "--dev", "/dev", "--proc", "/proc", "true")
	if out, err := probe.CombinedOutput(); err != nil {
		skipOrFail(t, fmt.Sprintf("bwrap cannot create its namespaces here (unprivileged user namespaces may be restricted): %v: %s", err, strings.TrimSpace(string(out))))
	}
	return r
}

// skipOrFail skips the test on a developer machine without a usable backend,
// and fails it where GROOT_REQUIRE_SANDBOX is set (CI), so a silent skip can
// never pass for a real run.
func skipOrFail(t *testing.T, reason string) {
	t.Helper()
	if os.Getenv("GROOT_REQUIRE_SANDBOX") != "" {
		t.Fatalf("GROOT_REQUIRE_SANDBOX is set and the sandbox is unavailable: %s", reason)
	}
	t.Skip(reason)
}

// runSandboxed runs a shell script under r and returns its combined output and error.
func runSandboxed(t *testing.T, r Runner, dir string, network bool, script string) (string, error) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	cmd, err := r.Wrap(ctx, Spec{
		Dir:           dir,
		Command:       []string{"/bin/sh", "-c", script},
		AllowNetwork:  network,
		WritablePaths: []string{dir},
	})
	if err != nil {
		t.Fatalf("Wrap: %v", err)
	}
	out, err := cmd.CombinedOutput()
	return string(out), err
}

func TestBwrapSandboxWritesOnlyInsideTheSpace(t *testing.T) {
	r := realRunner(t, t.TempDir())
	space := t.TempDir()

	if out, err := runSandboxed(t, r, space, false, "echo ok > inside.txt"); err != nil {
		t.Fatalf("write inside the space must succeed: %v\n%s", err, out)
	}
	if _, err := os.Stat(filepath.Join(space, "inside.txt")); err != nil {
		t.Fatalf("file inside the space missing: %v", err)
	}

	realHome, err := os.UserHomeDir()
	if err != nil {
		t.Skip("no home directory")
	}
	target := filepath.Join(realHome, ".groot-sandbox-probe-"+strconv.FormatInt(time.Now().UnixNano(), 36))
	t.Cleanup(func() { _ = os.Remove(target) })
	if out, err := runSandboxed(t, r, space, false, "echo x > "+target); err == nil {
		t.Fatalf("write outside the space must fail\n%s", out)
	}
	if _, statErr := os.Stat(target); statErr == nil {
		t.Fatalf("the sandbox let the file be created at %s", target)
	}
}

func TestBwrapSandboxDeniesSecretDirReads(t *testing.T) {
	home := t.TempDir()
	ssh := filepath.Join(home, ".ssh")
	if err := os.MkdirAll(ssh, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(ssh, "id_test"), []byte("SECRET"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(home, "note.txt"), []byte("plain"), 0o600); err != nil {
		t.Fatal(err)
	}
	kube := filepath.Join(home, ".kube")
	if err := os.MkdirAll(kube, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(kube, "config"), []byte("KUBE-SECRET"), 0o600); err != nil {
		t.Fatal(err)
	}
	r := realRunner(t, home)
	space := t.TempDir()

	out, err := runSandboxed(t, r, space, false, "cat "+filepath.Join(ssh, "id_test"))
	if err == nil || strings.Contains(out, "SECRET") {
		t.Fatalf("reading ~/.ssh must fail, got err=%v out=%q", err, out)
	}
	out, err = runSandboxed(t, r, space, true, "cat "+filepath.Join(kube, "config"))
	if err == nil || strings.Contains(out, "KUBE-SECRET") {
		t.Fatalf("reading ~/.kube must fail even with network allowed, got err=%v out=%q", err, out)
	}
	out, err = runSandboxed(t, r, space, false, "cat "+filepath.Join(home, "note.txt"))
	if err != nil || !strings.Contains(out, "plain") {
		t.Fatalf("reading a non-secret file must work: err=%v out=%q", err, out)
	}
}

func TestBwrapSandboxNetwork(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	srv := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("pong"))
	})}
	go func() { _ = srv.Serve(ln) }()
	t.Cleanup(func() { _ = srv.Close() })

	curl, err := exec.LookPath("curl")
	if err != nil {
		t.Skip("curl not found")
	}
	script := curl + " -sS --max-time 3 http://" + ln.Addr().String() + "/"

	r := realRunner(t, t.TempDir())
	space := t.TempDir()

	out, err := runSandboxed(t, r, space, true, script)
	if err != nil || !strings.Contains(out, "pong") {
		t.Fatalf("network allowed must reach the local server: err=%v out=%q", err, out)
	}
	out, err = runSandboxed(t, r, space, false, script)
	if err == nil || strings.Contains(out, "pong") {
		t.Fatalf("network denied must not reach the local server: err=%v out=%q", err, out)
	}
}
