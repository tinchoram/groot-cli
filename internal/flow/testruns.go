package flow

import (
	"fmt"
	"regexp"
	"strings"
	"time"
)

// TestRun is one test-runner command of the current implement cycle, as its
// command evidence recorded it. Exit is "0" or "non-zero", as RecordBash stores it.
type TestRun struct {
	At      time.Time
	Phase   Phase
	Command string
	Exit    string
}

// testRunner matches the test-runner commands: word-bounded, case-insensitive,
// anywhere in the command line.
var testRunner = regexp.MustCompile(`(?i)\b(go\s+test|pytest|npm\s+test|pnpm\s+test|yarn\s+test|vitest|jest|cargo\s+test|mvn\s+test|gradle\s+test|dotnet\s+test|make\s+test|bun\s+test|rspec|phpunit)\b`)

// maxRunCommand is the length of a test run's command, in runes, including the
// ellipsis when it is cut.
const maxRunCommand = 100

// CycleStart is the start of the current implement cycle. A cycle starts when
// the task enters implement; every return to implement from verify or deliver
// records a note in implement with its reason, so the time of the newest such
// note is the start. A task that has not returned yet has no such note: its
// first cycle starts at zero, which is correct because command evidence exists
// only in implement and verify, so nothing precedes the first implement.
func CycleStart(evidence []Evidence) time.Time {
	for i := len(evidence) - 1; i >= 0; i-- {
		e := evidence[i]
		if e.Kind == KindNote && e.Phase == PhaseImplement {
			return e.At
		}
	}
	return time.Time{}
}

// TestRuns selects the test-runner commands among the command evidence at or
// after since, oldest first. Callers pass CycleStart(evidence) to see the runs
// of the current cycle.
func TestRuns(evidence []Evidence, since time.Time) []TestRun {
	var out []TestRun
	for _, e := range evidence {
		if e.Kind != KindCommand || e.At.Before(since) {
			continue
		}
		command, exit, ok := parseCommandEvidence(e.Text)
		if !ok || !testRunner.MatchString(command) {
			continue
		}
		out = append(out, TestRun{At: e.At, Phase: e.Phase, Command: cutTo(command, maxRunCommand), Exit: exit})
	}
	return out
}

// parseCommandEvidence reads the text RecordBash stores: "$ <command>", the
// output, and "exit: <status>" as the last line.
func parseCommandEvidence(text string) (command, exit string, ok bool) {
	lines := strings.Split(text, "\n")
	if len(lines) < 2 || !strings.HasPrefix(lines[0], "$ ") {
		return "", "", false
	}
	last := lines[len(lines)-1]
	if !strings.HasPrefix(last, "exit: ") {
		return "", "", false
	}
	return strings.TrimPrefix(lines[0], "$ "), strings.TrimPrefix(last, "exit: "), true
}

// FormatTestRuns is the block that the delivery review and task status show. It
// lists the runs with their exit codes and states no verdict.
func FormatTestRuns(runs []TestRun) string {
	if len(runs) == 0 {
		return "Test runs this cycle: none recorded"
	}
	width := 0
	for _, r := range runs {
		width = max(width, len([]rune(r.Command)))
	}
	var b strings.Builder
	b.WriteString("Test runs this cycle (oldest first):")
	for i, r := range runs {
		fmt.Fprintf(&b, "\n  %d. %-13s   %-*s   (%s)", i+1, "exit "+r.Exit, width, r.Command, r.Phase)
	}
	return b.String()
}

// cutTo keeps at most n runes of s, the last one an ellipsis when it cuts.
func cutTo(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n-1]) + "…"
}
