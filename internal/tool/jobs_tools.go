package tool

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/tinchoram/groot-cli/internal/api"
	"github.com/tinchoram/groot-cli/internal/jobs"
	shellsandbox "github.com/tinchoram/groot-cli/internal/sandbox"
)

// Background jobs: a command keeps running after its call returns, and its
// output or exit comes back as events in a later turn. The start tools share
// the sandbox, the environment and the timeout rules of bash.

// jobRules is the part of a start result that tells the model how events work.
const jobRules = "Its events arrive in a later turn: read output with job_output, end it with job_stop."

// jobLineClip bounds one output line in job_list.
const jobLineClip = 200

// jobTimeout is the limit of a background job. Without timeout_seconds it is
// jobs.DefaultTimeout. With it, the ceiling is the larger of the bash ceiling
// and jobs.DefaultTimeout, so a job can run as long as its default allows.
func (b *Bash) jobTimeout(secs *int) (time.Duration, string, error) {
	if secs == nil {
		return jobs.DefaultTimeout, "", nil
	}
	if *secs <= 0 {
		return 0, "", errors.New("invalid input: timeout_seconds must be a positive integer")
	}
	ceiling := max(b.maxTimeout(), jobs.DefaultTimeout)
	maxSecs := int(ceiling / time.Second)
	if *secs > maxSecs {
		return ceiling, fmt.Sprintf("[bash: timeout_seconds %d clamped to %d]\n", *secs, maxSecs), nil
	}
	return time.Duration(*secs) * time.Second, "", nil
}

// startJob runs command as a job of m, in the sandbox that bash would use.
func startJob(ctx context.Context, b *Bash, m *jobs.Manager, toolName, command string, secs *int, monitor bool, pattern *regexp.Regexp) (jobs.Job, string, error) {
	timeout, notice, err := b.jobTimeout(secs)
	if err != nil {
		return jobs.Job{}, "", err
	}
	job, err := m.Start(ctx, jobs.StartSpec{
		Command: command,
		Timeout: timeout,
		Monitor: monitor,
		Pattern: pattern,
		Build:   b.buildCommand(command),
		Tool:    toolName,
		Sandbox: b.SandboxName(),
	})
	return job, notice, err
}

// noSandboxSuffix repeats bash's warning on a start result when no sandbox is active.
func (b *Bash) noSandboxSuffix() string {
	if b.Runner != nil && b.Runner.Name() == shellsandbox.BackendNone {
		return " " + noSandboxNotice
	}
	return ""
}

// jobStartInput is the input of bash_background and monitor.
type jobStartInput struct {
	Command        string `json:"command"`
	Pattern        string `json:"pattern"`
	TimeoutSeconds *int   `json:"timeout_seconds"`
}

func decodeStart(input string) (jobStartInput, error) {
	var in jobStartInput
	if err := json.Unmarshal([]byte(input), &in); err != nil || strings.TrimSpace(in.Command) == "" {
		return in, errors.New("invalid input: a non-empty command is required")
	}
	return in, nil
}

// BashBackground starts a shell command as a background job and returns at once.
// Its risk is always exec: the command keeps running after the call.
type BashBackground struct {
	bash *Bash
	m    *jobs.Manager
}

// NewBashBackground returns the bash_background tool. It runs commands like b and keeps its jobs in m.
func NewBashBackground(b *Bash, m *jobs.Manager) *BashBackground {
	return &BashBackground{bash: b, m: m}
}

func (*BashBackground) Definition() api.ToolDef {
	return api.ToolDef{
		Name: "bash_background",
		Description: "Start a shell command in the background and keep working. Use it for commands that run for a long time: a server, a build or test run you do not want to wait for, a rollout you check later. " +
			"Use bash for anything that finishes within a minute or two. The result comes back as an event in a later turn, not in this result. " +
			"Default timeout 30 minutes; timeout_seconds sets another limit, up to the configured maximum. At most " + strconv.Itoa(jobs.DefaultLimit) + " jobs run at once.",
		InputSchema: json.RawMessage(`{"type":"object","properties":{"command":{"type":"string"},"timeout_seconds":{"type":"integer"}},"required":["command"]}`),
	}
}

