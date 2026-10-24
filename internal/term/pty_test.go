package term

import (
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// waitShell blocks until the shell has drawn something, so that input typed
// next is not swallowed by its start-up.
func waitShell(t *testing.T, p Terminal) {
	t.Helper()
	deadline := time.After(10 * time.Second)
	for {
		for _, r := range p.Render() {
			if strings.TrimSpace(r) != "" {
				return
			}
		}
		select {
		case <-p.Output():
		case <-deadline:
			t.Fatal("the shell never drew a prompt")
		case <-time.After(50 * time.Millisecond):
		}
	}
}

// waitFor redraws until the screen contains want or the deadline passes.
func waitFor(t *testing.T, p Terminal, want string) {
	t.Helper()
	deadline := time.After(5 * time.Second)
	for {
		if strings.Contains(strings.Join(p.Render(), "\n"), want) {
			return
		}
		select {
		case <-p.Output():
		case <-deadline:
			t.Fatalf("screen never showed %q:\n%s", want, strings.Join(p.Render(), "\n"))
		case <-time.After(50 * time.Millisecond):
		}
	}
}

func TestPTYRunsShellAndRendersOutput(t *testing.T) {
	t.Setenv("SHELL", "/bin/sh") // a plain shell keeps the test independent of the user's prompt theme
	p := NewPTY()
	defer p.Close()
	dir := t.TempDir()
	if err := p.Start(80, 24, dir); err != nil {
		t.Skipf("pseudo-terminals are not available here: %v", err)
	}
	if !p.Alive() {
		t.Fatal("shell should be running after Start")
	}
	waitShell(t, p)
	// expr prints 42; the echoed command line does not contain "42".
	if _, err := p.Write([]byte("expr 6 \\* 7\r")); err != nil {
		t.Fatal(err)
	}
	waitFor(t, p, "42")
}

func TestPTYStartsInRequestedDirectory(t *testing.T) {
	t.Setenv("SHELL", "/bin/sh")
	p := NewPTY()
	defer p.Close()
	dir := t.TempDir()
	if err := p.Start(80, 24, dir); err != nil {
		t.Skipf("pseudo-terminals are not available here: %v", err)
	}
	waitShell(t, p)
	if _, err := p.Write([]byte("pwd\r")); err != nil {
		t.Fatal(err)
	}
	waitFor(t, p, filepath.Base(dir))
}

func TestPTYResizeAndClose(t *testing.T) {
	t.Setenv("SHELL", "/bin/sh")
	p := NewPTY()
	if err := p.Start(80, 24, t.TempDir()); err != nil {
		t.Skipf("pseudo-terminals are not available here: %v", err)
	}
	if err := p.Resize(100, 30); err != nil {
		t.Fatalf("Resize: %v", err)
	}
	if h := len(p.Render()); h != 30 {
		t.Fatalf("screen has %d rows after resize, want 30", h)
	}
	if err := p.Close(); err != nil {
		t.Fatal(err)
	}
	if p.Alive() {
		t.Fatal("closed terminal must not report alive")
	}
}

func TestPTYWriteBeforeStartFails(t *testing.T) {
	if _, err := NewPTY().Write([]byte("x")); err == nil {
		t.Fatal("writing to a terminal that was never started must fail")
	}
}

func TestPTYDetectsAppMouseTracking(t *testing.T) {
	t.Setenv("SHELL", "/bin/sh")
	p := NewPTY()
	defer p.Close()
	if err := p.Start(80, 24, t.TempDir()); err != nil {
		t.Skipf("pseudo-terminals are not available here: %v", err)
	}
	waitShell(t, p)
	if _, err := p.Write([]byte("printf '\\033[?1003h'\r")); err != nil {
		t.Fatal(err)
	}
	deadline := time.After(5 * time.Second)
	for !p.Tracking() {
		select {
		case <-p.Output():
		case <-deadline:
			t.Fatal("the emulator never reported mouse tracking")
		case <-time.After(50 * time.Millisecond):
		}
	}
}
