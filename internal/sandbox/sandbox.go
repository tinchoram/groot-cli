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
	"slices"
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
// denial. Reads are open everywhere except SecretDirs. ReadableSecrets lists
// home-relative entries of SecretDirs that the command may read but not write;
// any other secret stays denied.
type Spec struct {
	Dir             string
	Command         []string
	Env             []string
	AllowNetwork    bool
	WritablePaths   []string
	ReadableSecrets []string
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

// CredentialSets name the groups of SecretDirs an agent may be granted. A
// granted set is readable, never writable, inside that agent's commands. The
// set names are what agent files and the admin tools use. .groot is in no set.
var CredentialSets = map[string][]string{
	"aws":    {".aws"},
	"kube":   {".kube"},
	"ssh":    {".ssh"},
	"gcloud": {".config/gcloud"},
	"azure":  {".azure"},
	"docker": {".docker"},
	"gh":     {".config/gh"},
	"gnupg":  {".gnupg"},
	"netrc":  {".netrc"},
}

// CredentialNames returns the names of the credential sets, sorted.
func CredentialNames() []string {
	names := make([]string, 0, len(CredentialSets))
	for name := range CredentialSets {
		names = append(names, name)
	}
	slices.Sort(names)
	return names
}

// CredentialDirs returns the home-relative secret entries of the named sets,
// sorted and without duplicates. A name that is no set is an error that lists
// the known ones.
func CredentialDirs(sets []string) ([]string, error) {
	var dirs []string
	for _, name := range sets {
		entries, ok := CredentialSets[name]
		if !ok {
			return nil, fmt.Errorf("unknown credential set %q (known: %s)", name, strings.Join(CredentialNames(), ", "))
		}
		dirs = append(dirs, entries...)
	}
	slices.Sort(dirs)
	return slices.Compact(dirs), nil
}

// DefaultCacheDirs returns the tool cache roots of a platform that commands may
// write: $XDG_CACHE_HOME or ~/.cache, ~/.npm, ~/Library/Caches on darwin,
// $GOCACHE, and the Go module cache: $GOMODCACHE, else the pkg/mod directory of
// the first $GOPATH entry, else ~/go/pkg/mod. Relative values from the
// environment are ignored. getenv is os.Getenv in production.
func DefaultCacheDirs(goos, home string, getenv func(string) string) []string {
	var out []string
	if xdg := getenv("XDG_CACHE_HOME"); filepath.IsAbs(xdg) {
		out = append(out, filepath.Clean(xdg))
	} else if home != "" {
		out = append(out, filepath.Join(home, ".cache"))
	}
	if home != "" {
		out = append(out, filepath.Join(home, ".npm"))
	}
	if goos == "darwin" && home != "" {
		out = append(out, filepath.Join(home, "Library", "Caches"))
	}
	if gocache := getenv("GOCACHE"); filepath.IsAbs(gocache) {
		out = append(out, filepath.Clean(gocache))
	}
	if mod := moduleCacheDir(home, getenv); mod != "" {
		out = append(out, mod)
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// moduleCacheDir is the Go module cache: $GOMODCACHE when absolute, else the
// pkg/mod of the first absolute $GOPATH entry, else ~/go/pkg/mod. It is "" when
// none applies (no home directory and no usable environment value).
func moduleCacheDir(home string, getenv func(string) string) string {
	if mod := getenv("GOMODCACHE"); filepath.IsAbs(mod) {
		return filepath.Clean(mod)
	}
	if first, _, _ := strings.Cut(getenv("GOPATH"), string(filepath.ListSeparator)); filepath.IsAbs(first) {
		return filepath.Join(filepath.Clean(first), "pkg", "mod")
	}
	if home != "" {
		return filepath.Join(home, "go", "pkg", "mod")
	}
	return ""
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
	// osRelease is the content of /etc/os-release, empty when unknown.
	osRelease string
}

// defaultHost reads the real platform. home is the user's home directory.
func defaultHost(home string) host {
	return host{
		goos:      runtime.GOOS,
		home:      home,
		tmp:       []string{os.TempDir(), "/tmp"},
		lookPath:  exec.LookPath,
		exists:    exists,
		isDir:     isDir,
		resolve:   realPath,
		osRelease: ReadOSRelease(runtime.GOOS),
	}
}

// ReadOSRelease returns the content of /etc/os-release on linux, and "" on other
// platforms or when the file is missing.
func ReadOSRelease(goos string) string {
	if goos != "linux" {
		return ""
	}
	raw, err := os.ReadFile("/etc/os-release")
	if err != nil {
		return ""
	}
	return string(raw)
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
	install := InstallHint(h.goos, h.osRelease)
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

// InstallHint names how to get the sandbox backend on goos. On linux the command
// follows the distribution in osRelease (the content of /etc/os-release); when
// the distribution is unknown it falls back to the apt command.
func InstallHint(goos, osRelease string) string {
	switch goos {
	case "darwin":
		return "Install the Xcode Command Line Tools (xcode-select --install) to get sandbox-exec."
	case "linux":
		if cmd := bubblewrapCommand(osRelease); cmd != "" {
			return "Install bubblewrap (" + cmd + ") to enable the sandbox."
		}
		return "Install bubblewrap (apt install bubblewrap) to enable the sandbox."
	}
	return "No sandbox backend exists for " + goos + " yet."
}

// bubblewrapCommand returns the package-manager command for the distribution
// named in osRelease, matching ID first and then ID_LIKE. It returns "" when no
// known family matches.
func bubblewrapCommand(osRelease string) string {
	families := []struct {
		names []string
		cmd   string
	}{
		{[]string{"debian", "ubuntu"}, "sudo apt install bubblewrap"},
		{[]string{"fedora", "rhel", "centos"}, "sudo dnf install bubblewrap"},
		{[]string{"arch"}, "sudo pacman -S bubblewrap"},
		{[]string{"suse", "opensuse", "sles", "opensuse-tumbleweed", "opensuse-leap"}, "sudo zypper install bubblewrap"},
	}
	for _, id := range osReleaseIDs(osRelease) {
		for _, f := range families {
			if slices.Contains(f.names, id) {
				return f.cmd
			}
		}
	}
	return ""
}

// osReleaseIDs returns the ID value followed by the ID_LIKE values, lowercased.
func osReleaseIDs(osRelease string) []string {
	var ids []string
	for _, line := range strings.Split(osRelease, "\n") {
		key, value, ok := strings.Cut(strings.TrimSpace(line), "=")
		if !ok {
			continue
		}
		value = strings.ToLower(strings.Trim(value, `"'`))
		switch key {
		case "ID":
			ids = append([]string{value}, ids...)
		case "ID_LIKE":
			ids = append(ids, strings.Fields(value)...)
		}
	}
	return ids
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

// secretEntry is one SecretDirs entry: its home-relative name and its resolved path.
type secretEntry struct {
	rel  string
	path string
}

// secretEntries returns the resolved SecretDirs under the home directory.
func secretEntries(h host) []secretEntry {
	if h.home == "" {
		return nil
	}
	out := make([]secretEntry, 0, len(SecretDirs))
	for _, d := range SecretDirs {
		out = append(out, secretEntry{rel: d, path: h.resolve(filepath.Join(h.home, d))})
	}
	return out
}