func (*BashBackground) Risk() Risk            { return RiskExec }
func (*BashBackground) RiskFor(string) Risk   { return RiskExec }
func (t *BashBackground) SandboxName() string { return t.bash.SandboxName() }
func (t *BashBackground) DestructiveFor(input string) (DestructiveKind, string, bool) {
	return t.bash.DestructiveFor(input)
}

func (*BashBackground) Preview(input string) string {
	in, err := decodeStart(input)
	if err != nil {
		return "bash_background: invalid input"
	}
	return "bash_background: " + Redact(in.Command)
}

func (t *BashBackground) Execute(ctx context.Context, input string) (string, bool) {
	out, isErr := t.execute(ctx, input)
	return Redact(out), isErr
}

func (t *BashBackground) execute(ctx context.Context, input string) (string, bool) {
	in, err := decodeStart(input)
	if err != nil {
		return err.Error(), true
	}
	job, notice, err := startJob(ctx, t.bash, t.m, "bash_background", in.Command, in.TimeoutSeconds, false, nil)
	if err != nil {
		return err.Error(), true
	}
	return fmt.Sprintf("%sjob #%d started: %s. %s%s", notice, job.ID, in.Command, jobRules, t.bash.noSandboxSuffix()), false
}

// Monitor starts a command and turns its output lines into events. Only lines
// that match the optional pattern become events; the exit always does.
type Monitor struct {
	bash *Bash
	m    *jobs.Manager
}

// NewMonitor returns the monitor tool. It runs commands like b and keeps its jobs in m.
func NewMonitor(b *Bash, m *jobs.Manager) *Monitor {
	return &Monitor{bash: b, m: m}
}

func (*Monitor) Definition() api.ToolDef {
	return api.ToolDef{
		Name: "monitor",
		Description: "Watch a shell command's output and get an event for each matching line, without polling. Use it to follow a log, wait for a rollout to report ready, or catch an error as it happens. " +
			"pattern is a Go regular expression that selects the lines; without it every line is an event. Each job sends at most 50 line events, then a notice that the cap was reached; its exit is always reported. " +
			"Events arrive in later turns, not in this result. Use bash_background when only the exit matters. Same timeout and sandbox rules as bash_background.",
		InputSchema: json.RawMessage(`{"type":"object","properties":{"command":{"type":"string"},"pattern":{"type":"string","description":"Go regular expression; only matching lines become events"},"timeout_seconds":{"type":"integer"}},"required":["command"]}`),
	}
}

func (*Monitor) Risk() Risk            { return RiskExec }
func (*Monitor) RiskFor(string) Risk   { return RiskExec }
func (t *Monitor) SandboxName() string { return t.bash.SandboxName() }
func (t *Monitor) DestructiveFor(input string) (DestructiveKind, string, bool) {
	return t.bash.DestructiveFor(input)
}

func (*Monitor) Preview(input string) string {
	in, err := decodeStart(input)
	if err != nil {
		return "monitor: invalid input"
	}
	if in.Pattern != "" {
		return "monitor: " + Redact(in.Command) + " (pattern " + Redact(in.Pattern) + ")"
	}
	return "monitor: " + Redact(in.Command)
}

func (t *Monitor) Execute(ctx context.Context, input string) (string, bool) {
	out, isErr := t.execute(ctx, input)
	return Redact(out), isErr
}

