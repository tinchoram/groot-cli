//go:build darwin

package sandbox

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The real seatbelt backend reads a secret set the agent was granted, and
// refuses the same file without it.
func TestDarwinSandboxReadsGrantedCredentialsOnly(t *testing.T) {
	home := t.TempDir()
	aws := filepath.Join(home, ".aws")
	if err := os.MkdirAll(aws, 0o700); err != nil {
		t.Fatal(err)
	}
	creds := filepath.Join(aws, "credentials")
	if err := os.WriteFile(creds, []byte("AWS-GRANTED"), 0o600); err != nil {
		t.Fatal(err)
	}
	r := realRunner(t, home)
	space := t.TempDir()

	run := func(readable []string, script string) (string, error) {
		t.Helper()
		cmd, err := r.Wrap(t.Context(), Spec{
			Dir:             space,
			Command:         []string{"/bin/sh", "-c", script},
			WritablePaths:   []string{space},
			ReadableSecrets: readable,
		})
		if err != nil {
			t.Fatalf("Wrap: %v", err)
		}
		out, err := cmd.CombinedOutput()
		return string(out), err
	}

	out, err := run([]string{".aws"}, "cat "+creds)
	if err != nil || !strings.Contains(out, "AWS-GRANTED") {
		t.Fatalf("a granted set must be readable: err=%v out=%q", err, out)
	}
	out, err = run([]string{".aws"}, "echo x > "+filepath.Join(aws, "written"))
	if err == nil {
		t.Fatalf("a granted set must stay read-only, got out=%q", out)
	}
	out, err = run(nil, "cat "+creds)
	if err == nil || strings.Contains(out, "AWS-GRANTED") {
		t.Fatalf("without the set .aws must be denied: err=%v out=%q", err, out)
	}
}
