package tool

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"syscall"
	"time"

	"github.com/tinchoram/groot-cli/internal/api"
	shellsandbox "github.com/tinchoram/groot-cli/internal/sandbox"
)

const (
	defaultExecTimeout    = 60 * time.Second
	defaultExecMaxTimeout = 10 * time.Minute
)

// cappedBuffer keeps at most limit bytes and records that it dropped the rest.
// The limit is maxCapturedOutput: the saved output of a call is the full text
// up to that ceiling (see finishOutput).
type cappedBuffer struct {
	buf       bytes.Buffer
	limit     int
	truncated bool
}

func (c *cappedBuffer) Write(p []byte) (int, error) {
	room := c.limit - c.buf.Len()
	if room > 0 {
		if len(p) <= room {
			c.buf.Write(p)
		} else {
			c.buf.Write(p[:room])
			c.truncated = true
		}
	} else if len(p) > 0 {
		c.truncated = true
	}
	return len(p), nil
}

func (c *cappedBuffer) String() string {
	s := c.buf.String()
	if c.truncated {
		s += fmt.Sprintf("\n... [output capture stopped at %d MiB]", c.limit>>20)
	}
	return s
}

type runResult struct {
	output   string
	exitCode int
	timedOut bool
	startErr error
}

// runCapped runs a process with a timeout, capped output, and a process group
// kill so that children (for example `sleep` under `sh -c`) die with it.
func runCapped(ctx context.Context, timeout time.Duration, dir, name string, args ...string) runResult {
	return runCmd(ctx, timeout, func(c context.Context) (*exec.Cmd, error) {
		cmd := exec.CommandContext(c, name, args...)
		cmd.Dir = dir
		return cmd, nil
	})
}

// runCmd runs the command that build returns, with the limits of runCapped.
// build receives the context that carries the timeout, so a sandbox wrapper
// can bind the command to it.
func runCmd(ctx context.Context, timeout time.Duration, build func(context.Context) (*exec.Cmd, error)) runResult {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	cmd, err := build(ctx)
	if err != nil {
		return runResult{startErr: err}
	}
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error {
		if cmd.Process == nil {
			return nil
		}
		return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
	}
	cmd.WaitDelay = time.Second

	out := &cappedBuffer{limit: maxCapturedOutput}
	cmd.Stdout = out
	cmd.Stderr = out

	err = cmd.Run()
	res := runResult{output: out.String()}
	if errors.Is(ctx.Err(), context.DeadlineExceeded) {
		res.timedOut = true
		return res
	}
	var exitErr *exec.ExitError
	switch {
	case err == nil:
	case errors.As(err, &exitErr):
		res.exitCode = exitErr.ExitCode()
	default:
		res.startErr = err
	}
	return res
}

// Bash runs a shell command with `sh -c`, inside Runner when one is set.
// Network and ExtraWritable are the bash.sandbox settings that the runner
// applies to every command. A nil Runner runs commands unwrapped.
type Bash struct {
	Dir           string
	Timeout       time.Duration
	MaxTimeout    time.Duration // ceiling for a call's timeout_seconds
	Runner        shellsandbox.Runner
	Network       bool
	ExtraWritable []string
}

// noSandboxNotice is prefixed to the output of a command that ran without a sandbox.
const noSandboxNotice = "[sandbox: none] this command ran with your full permissions"

// SandboxName reports the backend that confines the commands, for the audit log.
func (b *Bash) SandboxName() string {
	if b.Runner == nil {
		return shellsandbox.BackendOff
	}
	return b.Runner.Name()
}

// NewBash returns the bash tool. Commands run in dir (empty means the process cwd).
func NewBash(dir string) *Bash {
	return &Bash{Dir: dir, Timeout: defaultExecTimeout, MaxTimeout: defaultExecMaxTimeout}
}

type commandInput struct {
	Command        string `json:"command"`
	TimeoutSeconds *int   `json:"timeout_seconds"`
}

