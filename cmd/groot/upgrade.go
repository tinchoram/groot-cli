package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"time"

	"github.com/tinchoram/groot-cli/internal/update"
)

// upgradeEnv is what `groot upgrade` reads from the machine. Tests fill it.
type upgradeEnv struct {
	Version string
	Getenv  func(string) string
	Client  *http.Client
	Exe     func() (string, error) // the running binary, symlinks resolved
	Goos    string
	Goarch  string
}

// upgradeTimeout bounds the whole download and replace.
const upgradeTimeout = 5 * time.Minute

// runUpgrade handles `groot upgrade [--check]`. It needs no API key.
func runUpgrade(args []string, stdout, stderr io.Writer, env upgradeEnv) int {
	fs := flag.NewFlagSet("groot upgrade", flag.ContinueOnError)
	fs.SetOutput(stderr)
	checkOnly := fs.Bool("check", false, "only check whether a newer release exists")
	if err := fs.Parse(args); err != nil {
		return 2
	}

	ctx, cancel := context.WithTimeout(context.Background(), upgradeTimeout)
	defer cancel()
	token := update.Token(env.Getenv)
	rel, err := update.Latest(ctx, env.Client, update.Repo, token)
	if err != nil {
		fmt.Fprintf(stderr, "groot: %v\n", err)
		return 1
	}
	fmt.Fprintf(stdout, "current: v%s\n", env.Version)
	fmt.Fprintf(stdout, "latest: v%s\n", rel.Version)

	if !update.Newer(env.Version, rel.Version) {
		fmt.Fprintf(stdout, "groot v%s is the latest\n", env.Version)
		return 0
	}
	if *checkOnly {
		fmt.Fprintln(stdout, "update available: run groot upgrade")
		return 0
	}

	exe, err := env.Exe()
	if err != nil {
		fmt.Fprintf(stderr, "groot: locate the running binary: %v\n", err)
		return 1
	}
	if _, err := update.Apply(ctx, env.Client, rel, exe, env.Goos, env.Goarch, token); err != nil {
		fmt.Fprintf(stderr, "groot: %v\n", err)
		return 1
	}
	fmt.Fprintf(stdout, "upgraded to v%s; restart groot\n", rel.Version)
	return 0
}

// upgradeEnvironment is the real machine: the running binary and this build's platform.
func upgradeEnvironment(getenv func(string) string) upgradeEnv {
	return upgradeEnv{
		Version: version,
		Getenv:  getenv,
		Client:  update.NewClient(version),
		Exe:     executablePath,
		Goos:    runtime.GOOS,
		Goarch:  runtime.GOARCH,
	}
}

// executablePath returns the running binary with symlinks resolved, so that a
// symlink on PATH is upgraded in place of its target.
func executablePath() (string, error) {
	exe, err := os.Executable()
	if err != nil {
		return "", err
	}
	return filepath.EvalSymlinks(exe)
}
