package env

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

var noon = time.Date(2026, 10, 27, 12, 30, 0, 0, time.UTC)

// fakeRun answers git and sw_vers from a table; a missing key fails.
func fakeRun(answers map[string]string) func(context.Context, string, string, ...string) (string, error) {
	return func(_ context.Context, _ string, name string, args ...string) (string, error) {
		key := name + " " + strings.Join(args, " ")
		if out, ok := answers[key]; ok {
			return out, nil
		}
		return "", errors.New("no answer for " + key)
	}
}

func gitRepo() map[string]string {
	return map[string]string{
		"git rev-parse --is-inside-work-tree":        "true",
		"git rev-parse --abbrev-ref HEAD":            "main",
		"git --no-optional-locks status --porcelain": " M a.go\n?? b.txt\n",
		"git log -1 --format=%s":                     "feat: add the thing",
		"sw_vers -productVersion":                    "15.4",
	}
}

func TestCollectRendersEveryField(t *testing.T) {
	p := Probes{Run: fakeRun(gitRepo()), GOOS: "darwin", GOARCH: "arm64", Getenv: func(string) string { return "/bin/zsh" }}
	out := p.Collect(context.Background(), "/work/space", noon, true).Render()
	for _, want := range []string{
		"- Working directory: /work/space",
		"- OS: macOS 15.4 (darwin/arm64)",
		"- Shell: /bin/zsh",
		"- Date: 2026-10-27",
		"- Git branch: main",
		"- Git status: 2 changed paths",
		`- Last commit: feat: add the thing`,
		"- AGENTS.md: loaded",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("block misses %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "15:") || strings.Contains(out, "12:30") {
		t.Errorf("the block must carry the date only, got:\n%s", out)
	}
}

func TestNonGitDirectoryRendersNoGitLines(t *testing.T) {
	answers := map[string]string{"sw_vers -productVersion": "15.4"}
	p := Probes{Run: fakeRun(answers), GOOS: "darwin", GOARCH: "arm64", Getenv: func(string) string { return "/bin/zsh" }}
	snap := p.Collect(context.Background(), "/tmp/plain", noon, false)
	if snap.Git != nil {
		t.Fatalf("a non-git directory has no git state, got %+v", snap.Git)
	}
	out := snap.Render()
	for _, banned := range []string{"Git", "Last commit"} {
		if strings.Contains(out, banned) {
			t.Errorf("non-git block mentions %q:\n%s", banned, out)
		}
	}
	if !strings.Contains(out, "- AGENTS.md: not present") {
		t.Errorf("missing AGENTS.md line:\n%s", out)
	}
}

func TestFailingProbesRenderUnknownWithoutError(t *testing.T) {
	failing := func(context.Context, string, string, ...string) (string, error) {
		return "", errors.New("boom")
	}
	p := Probes{
		Run:      failing,
		ReadFile: func(string) ([]byte, error) { return nil, errors.New("no file") },
		GOOS:     "linux", GOARCH: "amd64",
		Getenv: func(string) string { return "" },
	}
	snap := p.Collect(context.Background(), "/x", noon, false)
	out := snap.Render()
	for _, want := range []string{"- Shell: unknown", "- OS: unknown (linux/amd64)"} {
		if !strings.Contains(out, want) {
			t.Errorf("failed probes must render unknown, missing %q:\n%s", want, out)
		}
	}
	if snap.Git != nil {
		t.Errorf("a failing rev-parse means no git repository")
	}
}

func TestOSReleasePrettyNameIsUsedOnLinux(t *testing.T) {
	release := "NAME=\"Ubuntu\"\nPRETTY_NAME=\"Ubuntu 24.04.2 LTS\"\nVERSION_ID=\"24.04\"\n"
	p := Probes{
		Run:      fakeRun(nil),
		ReadFile: func(string) ([]byte, error) { return []byte(release), nil },
		GOOS:     "linux", GOARCH: "amd64",
		Getenv: func(string) string { return "/bin/bash" },
	}
	out := p.Collect(context.Background(), "/x", noon, false).Render()
	if !strings.Contains(out, "- OS: Ubuntu 24.04.2 LTS (linux/amd64)") {
		t.Errorf("linux OS must come from PRETTY_NAME:\n%s", out)
	}
}

func TestHostProbesReadTheRealMachine(t *testing.T) {
	snap := Host().Collect(context.Background(), t.TempDir(), noon, false)
	if snap.Date != "2026-10-27" {
		t.Errorf("date = %q", snap.Date)
	}
	if snap.OS == "" {
		t.Error("OS must never be empty")
	}
}
