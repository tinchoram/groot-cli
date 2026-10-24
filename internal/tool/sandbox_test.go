package tool

import (
	"context"
	"errors"
	"os/exec"
	"slices"
	"strings"
	"testing"

	shellsandbox "github.com/tinchoram/groot-cli/internal/sandbox"
)

// recordRunner records the Spec it is asked to wrap and runs the command unwrapped.
type recordRunner struct {
	name string
	err  error
	spec shellsandbox.Spec
}

func (r *recordRunner) Name() string { return r.name }

func (r *recordRunner) Wrap(ctx context.Context, spec shellsandbox.Spec) (*exec.Cmd, error) {
	r.spec = spec
	if r.err != nil {
		return nil, r.err
	}
	return exec.CommandContext(ctx, spec.Command[0], spec.Command[1:]...), nil
}

func TestBashHandsTheSpaceAndPolicyToTheRunner(t *testing.T) {
	dir := t.TempDir()
	rec := &recordRunner{name: shellsandbox.BackendBwrap}
	b := NewBash(dir)
	b.Runner = rec
	b.Network = true
	b.ExtraWritable = []string{"/opt/cache"}

	out, isErr := b.Execute(context.Background(), `{"command":"echo hi"}`)
	if isErr || !strings.Contains(out, "hi") {
		t.Fatalf("out = %q, isErr = %v", out, isErr)
	}
	if rec.spec.Dir != dir {
		t.Errorf("Dir = %q, want %q", rec.spec.Dir, dir)
	}
	if !rec.spec.AllowNetwork {
		t.Error("AllowNetwork must follow bash.sandbox.network")
	}
	if !slices.Equal(rec.spec.WritablePaths, []string{dir, "/opt/cache"}) {
		t.Errorf("WritablePaths = %q", rec.spec.WritablePaths)
	}
	if !slices.Equal(rec.spec.Command, []string{"sh", "-c", "echo hi"}) {
		t.Errorf("Command = %q", rec.spec.Command)
	}
}

func TestBashUsesProcessCwdWhenDirIsEmpty(t *testing.T) {
	rec := &recordRunner{name: shellsandbox.BackendBwrap}
	b := NewBash("")
	b.Runner = rec
	if _, isErr := b.Execute(context.Background(), `{"command":"true"}`); isErr {
		t.Fatal("true must succeed")
	}
	if rec.spec.Dir == "" {
		t.Fatal("the space dir must never be empty: the sandbox needs a writable root")
	}
}

func TestBashNoneBackendWarnsInTheResult(t *testing.T) {
	b := NewBash(t.TempDir())
	b.Runner = &recordRunner{name: shellsandbox.BackendNone}
	out, isErr := b.Execute(context.Background(), `{"command":"echo hi"}`)
	if isErr {
		t.Fatalf("unexpected error: %q", out)
	}
	if !strings.Contains(out, "sandbox: none") || !strings.Contains(out, "hi") {
		t.Fatalf("result must warn and keep the output: %q", out)
	}
}

func TestBashWrapErrorIsAToolError(t *testing.T) {
	b := NewBash(t.TempDir())
	b.Runner = &recordRunner{name: shellsandbox.BackendNone, err: errors.New("bash is refused: install bubblewrap")}
	out, isErr := b.Execute(context.Background(), `{"command":"echo hi"}`)
	if !isErr || !strings.Contains(out, "install bubblewrap") {
		t.Fatalf("out = %q, isErr = %v; want the refusal as an error", out, isErr)
	}
}

func TestBashSandboxNameForTheAuditLog(t *testing.T) {
	b := NewBash(t.TempDir())
	if got := b.SandboxName(); got != shellsandbox.BackendOff {
		t.Fatalf("no runner: SandboxName = %q, want off", got)
	}
	b.Runner = &recordRunner{name: shellsandbox.BackendSandboxExec}
	if got := b.SandboxName(); got != shellsandbox.BackendSandboxExec {
		t.Fatalf("SandboxName = %q, want sandbox-exec", got)
	}
}
