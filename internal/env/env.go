// Package env describes the machine and the space an agent works in: the
// working directory, the operating system, the shell, the date and the git
// state. It runs processes, so only the workspace and the entry points import
// it; the core packages never do.
package env

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"time"
)

// probeTimeout bounds each external probe, so a slow git never holds a conversation open.
const probeTimeout = 3 * time.Second

// Git is the state of the space's repository when the space is one.
type Git struct {
	Branch     string // branch name, "HEAD" when detached, or "unknown"
	Status     string // "clean", "N changed paths", or "unknown"
	LastCommit string // subject of the last commit, or "unknown"
}

// Snapshot is what the environment block says about the session.
type Snapshot struct {
	Dir      string
	OS       string
	Shell    string
	Date     string // YYYY-MM-DD
	Git      *Git   // nil outside a git work tree
	AgentsMD bool   // the space's AGENTS.md was loaded
}

// Probes are the reads a snapshot is made of. Host returns the real ones; tests
// replace them.
type Probes struct {
	Run      func(ctx context.Context, dir, name string, args ...string) (string, error)
	ReadFile func(name string) ([]byte, error)
	GOOS     string
	GOARCH   string
	Getenv   func(key string) string
}

// Host returns the probes of the running machine.
func Host() Probes {
	return Probes{
		Run:      run,
		ReadFile: os.ReadFile,
		GOOS:     runtime.GOOS,
		GOARCH:   runtime.GOARCH,
		Getenv:   os.Getenv,
	}
}

func run(ctx context.Context, dir, name string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Dir = dir
	var stdout bytes.Buffer
	cmd.Stdout = &stdout
	if err := cmd.Run(); err != nil {
		return "", err
	}
	return strings.TrimSpace(stdout.String()), nil
}

// Collect reads the snapshot of dir at now. It never fails: a probe that fails
// leaves its field as "unknown", and a directory that is not a git work tree has
// no Git state.
func (p Probes) Collect(ctx context.Context, dir string, now time.Time, agentsMD bool) Snapshot {
	ctx, cancel := context.WithTimeout(ctx, probeTimeout)
	defer cancel()
	snap := Snapshot{
		Dir:      dir,
		OS:       p.osLabel(ctx),
		Shell:    "unknown",
		Date:     now.Format("2006-01-02"),
		AgentsMD: agentsMD,
	}
	if sh := p.Getenv("SHELL"); sh != "" {
		snap.Shell = sh
	}
	snap.Git = p.git(ctx, dir)
	return snap
}

func (p Probes) osLabel(ctx context.Context) string {
	arch := p.GOARCH
	switch p.GOOS {
	case "darwin":
		ver, err := p.Run(ctx, "", "sw_vers", "-productVersion")
		if err != nil || ver == "" {
			ver = "unknown"
		}
		return fmt.Sprintf("macOS %s (darwin/%s)", ver, arch)
	case "linux":
		name := "unknown"
		if raw, err := p.ReadFile("/etc/os-release"); err == nil {
			if pretty := osReleaseValue(raw, "PRETTY_NAME"); pretty != "" {
				name = pretty
			}
		}
		return fmt.Sprintf("%s (linux/%s)", name, arch)
	}
	return fmt.Sprintf("%s/%s", p.GOOS, arch)
}

// osReleaseValue returns the unquoted value of key in an os-release file, or "".
func osReleaseValue(raw []byte, key string) string {
	sc := bufio.NewScanner(bytes.NewReader(raw))
	for sc.Scan() {
		k, v, ok := strings.Cut(sc.Text(), "=")
		if ok && k == key {
			return strings.Trim(v, `"'`)
		}
	}
	return ""
}

func (p Probes) git(ctx context.Context, dir string) *Git {
	inside, err := p.Run(ctx, dir, "git", "rev-parse", "--is-inside-work-tree")
	if err != nil || inside != "true" {
		return nil
	}
	g := &Git{Branch: "unknown", Status: "unknown", LastCommit: "unknown"}
	if b, err := p.Run(ctx, dir, "git", "rev-parse", "--abbrev-ref", "HEAD"); err == nil && b != "" {
		g.Branch = b
	}
	if out, err := p.Run(ctx, dir, "git", "--no-optional-locks", "status", "--porcelain"); err == nil {
		n := 0
		for _, line := range strings.Split(out, "\n") {
			if strings.TrimSpace(line) != "" {
				n++
			}
		}
		if n == 0 {
			g.Status = "clean"
		} else {
			g.Status = fmt.Sprintf("%d changed paths", n)
		}
	}
	if subject, err := p.Run(ctx, dir, "git", "log", "-1", "--format=%s"); err == nil && subject != "" {
		g.LastCommit = subject
	}
	return g
}

// Render returns the body of the environment block: one "- field: value" line
// per field. The git lines appear only in a git work tree.
func (s Snapshot) Render() string {
	lines := []string{
		"- Working directory: " + s.Dir,
		"- OS: " + s.OS,
		"- Shell: " + s.Shell,
		"- Date: " + s.Date,
	}
	if s.Git != nil {
		lines = append(lines,
			"- Git branch: "+s.Git.Branch,
			"- Git status: "+s.Git.Status,
			"- Last commit: "+s.Git.LastCommit,
		)
	}
	agents := "not present"
	if s.AgentsMD {
		agents = "loaded"
	}
	lines = append(lines, "- AGENTS.md: "+agents)
	return strings.Join(lines, "\n")
}
