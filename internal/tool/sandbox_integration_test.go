//go:build darwin || linux

package tool

import (
	"context"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	shellsandbox "github.com/tinchoram/groot-cli/internal/sandbox"
)

// TestBashThroughTheRealSandbox drives the bash tool with the sandbox backend of
// this host: a write in the space works, a write to the home directory fails.
func TestBashThroughTheRealSandbox(t *testing.T) {
	runner, _ := shellsandbox.Detect(shellsandbox.Config{Mode: "auto"})
	if runner.Name() == shellsandbox.BackendNone {
		t.Skip("no sandbox backend on this host")
	}
	space := t.TempDir()
	b := NewBash(space)
	b.Runner = runner
	if b.SandboxName() != runner.Name() {
		t.Fatalf("SandboxName = %q, want %q", b.SandboxName(), runner.Name())
	}
	ctx := context.Background()

	out, isErr := b.Execute(ctx, `{"command":"echo ok > inside.txt && cat inside.txt"}`)
	if isErr || !strings.Contains(out, "ok") {
		t.Fatalf("write inside the space must work: isErr=%v out=%q", isErr, out)
	}

	home, err := os.UserHomeDir()
	if err != nil {
		t.Skip("no home directory")
	}
	target := filepath.Join(home, ".groot-sandbox-probe-"+strconv.FormatInt(time.Now().UnixNano(), 36))
	t.Cleanup(func() { _ = os.Remove(target) })
	out, isErr = b.Execute(ctx, `{"command":"echo x > `+target+`"}`)
	if !isErr {
		t.Fatalf("write to the home directory must fail: out=%q", out)
	}
	if _, statErr := os.Stat(target); statErr == nil {
		t.Fatalf("the sandbox let the file be created at %s", target)
	}
}