func (t *Monitor) execute(ctx context.Context, input string) (string, bool) {
	in, err := decodeStart(input)
	if err != nil {
		return err.Error(), true
	}
	var re *regexp.Regexp
	if in.Pattern != "" {
		re, err = regexp.Compile(in.Pattern)
		if err != nil {
			return "invalid pattern: " + err.Error(), true
		}
	}
	job, notice, err := startJob(ctx, t.bash, t.m, "monitor", in.Command, in.TimeoutSeconds, true, re)
	if err != nil {
		return err.Error(), true
	}
	filter := ""
	if in.Pattern != "" {
		filter = fmt.Sprintf(", pattern %q", in.Pattern)
	}
	return fmt.Sprintf("%sjob #%d watching: %s%s. Matching lines arrive as events in later turns, at most %d; the exit arrives too. Read output with job_output, end it with job_stop.%s",
		notice, job.ID, in.Command, filter, jobs.MonitorCap, t.bash.noSandboxSuffix()), false
}

// JobList shows the jobs of the conversation with their state and last lines.
type JobList struct {
	m *jobs.Manager
}

// NewJobList returns the job_list tool for m.
func NewJobList(m *jobs.Manager) *JobList { return &JobList{m: m} }

func (*JobList) Definition() api.ToolDef {
	return api.ToolDef{
		Name:        "job_list",
		Description: "List the background jobs of this conversation: id, command, state (running, exited with its code, or stopped), start time and the last five output lines of each. Use it to check on a job before reading its output with job_output.",
		InputSchema: json.RawMessage(`{"type":"object","properties":{}}`),
	}
}

func (*JobList) Risk() Risk            { return RiskRead }
func (*JobList) RiskFor(string) Risk   { return RiskRead }
func (*JobList) Untrusted() bool       { return true }
func (*JobList) Preview(string) string { return "job_list" }

func (t *JobList) Execute(_ context.Context, input string) (string, bool) {
	return Redact(t.list()), false
}

func (t *JobList) list() string {
	list := t.m.List()
	if len(list) == 0 {
		return "no background jobs in this conversation"
	}
	var buf bytes.Buffer
	tw := tabwriter.NewWriter(&buf, 0, 2, 2, ' ', 0)
	fmt.Fprintln(tw, "ID\tSTATE\tEXIT\tSTARTED\tCOMMAND")
	for _, j := range list {
		exit := "-"
		if j.State != jobs.Running {
			exit = strconv.Itoa(j.ExitCode)
		}
		fmt.Fprintf(tw, "#%d\t%s\t%s\t%s\t%s\n", j.ID, j.State, exit, ago(j.Started), oneLine(j.Command))
	}
	_ = tw.Flush()
	for _, j := range list {
		fmt.Fprintf(&buf, "\n#%d last lines:\n", j.ID)
		tail := t.m.Tail(j.ID, 5)
		if len(tail) == 0 {
			buf.WriteString("  (no output yet)\n")
		}
		for _, line := range tail {
			fmt.Fprintf(&buf, "  | %s\n", clipLine(line, jobLineClip))
		}
	}
	return strings.TrimRight(buf.String(), "\n")
}

// JobOutput returns the buffered output of one job. Like a bash result, text
// over the inline limit is saved under the space's .groot/outputs.
type JobOutput struct {
	m   *jobs.Manager
	dir string
}

// NewJobOutput returns the job_output tool for m. dir is the space directory
// where long output is saved; empty means the working directory.
func NewJobOutput(m *jobs.Manager, dir string) *JobOutput { return &JobOutput{m: m, dir: dir} }

func (*JobOutput) Definition() api.ToolDef {
	return api.ToolDef{
		Name:        "job_output",
		Description: "Return the buffered output of a background job (the last 64 KB it printed), capped like a bash result. Use it to read a log or to see why a job failed.",
		InputSchema: json.RawMessage(`{"type":"object","properties":{"id":{"type":"integer","description":"job number from bash_background or job_list"}},"required":["id"]}`),
	}
}

func (*JobOutput) Risk() Risk          { return RiskRead }
func (*JobOutput) RiskFor(string) Risk { return RiskRead }
func (*JobOutput) Untrusted() bool     { return true }