func (*Bash) Definition() api.ToolDef {
	return api.ToolDef{
		Name:        "bash",
		Description: "Run a shell command with sh -c in the space and return its output and exit code. Use it for builds, tests, git and command-line tools that no other tool covers; for reading files and searching names, prefer read_file, list_dir or glob. A command still running at its timeout is stopped, so pass timeout_seconds for a slow test suite instead of backgrounding it. Output over 30,000 characters is saved in full under .groot/outputs and the result names the file; a non-zero exit is reported as an error.",
		InputSchema: json.RawMessage(`{"type":"object","properties":{` +
			`"command":{"type":"string","description":"The shell command line, run with sh -c in the space directory"},` +
			`"timeout_seconds":{"type":"integer","description":"Seconds before the command is stopped; default 60, and the ceiling comes from the bash config (600 by default)"}},` +
			`"required":["command"]}`),
	}
}

func (*Bash) Risk() Risk { return RiskExec }

// Untrusted: command output carries whatever the files and services print.
func (*Bash) Untrusted() bool { return true }

func (*Bash) Preview(input string) string {
	var in commandInput
	if err := json.Unmarshal([]byte(input), &in); err != nil || in.Command == "" {
		return "bash: invalid input"
	}
	return Redact(in.Command)
}

func (b *Bash) Execute(ctx context.Context, input string) (string, bool) {
	out, isErr := b.execute(ctx, input)
	return finishOutput(b.Dir, "bash", out), isErr
}

func (b *Bash) execute(ctx context.Context, input string) (string, bool) {
	var in commandInput
	if err := json.Unmarshal([]byte(input), &in); err != nil || strings.TrimSpace(in.Command) == "" {
		return "invalid input: a non-empty command is required", true
	}
	timeout, notice, err := b.callTimeout(in.TimeoutSeconds)
	if err != nil {
		return err.Error(), true
	}
	out, isErr := b.run(ctx, in.Command, timeout, b.maxTimeout())
	return notice + out, isErr
}

// maxTimeout is the ceiling for timeout_seconds, the default when unset.
func (b *Bash) maxTimeout() time.Duration {
	if b.MaxTimeout <= 0 {
		return defaultExecMaxTimeout
	}
	return b.MaxTimeout
}

// callTimeout resolves the timeout_seconds of a call: the configured timeout
// when the call names none, the value when it fits the ceiling, and the ceiling
// when it does not, with a notice that says so.
func (b *Bash) callTimeout(secs *int) (time.Duration, string, error) {
	timeout := b.Timeout
	if timeout <= 0 {
		timeout = defaultExecTimeout
	}
	if secs == nil {
		return timeout, "", nil
	}
	if *secs <= 0 {
		return 0, "", errors.New("invalid input: timeout_seconds must be a positive integer")
	}
	maxTimeout := b.maxTimeout()
	maxSecs := int(maxTimeout / time.Second)
	if *secs > maxSecs {
		return maxTimeout, fmt.Sprintf("[bash: timeout_seconds %d clamped to %d]\n", *secs, maxSecs), nil
	}
	return time.Duration(*secs) * time.Second, "", nil
}

// sandboxSpec describes one `sh -c` command for the Runner: the working
// directory, the network and the extra writable paths of the bash settings.
// Foreground and background commands both use it, so they get the same sandbox.
func (b *Bash) sandboxSpec(command string) shellsandbox.Spec {
	dir := b.Dir
	if dir == "" {
		dir, _ = os.Getwd()
	}
	return shellsandbox.Spec{
		Dir:           dir,
		Command:       []string{"sh", "-c", command},
		AllowNetwork:  b.Network,
		WritablePaths: append([]string{dir}, b.ExtraWritable...),
	}
}

// buildCommand returns the builder of one command for runCmd or a job: the
// Runner wraps it, or without a Runner it runs unwrapped in Dir.
func (b *Bash) buildCommand(command string) func(context.Context) (*exec.Cmd, error) {
	if b.Runner == nil {
		return func(c context.Context) (*exec.Cmd, error) {
			cmd := exec.CommandContext(c, "sh", "-c", command)
			cmd.Dir = b.Dir
			return cmd, nil
		}
	}
	spec := b.sandboxSpec(command)
	return func(c context.Context) (*exec.Cmd, error) {
		return b.Runner.Wrap(c, spec)
	}
}

