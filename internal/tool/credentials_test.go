package tool

import (
	"context"
	"slices"
	"testing"

	shellsandbox "github.com/tinchoram/groot-cli/internal/sandbox"
)

func TestBashHandsTheGrantedSecretDirsToTheRunner(t *testing.T) {
	dir := t.TempDir()
	rec := &recordRunner{name: shellsandbox.BackendBwrap}
	b := NewBash(dir)
	b.Runner = rec
	b.Credentials = []string{"kube", "aws"}

	if _, isErr := b.Execute(context.Background(), `{"command":"true"}`); isErr {
		t.Fatal("bash failed")
	}
	if want := []string{".aws", ".kube"}; !slices.Equal(rec.spec.ReadableSecrets, want) {
		t.Fatalf("ReadableSecrets = %q, want %q", rec.spec.ReadableSecrets, want)
	}
	if !slices.Equal(b.CredentialSets(), []string{"kube", "aws"}) {
		t.Fatalf("CredentialSets = %q", b.CredentialSets())
	}
}

func TestBashWithoutCredentialsGrantsNoSecretDir(t *testing.T) {
	rec := &recordRunner{name: shellsandbox.BackendBwrap}
	b := NewBash(t.TempDir())
	b.Runner = rec
	if _, isErr := b.Execute(context.Background(), `{"command":"true"}`); isErr {
		t.Fatal("bash failed")
	}
	if len(rec.spec.ReadableSecrets) != 0 || len(b.CredentialSets()) != 0 {
		t.Fatalf("no sets, but ReadableSecrets = %q", rec.spec.ReadableSecrets)
	}
}

func TestBashCacheDirsJoinTheWritableRoots(t *testing.T) {
	dir := t.TempDir()
	rec := &recordRunner{name: shellsandbox.BackendBwrap}
	b := NewBash(dir)
	b.Runner = rec
	b.ExtraWritable = []string{"/opt/extra"}
	b.CacheDirs = []string{"/home/u/.cache", "/home/u/.npm"}

	if _, isErr := b.Execute(context.Background(), `{"command":"true"}`); isErr {
		t.Fatal("bash failed")
	}
	want := []string{dir, "/opt/extra", "/home/u/.cache", "/home/u/.npm"}
	if !slices.Equal(rec.spec.WritablePaths, want) {
		t.Fatalf("WritablePaths = %q, want %q", rec.spec.WritablePaths, want)
	}
}

// The background tools run the same command with the same sandbox settings.
func TestBashBackgroundAndMonitorCarryTheCredentialSets(t *testing.T) {
	h := newJobHarness(t)
	b := NewBash(t.TempDir())
	b.Credentials = []string{"aws"}
	b.CacheDirs = []string{"/home/u/.npm"}
	rec := &recordRunner{name: shellsandbox.BackendBwrap}
	b.Runner = rec

	bg := NewBashBackground(b, h.m)
	if !slices.Equal(bg.CredentialSets(), []string{"aws"}) {
		t.Fatalf("bash_background CredentialSets = %q", bg.CredentialSets())
	}
	mon := NewMonitor(b, h.m)
	if !slices.Equal(mon.CredentialSets(), []string{"aws"}) {
		t.Fatalf("monitor CredentialSets = %q", mon.CredentialSets())
	}
	if _, isErr := bg.Execute(context.Background(), `{"command":"true"}`); isErr {
		t.Fatal("bash_background failed to start")
	}
	if !slices.Equal(rec.spec.ReadableSecrets, []string{".aws"}) || !slices.Contains(rec.spec.WritablePaths, "/home/u/.npm") {
		t.Fatalf("background spec = %+v, want the set and the cache root", rec.spec)
	}
}

func TestJobOutputsAreNotCredentialed(t *testing.T) {
	h := newJobHarness(t)
	if _, ok := any(NewJobOutput(h.m, "")).(Credentialed); ok {
		t.Fatal("job_output runs no command and must not be credentialed")
	}
	if _, ok := any(NewJobStop(h.m)).(Credentialed); ok {
		t.Fatal("job_stop runs no command and must not be credentialed")
	}
}
