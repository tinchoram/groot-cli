// Package sandbox confines the shell commands that the bash tool runs. A Runner
// wraps one command so that it can write only inside the space directory and
// the temp directories, cannot read or write the credential directories under
// the home directory, and, unless the configuration allows it, cannot open
// network connections. The user's own terminal column is not sandboxed.
package sandbox

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

// Backend names, as shown in the UI and written to the audit log.
const (
	BackendSandboxExec = "sandbox-exec"
	BackendBwrap       = "bwrap"
	BackendNone        = "none"
	BackendOff         = "off"
)

// Runner wraps a command in the sandbox. Name reports the backend.
type Runner interface {
	Wrap(ctx context.Context, spec Spec) (*exec.Cmd, error)
	Name() string
}

// Spec describes one command. Dir is the working directory and always a
// writable root. WritablePaths lists more roots that may be written; the
// backends add the temp directories themselves. AllowNetwork lifts the network
// denial. Reads are open everywhere except SecretDirs.
type Spec struct {
	Dir           string
	Command       []string
	Env           []string
	AllowNetwork  bool
	WritablePaths []string
}

// Warning explains why no real sandbox is active. Empty means there is nothing to say.
type Warning string

// Config is the bash.sandbox mode: auto (the best backend, direct when none),
// strict (refuse to run without a backend) or off (never wrap).
type Config struct {
	Mode string
}

// sandboxExecPath is shipped with every macOS install.
const sandboxExecPath = "/usr/bin/sandbox-exec"

// SecretDirs are the credential paths under the home directory (cluster, git
// host, registry, cloud and SSH/GPG credentials). No sandboxed command may read
// or write them, with or without network. Entries may name files (.netrc) or
// directories. The backends and the README share this list.
var SecretDirs = []string{
	".ssh", ".aws", ".gnupg", ".groot", ".kube", ".netrc",
	".docker", ".config/gh", ".config/gcloud", ".azure",
}

// host is what detection depends on. Tests inject a fake one.
type host struct {
	goos     string
	home     string
	tmp      []string
	lookPath func(string) (string, error)
	exists   func(string) bool
	isDir    func(string) bool
	resolve  func(string) string
}

// defaultHost reads the real platform. home is the user's home directory.
func defaultHost(home string) host {
	return host{
		goos:     runtime.GOOS,
		home:     home,
		tmp:      []string{os.TempDir(), "/tmp"},
		lookPath: exec.LookPath,
		exists:   exists,
		isDir:    isDir,
		resolve:  realPath,
	}
}

// Detect picks the backend for this platform. When none is available, auto
// returns a runner that runs commands directly, with a warning; strict returns
// a runner that refuses every command, with the same warning.
func Detect(cfg Config) (Runner, Warning) {
	home, _ := os.UserHomeDir()
	return detect(defaultHost(home), cfg)
}

func detect(h host, cfg Config) (Runner, Warning) {
	if cfg.Mode == "off" {
		return offRunner{}, ""
	}
	if r := backendFor(h); r != nil {
		return r, ""
	}
	install := installHint(h.goos)
	if cfg.Mode == "strict" {
		msg := "bash is refused: bash.sandbox is strict and no sandbox is available. " + install
		return refusedRunner{reason: msg}, Warning(msg)
	}
	return noneRunner{}, Warning("bash runs without a sandbox, with your full permissions. " + install)
}

func backendFor(h host) Runner {
	switch h.goos {
	case "darwin":
		if h.exists(sandboxExecPath) {
			return newSeatbelt(h)
		}
	case "linux":
		if p, err := h.lookPath("bwrap"); err == nil {
			return newBwrap(p, h)
		}
	}
	return nil
}

func installHint(goos string) string {
	switch goos {
	case "darwin":
		return "Install the Xcode Command Line Tools (xcode-select --install) to get sandbox-exec."
	case "linux":
		return "Install bubblewrap (apt install bubblewrap) to enable the sandbox."
	}
	return "No sandbox backend exists for " + goos + " yet."
}

// validate rejects specs that cannot be confined safely.
func (s Spec) validate() error {
	if len(s.Command) == 0 {
		return errors.New("sandbox: the command is empty")
	}
	if s.Dir == "" {
		return errors.New("sandbox: a working directory is required")
	}
	for _, p := range append([]string{s.Dir}, s.WritablePaths...) {
		if strings.ContainsAny(p, "\n\r\x00") {
			return fmt.Errorf("sandbox: path %q contains a control character", p)
		}
	}
	return nil
}

// direct runs the command with no wrapper.
func direct(ctx context.Context, spec Spec) (*exec.Cmd, error) {
	if len(spec.Command) == 0 {
		return nil, errors.New("sandbox: the command is empty")
	}
	cmd := exec.CommandContext(ctx, spec.Command[0], spec.Command[1:]...)
	cmd.Dir = spec.Dir
	if spec.Env != nil {
		cmd.Env = spec.Env
	}
	return cmd, nil
}

// noneRunner runs commands directly: no sandbox was available in auto mode.
type noneRunner struct{}

func (noneRunner) Name() string { return BackendNone }

func (noneRunner) Wrap(ctx context.Context, spec Spec) (*exec.Cmd, error) {
	return direct(ctx, spec)
}

// offRunner runs commands directly by configuration, without a warning.
type offRunner struct{}

func (offRunner) Name() string { return BackendOff }

func (offRunner) Wrap(ctx context.Context, spec Spec) (*exec.Cmd, error) {
	return direct(ctx, spec)
}

// refusedRunner refuses every command: strict mode with no backend.
type refusedRunner struct{ reason string }

func (refusedRunner) Name() string { return BackendNone }

func (r refusedRunner) Wrap(context.Context, Spec) (*exec.Cmd, error) {
	return nil, errors.New(r.reason)
}

func exists(p string) bool {
	_, err := os.Stat(p)
	return err == nil
}

func isDir(p string) bool {
	fi, err := os.Stat(p)
	return err == nil && fi.IsDir()
}

// realPath resolves symlinks in the longest existing prefix of p, so that
// /var/x becomes /private/var/x on macOS even when x does not exist yet.
func realPath(p string) string {
	clean := filepath.Clean(p)
	cur, rest := clean, ""
	for {
		if r, err := filepath.EvalSymlinks(cur); err == nil {
			return filepath.Join(r, rest)
		}
		parent := filepath.Dir(cur)
		if parent == cur {
			return clean
		}
		rest = filepath.Join(filepath.Base(cur), rest)
		cur = parent
	}
}

// writableRoots returns the resolved, de-duplicated writable roots: the
// working directory, the extra paths of the spec, and the temp directories.
func writableRoots(spec Spec, h host) []string {
	var out []string
	seen := map[string]bool{}
	for _, p := range append(append([]string{spec.Dir}, spec.WritablePaths...), h.tmp...) {
		if p == "" {
			continue
		}
		r := h.resolve(p)
		if !seen[r] {
			seen[r] = true
			out = append(out, r)
		}
	}
	return out
}

// secretPaths returns the resolved SecretDirs under the home directory.
func secretPaths(h host) []string {
	if h.home == "" {
		return nil
	}
	out := make([]string, 0, len(SecretDirs))
	for _, d := range SecretDirs {
		out = append(out, h.resolve(filepath.Join(h.home, d)))
	}
	return out
}