func (*JobOutput) Preview(input string) string {
	id, err := jobID(input)
	if err != nil {
		return "job_output: invalid input"
	}
	return fmt.Sprintf("job_output #%d", id)
}

func (t *JobOutput) Execute(_ context.Context, input string) (string, bool) {
	id, err := jobID(input)
	if err != nil {
		return err.Error(), true
	}
	job, ok := t.m.Get(id)
	if !ok {
		return fmt.Sprintf("no job #%d", id), true
	}
	text, _ := t.m.Output(id)
	if text == "" {
		text = "(no output)"
	}
	header := fmt.Sprintf("job #%d %s%s: %s\n", job.ID, job.State, exitSuffix(job), oneLine(job.Command))
	return Redact(header) + finishOutput(t.dir, "job_output", text), false
}

// JobStop ends a running job: SIGTERM, then SIGKILL after 5 seconds.
type JobStop struct {
	m *jobs.Manager
}

// NewJobStop returns the job_stop tool for m.
func NewJobStop(m *jobs.Manager) *JobStop { return &JobStop{m: m} }

func (*JobStop) Definition() api.ToolDef {
	return api.ToolDef{
		Name:        "job_stop",
		Description: "Stop a background job: SIGTERM to its process group, then SIGKILL after 5 seconds. Use it when a job is no longer needed or is stuck. Stopping a job that already ended changes nothing.",
		InputSchema: json.RawMessage(`{"type":"object","properties":{"id":{"type":"integer","description":"job number from bash_background or job_list"}},"required":["id"]}`),
	}
}

func (*JobStop) Risk() Risk          { return RiskExec }
func (*JobStop) RiskFor(string) Risk { return RiskExec }

func (*JobStop) Preview(input string) string {
	id, err := jobID(input)
	if err != nil {
		return "job_stop: invalid input"
	}
	return fmt.Sprintf("job_stop #%d", id)
}

func (t *JobStop) Execute(_ context.Context, input string) (string, bool) {
	id, err := jobID(input)
	if err != nil {
		return err.Error(), true
	}
	job, err := t.m.Stop(id)
	if err != nil {
		return err.Error(), true
	}
	return fmt.Sprintf("job #%d %s%s: %s", job.ID, job.State, exitSuffix(job), oneLine(job.Command)), false
}

// jobID decodes the id of job_output and job_stop.
func jobID(input string) (int, error) {
	var in struct {
		ID int `json:"id"`
	}
	if err := json.Unmarshal([]byte(input), &in); err != nil {
		return 0, fmt.Errorf("invalid input: %v", err)
	}
	if in.ID <= 0 {
		return 0, errors.New("invalid input: id must be the job number (see job_list)")
	}
	return in.ID, nil
}

// exitSuffix names the exit code of an ended job; running jobs have none.
func exitSuffix(j jobs.Job) string {
	if j.State == jobs.Running {
		return ""
	}
	return fmt.Sprintf(" (exit %d)", j.ExitCode)
}

// ago prints a time as a short relative age, such as "42s ago" or "3h ago".
func ago(t time.Time) string {
	d := time.Since(t).Round(time.Second)
	switch {
	case d < time.Minute:
		return fmt.Sprintf("%ds ago", int(d.Seconds()))
	case d < time.Hour:
		return fmt.Sprintf("%dm ago", int(d.Minutes()))
	case d < 48*time.Hour:
		return fmt.Sprintf("%dh ago", int(d.Hours()))
	}
	return fmt.Sprintf("%dd ago", int(d.Hours()/24))
}

// oneLine keeps a command on one line in a table.
func oneLine(s string) string {
	return strings.ReplaceAll(strings.TrimSpace(s), "\n", "; ")
}

// clipLine shortens a line to n bytes, keeping valid UTF-8.
func clipLine(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return strings.ToValidUTF8(s[:n], "") + "..."
}
