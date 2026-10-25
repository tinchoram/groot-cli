package flow

import (
	"strings"
	"testing"
	"time"
)

func TestTestRunnerPatternMatchesTheTestCommands(t *testing.T) {
	cases := []string{
		"go test ./...",
		"GO TEST ./internal/x",
		"cd api && pytest -q tests/",
		"uv run pytest tests",
		"npm test",
		"pnpm test -- --watch=false",
		"yarn test",
		"vitest run",
		"jest --ci",
		"cargo test --all",
		"mvn test",
		"gradle test",
		"dotnet test",
		"make test",
		"bun test",
		"bundle exec rspec spec/",
		"vendor/bin/phpunit",
	}
	for _, cmd := range cases {
		t.Run(cmd, func(t *testing.T) {
			if !testRunner.MatchString(cmd) {
				t.Fatalf("%q should be a test run", cmd)
			}
		})
	}
}

func TestTestRunnerPatternRefusesTheOthers(t *testing.T) {
	cases := []string{
		"go build ./...",
		"go vet ./...",
		"go testdata",
		"echo mygo test",
		"cargo build",
		"make build",
		"npm install",
		"python -m unittest",
		"pytests",
		"jestify",
	}
	for _, cmd := range cases {
		t.Run(cmd, func(t *testing.T) {
			if testRunner.MatchString(cmd) {
				t.Fatalf("%q should not be a test run", cmd)
			}
		})
	}
}

func commandEvidence(at time.Time, phase Phase, command, exit string) Evidence {
	return Evidence{Phase: phase, Kind: KindCommand, At: at, Text: "$ " + command + "\nout\nexit: " + exit}
}

func TestCycleStartIsTheNewestImplementNote(t *testing.T) {
	ev := []Evidence{
		commandEvidence(t0, PhaseImplement, "go test ./a", "0"),
		{Phase: PhaseImplement, Kind: KindNote, At: t0.Add(time.Minute), Text: "fix the edge case"},
		{Phase: PhaseVerify, Kind: KindNote, At: t0.Add(2 * time.Minute), Text: "not an implement note"},
	}
	if got := CycleStart(ev); !got.Equal(t0.Add(time.Minute)) {
		t.Fatalf("cycle start = %v, want the implement note", got)
	}
	if got := CycleStart(ev[:1]); !got.IsZero() {
		t.Fatalf("without a note the cycle starts at the beginning, got %v", got)
	}
}

func TestTestRunsExcludesTheRunsBeforeTheLastImplementEntry(t *testing.T) {
	ev := []Evidence{
		commandEvidence(t0.Add(1*time.Second), PhaseImplement, "go test ./old", "non-zero"),
		{Phase: PhaseImplement, Kind: KindNote, At: t0.Add(2 * time.Second), Text: "returned by verify"},
		commandEvidence(t0.Add(3*time.Second), PhaseImplement, "go test ./internal/memory/...", "non-zero"),
		commandEvidence(t0.Add(4*time.Second), PhaseImplement, "go build ./...", "0"),
		commandEvidence(t0.Add(5*time.Second), PhaseImplement, "go test ./internal/memory/...", "0"),
		commandEvidence(t0.Add(6*time.Second), PhaseVerify, "go test ./...", "0"),
	}
	runs := TestRuns(ev, CycleStart(ev))
	if len(runs) != 3 {
		t.Fatalf("runs = %+v, want the three test runs after the implement note", runs)
	}
	want := []struct{ command, exit string }{
		{"go test ./internal/memory/...", "non-zero"},
		{"go test ./internal/memory/...", "0"},
		{"go test ./...", "0"},
	}
	for i, w := range want {
		if runs[i].Command != w.command || runs[i].Exit != w.exit {
			t.Fatalf("run %d = %+v, want %+v", i+1, runs[i], w)
		}
	}
	if runs[2].Phase != PhaseVerify {
		t.Fatalf("the verify run keeps its phase: %+v", runs[2])
	}
}

func TestTestRunsKeepsTheOrderAndTheFullCommandCutToOneHundredRunes(t *testing.T) {
	long := "go test " + strings.Repeat("x", 200)
	ev := []Evidence{
		commandEvidence(t0, PhaseImplement, "go test ./a", "0"),
		commandEvidence(t0.Add(time.Second), PhaseImplement, long, "0"),
	}
	runs := TestRuns(ev, time.Time{})
	if len(runs) != 2 || runs[0].Command != "go test ./a" {
		t.Fatalf("runs = %+v", runs)
	}
	if n := len([]rune(runs[1].Command)); n != 100 {
		t.Fatalf("command has %d runes, want 100", n)
	}
	if !strings.HasSuffix(runs[1].Command, "…") {
		t.Fatalf("a cut command ends with an ellipsis: %q", runs[1].Command)
	}
}

func TestTestRunsSkipsNonCommandEvidenceAndNonTests(t *testing.T) {
	ev := []Evidence{
		{Phase: PhaseImplement, Kind: KindNote, At: t0, Text: "go test ./a"},
		commandEvidence(t0.Add(time.Second), PhaseImplement, "go build ./...", "0"),
	}
	if runs := TestRuns(ev, time.Time{}); len(runs) != 0 {
		t.Fatalf("runs = %+v, want none", runs)
	}
}

func TestFormatTestRunsShowsTheBlockOrNone(t *testing.T) {
	if got := FormatTestRuns(nil); got != "Test runs this cycle: none recorded" {
		t.Fatalf("none = %q", got)
	}
	runs := []TestRun{
		{Phase: PhaseImplement, Command: "go test ./internal/memory/...", Exit: "non-zero"},
		{Phase: PhaseVerify, Command: "go test ./...", Exit: "0"},
	}
	want := "Test runs this cycle (oldest first):\n" +
		"  1. exit non-zero   go test ./internal/memory/...   (implement)\n" +
		"  2. exit 0" + strings.Repeat(" ", 10) + "go test ./..." + strings.Repeat(" ", 19) + "(verify)"
	if got := FormatTestRuns(runs); got != want {
		t.Fatalf("block =\n%s\nwant\n%s", got, want)
	}
}

func TestStatusShowsTheTestRunsInVerifyOnly(t *testing.T) {
	h := newRig(t, ModeStrict)
	h.open(t, SizeMedium)
	h.ctl.RecordBash(ctx, `{"command":"go test ./a"}`, "", true)
	if out := h.ctl.Status(ctx); strings.Contains(out, "Test runs this cycle") {
		t.Fatalf("implement status has no test block:\n%s", out)
	}
	if _, err := h.ctl.Advance(ctx, PhaseVerify, ""); err != nil {
		t.Fatal(err)
	}
	h.ctl.RecordBash(ctx, `{"command":"go test ./..."}`, "", false)
	out := h.ctl.Status(ctx)
	for _, want := range []string{"Test runs this cycle (oldest first):", "exit non-zero", "go test ./...   (verify)"} {
		if !strings.Contains(out, want) {
			t.Fatalf("verify status misses %q:\n%s", want, out)
		}
	}
	if strings.Contains(h.ctl.ContextBlock(ctx), "Test runs") {
		t.Fatal("the agent's context block never carries the test runs")
	}
}
