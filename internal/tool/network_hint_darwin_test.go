//go:build darwin

package tool

import (
	"context"
	"os"
	"os/exec"
	"strings"
	"testing"

	shellsandbox "github.com/tinchoram/groot-cli/internal/sandbox"
)

// A real sandbox-exec run with the network denied: curl reports that the host
// cannot be resolved, and the hint names the setting that would allow it.
func TestDarwinSandboxNetworkDenialGetsTheHint(t *testing.T) {
	if _, err := exec.LookPath("curl"); err != nil {
		t.Skip("curl is not installed")
	}
	r, warn := shellsandbox.Detect(shellsandbox.Config{Mode: "auto"})
	if r.Name() != shellsandbox.BackendSandboxExec || warn != "" {
		if os.Getenv("GROOT_REQUIRE_SANDBOX") != "" {
			t.Fatalf("GROOT_REQUIRE_SANDBOX is set and sandbox-exec is unavailable: %q (warning %q)", r.Name(), warn)
		}
		t.Skipf("sandbox-exec is unavailable: %q", r.Name())
	}
	b := NewBash(t.TempDir())
	b.Runner = r
	b.Network = false

	out, isErr := b.Execute(context.Background(), `{"command":"curl -sS --max-time 5 https://example.com"}`)
	if !isErr {
		t.Fatalf("the request must fail with the network denied, got %q", out)
	}
	if !strings.Contains(out, "Could not resolve host") || !strings.HasSuffix(strings.TrimSpace(out), networkHint) {
		t.Fatalf("out = %q, want the resolver failure and the hint", out)
	}
}