func (b *Bash) run(ctx context.Context, command string, timeout, maxTimeout time.Duration) (string, bool) {
	if b.Runner == nil {
		res := runCapped(ctx, timeout, b.Dir, "sh", "-c", command)
		return describeBash(res, timeout, maxTimeout)
	}
	res := runCmd(ctx, timeout, b.buildCommand(command))
	out, isErr := describeBash(res, timeout, maxTimeout)
	if b.Runner.Name() == shellsandbox.BackendNone && res.startErr == nil {
		out = noSandboxNotice + "\n" + out
	}
	return out, isErr
}

// describeBash is describeResult with a timeout message that names the limit
// and the call that raises it.
func describeBash(res runResult, timeout, maxTimeout time.Duration) (string, bool) {
	if res.timedOut {
		return strings.TrimRight(res.output, "\n") + fmt.Sprintf("\n[command timed out after %s; rerun with timeout_seconds up to %d if it needs longer]", formatLimit(timeout), int(maxTimeout/time.Second)), true
	}
	return describeResult(res, timeout, "command")
}

// formatLimit prints whole seconds as "60s", not Duration's "1m0s".
func formatLimit(d time.Duration) string {
	if d%time.Second == 0 {
		return fmt.Sprintf("%ds", int(d/time.Second))
	}
	return d.String()
}

func describeResult(res runResult, timeout time.Duration, what string) (string, bool) {
	switch {
	case res.startErr != nil:
		return res.startErr.Error(), true
	case res.timedOut:
		return strings.TrimRight(res.output, "\n") + fmt.Sprintf("\n[%s timed out after %s]", what, timeout), true
	case res.exitCode != 0:
		return strings.TrimRight(res.output, "\n") + fmt.Sprintf("\n[exit %d]", res.exitCode), true
	case res.output == "":
		return "(no output)", false
	}
	return res.output, false
}

// Kubectl runs a read-only subset of kubectl with validated arguments.
type Kubectl struct {
	Binary     string
	Namespaces []string
	Timeout    time.Duration
	OutDir     string // space dir whose .groot/outputs keeps long output; empty means the working directory
}

// NewKubectl returns the kubectl tool. An empty binary means "kubectl" on PATH.
// An empty namespaces slice means any namespace is allowed.
func NewKubectl(binary string, namespaces []string) *Kubectl {
	if binary == "" {
		binary = "kubectl"
	}
	return &Kubectl{Binary: binary, Namespaces: namespaces, Timeout: defaultExecTimeout}
}

type kubectlInput struct {
	Args []string `json:"args"`
}

var (
	kubectlVerbs = map[string]bool{
		"get": true, "describe": true, "logs": true, "top": true,
		"version": true, "api-resources": true, "explain": true,
	}
	kubectlNamespacedVerbs = map[string]bool{"get": true, "describe": true, "logs": true, "top": true}
	kubectlDeniedFlags     = map[string]bool{
		"--kubeconfig": true, "--token": true, "--as": true, "--as-group": true, "--as-uid": true,
		"--client-certificate": true, "--client-key": true, "--certificate-authority": true,
		"--server": true, "-s": true, "--cluster": true, "--user": true, "--context": true,
		"--username": true, "--password": true, "--auth-provider": true, "--insecure-skip-tls-verify": true,
	}
)

// ValidateKubectlArgs enforces the verb allowlist, the credential and cluster
// override denylist, and the namespace allowlist (empty means unrestricted).
func ValidateKubectlArgs(args []string, namespaces []string) error {
	if len(args) == 0 {
		return errors.New("kubectl args are empty")
	}
	verb := args[0]
	if !kubectlVerbs[verb] {
		return fmt.Errorf("verb %q is not allowed (allowed: get, describe, logs, top, version, api-resources, explain)", verb)
	}

	var (
		seenNamespaces []string
		allNamespaces  bool
	)
	for i := 0; i < len(args); i++ {
		a := args[i]
		name := a
		if eq := strings.IndexByte(a, '='); eq > 0 {
			name = a[:eq]
		}
		if kubectlDeniedFlags[name] {
			return fmt.Errorf("flag %s is not allowed: credential and cluster overrides are blocked", name)
		}
		switch {
		case a == "-A" || a == "--all-namespaces":
			allNamespaces = true
		case a == "-n" || a == "--namespace":
			if i+1 >= len(args) {
				return fmt.Errorf("flag %s needs a value", a)
			}
			seenNamespaces = append(seenNamespaces, args[i+1])
			i++
		case strings.HasPrefix(a, "--namespace="):
			seenNamespaces = append(seenNamespaces, strings.TrimPrefix(a, "--namespace="))
		case strings.HasPrefix(a, "-n") && !strings.HasPrefix(a, "--") && len(a) > 2:
			seenNamespaces = append(seenNamespaces, a[2:])
		}
	}

	if len(namespaces) == 0 {
		return nil
	}
	if allNamespaces {
		return errors.New("all-namespaces is not allowed when a namespace allowlist is configured")
	}
	if kubectlNamespacedVerbs[verb] && len(seenNamespaces) == 0 {
		return errors.New("a namespace is required (-n <namespace>) when a namespace allowlist is configured")
	}
	for _, ns := range seenNamespaces {
		if !contains(namespaces, ns) {
			return fmt.Errorf("namespace %q is not in the allowlist (allowed: %s)", ns, strings.Join(namespaces, ", "))
		}
	}
	return nil
}

