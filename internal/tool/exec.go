package tool

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"strings"
	"syscall"
	"time"

	"github.com/tinchoram/groot-cli/internal/api"
)

const (
	defaultExecTimeout = 60 * time.Second
	bashOutputLimit    = 32 * 1024
	kubectlOutputLimit = 64 * 1024
)

// cappedBuffer keeps at most limit bytes and records that it dropped the rest.
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
		s += fmt.Sprintf("\n... [output truncated at %d KB]", c.limit/1024)
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
func runCapped(ctx context.Context, timeout time.Duration, outLimit int, dir, name string, args ...string) runResult {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Dir = dir
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error {
		if cmd.Process == nil {
			return nil
		}
		return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
	}
	cmd.WaitDelay = time.Second

	out := &cappedBuffer{limit: outLimit}
	cmd.Stdout = out
	cmd.Stderr = out

	err := cmd.Run()
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

// Bash runs a shell command with `sh -c`.
type Bash struct {
	Dir     string
	Timeout time.Duration
}

// NewBash returns the bash tool. Commands run in dir (empty means the process cwd).
func NewBash(dir string) *Bash {
	return &Bash{Dir: dir, Timeout: defaultExecTimeout}
}

type commandInput struct {
	Command string `json:"command"`
}

func (*Bash) Definition() api.ToolDef {
	return api.ToolDef{
		Name:        "bash",
		Description: "Run a shell command with sh -c. Times out after 60 seconds; output is capped at 32 KB.",
		InputSchema: json.RawMessage(`{"type":"object","properties":{"command":{"type":"string"}},"required":["command"]}`),
	}
}

func (*Bash) Risk() Risk { return RiskExec }

func (*Bash) Preview(input string) string {
	var in commandInput
	if err := json.Unmarshal([]byte(input), &in); err != nil || in.Command == "" {
		return "bash: invalid input"
	}
	return Redact(in.Command)
}

func (b *Bash) Execute(ctx context.Context, input string) (string, bool) {
	out, isErr := b.execute(ctx, input)
	return Redact(out), isErr
}

func (b *Bash) execute(ctx context.Context, input string) (string, bool) {
	var in commandInput
	if err := json.Unmarshal([]byte(input), &in); err != nil || strings.TrimSpace(in.Command) == "" {
		return "invalid input: a non-empty command is required", true
	}
	timeout := b.Timeout
	if timeout <= 0 {
		timeout = defaultExecTimeout
	}
	res := runCapped(ctx, timeout, bashOutputLimit, b.Dir, "sh", "-c", in.Command)
	return describeResult(res, timeout, "command")
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
		Description: "Run a read-only kubectl command. Pass args as an array, without the leading 'kubectl'. Allowed verbs: get, describe, logs, top, version, api-resources, explain.",
		InputSchema: json.RawMessage(`{"type":"object","properties":{"args":{"type":"array","items":{"type":"string"},"description":"Arguments, e.g. [\"get\",\"pods\",\"-n\",\"prod\"]"}},"required":["args"]}`),
	}
}

func (*Kubectl) Risk() Risk { return RiskRead }

func (*Kubectl) Preview(input string) string {
	var in kubectlInput
	if err := json.Unmarshal([]byte(input), &in); err != nil {
		return "kubectl: invalid input"
	}
	return Redact("kubectl " + strings.Join(in.Args, " "))
}

func (k *Kubectl) Execute(ctx context.Context, input string) (string, bool) {
	out, isErr := k.execute(ctx, input)
	return Redact(out), isErr
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
	res := runCapped(ctx, timeout, kubectlOutputLimit, "", path, in.Args...)
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