func contains(list []string, v string) bool {
	for _, x := range list {
		if x == v {
			return true
		}
	}
	return false
}

func (*Kubectl) Definition() api.ToolDef {
	return api.ToolDef{
		Name:        "kubectl",
		Description: "Run a read-only kubectl command against the configured cluster and return its output. Use it to inspect pods, logs, events, resource usage and API resources; use bash only for work this tool cannot do. Verbs other than get, describe, logs, top, version, api-resources and explain, credential or cluster overrides, and namespaces outside the configured allowlist are refused before anything runs. Output over 30,000 characters is saved in full under .groot/outputs and the result names the file.",
		InputSchema: json.RawMessage(`{"type":"object","properties":{"args":{"type":"array","items":{"type":"string"},"description":"Arguments as strings, without the leading kubectl, for example [\"get\",\"pods\",\"-n\",\"prod\"]"}},"required":["args"]}`),
	}
}

func (*Kubectl) Risk() Risk { return RiskRead }

// Untrusted: resource names, logs and events carry text from the cluster.
func (*Kubectl) Untrusted() bool { return true }

func (*Kubectl) Preview(input string) string {
	var in kubectlInput
	if err := json.Unmarshal([]byte(input), &in); err != nil {
		return "kubectl: invalid input"
	}
	return Redact("kubectl " + strings.Join(in.Args, " "))
}

func (k *Kubectl) Execute(ctx context.Context, input string) (string, bool) {
	out, isErr := k.execute(ctx, input)
	return finishOutput(k.OutDir, "kubectl", out), isErr
}

func (k *Kubectl) execute(ctx context.Context, input string) (string, bool) {
	var in kubectlInput
	if err := json.Unmarshal([]byte(input), &in); err != nil {
		return "invalid input: " + err.Error(), true
	}
	if err := ValidateKubectlArgs(in.Args, k.Namespaces); err != nil {
		return err.Error(), true
	}
	path, err := exec.LookPath(k.Binary)
	if err != nil {
		return fmt.Sprintf("kubectl binary %q not found on PATH: install kubectl or fix the configured binary", k.Binary), true
	}
	timeout := k.Timeout
	if timeout <= 0 {
		timeout = defaultExecTimeout
	}
	res := runCapped(ctx, timeout, "", path, in.Args...)
	return describeResult(res, timeout, "kubectl")
}

// RiskFor classifies the command: read-only commands are RiskRead, everything
// else is RiskExec. Unreadable input is RiskExec.
func (b *Bash) RiskFor(input string) Risk {
	var in commandInput
	if err := json.Unmarshal([]byte(input), &in); err != nil {
		return RiskExec
	}
	return ClassifyShell(in.Command)
}

func (k *Kubectl) RiskFor(string) Risk { return k.Risk() }

// Destructive is implemented by tools whose input can carry a destructive command.
type Destructive interface {
	DestructiveFor(input string) (kind DestructiveKind, reason string, ok bool)
}

var _ Destructive = (*Bash)(nil)

// DestructiveFor classifies the command of the input. Unreadable input is not
// reported as destructive; RiskFor already treats it as exec.
func (b *Bash) DestructiveFor(input string) (DestructiveKind, string, bool) {
	var in commandInput
	if err := json.Unmarshal([]byte(input), &in); err != nil {
		return "", "", false
	}
	return ClassifyDestructive(in.Command)
}
