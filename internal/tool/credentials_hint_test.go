package tool

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	shellsandbox "github.com/tinchoram/groot-cli/internal/sandbox"
)

func credentialsHintLine(set, dir string) string {
	return "[sandbox: ~/" + dir + " is not readable by this agent; the user can grant it with credentials: [" + set + "] in the agent file or grant_credentials " + set + "]"
}

// failingCommand prints out to stderr and exits 1, so the bash result is an error.
func failingCommand(t *testing.T, out string) string {
	t.Helper()
	in, err := json.Marshal(map[string]string{"command": "printf '%s\\n' '" + out + "' >&2; exit 1"})
	if err != nil {
		t.Fatal(err)
	}
	return string(in)
}

// cannedFailures are the outputs of the three credential sets, one per set.
var cannedFailures = []struct {
	set, dir, out string
}{
	{"ssh", ".ssh", "git@github.com: Permission denied (publickey).\nfatal: Could not read from remote repository."},
	{"ssh", ".ssh", "Host key verification failed."},
	{"ssh", ".ssh", "open /home/u/.ssh/known_hosts: No such file or directory"},
	{"aws", ".aws", "Unable to locate credentials. You can configure credentials by running aws configure."},
	{"aws", ".aws", "An error occurred (NoCredentialProviders): no valid providers in chain"},
	{"aws", ".aws", "error reading .aws/credentials"},
	{"kube", ".kube", "error: no configuration has been provided, try setting KUBECONFIG"},
	{"kube", ".kube", "stat /home/u/.kube/config: no such file"},
}

func TestCredentialsHintTextIsTheAgreedLine(t *testing.T) {
	want := "[sandbox: ~/.ssh is not readable by this agent; the user can grant it with credentials: [ssh] in the agent file or grant_credentials ssh]"
	if got := credentialsHintLine("ssh", ".ssh"); got != want {
		t.Fatalf("hint = %q", got)
	}
	if got := credentialHintFor("Permission denied (publickey).", []string{"ssh"}); got != want {
		t.Fatalf("credentialHintFor = %q", got)
	}
}

func TestCredentialFailureSignaturesPerSet(t *testing.T) {
	for _, c := range cannedFailures {
		if got := credentialHintFor(c.out, []string{c.set}); got != credentialsHintLine(c.set, c.dir) {
			t.Errorf("credentialHintFor(%q, %s) = %q", c.out, c.set, got)
		}
	}
	others := []string{
		"permission denied",
		"open /home/u/project/missing.txt: No such file or directory",
		"connection refused",
		"",
	}
	for _, out := range others {
		if got := credentialHintFor(out, []string{"ssh", "aws", "kube"}); got != "" {
			t.Errorf("credentialHintFor(%q) = %q, want no hint", out, got)
		}
	}
}

func TestBashAddsTheCredentialsHintPerSet(t *testing.T) {
	for _, c := range cannedFailures {
		b := NewBash(t.TempDir())
		b.Runner = &recordRunner{name: shellsandbox.BackendBwrap}
		out, isErr := b.Execute(context.Background(), failingCommand(t, c.out))
		if !isErr || !strings.HasSuffix(strings.TrimSpace(out), credentialsHintLine(c.set, c.dir)) {
			t.Errorf("%s: out = %q (error %v), want the %s hint", c.out, out, isErr, c.set)
		}
		if strings.Count(out, "[sandbox:") != 1 {
			t.Errorf("%s: more than one sandbox line in %q", c.out, out)
		}
	}
}

func TestBashCredentialsHintIsNotAddedWhenTheSetIsGranted(t *testing.T) {
	for _, c := range cannedFailures {
		b := NewBash(t.TempDir())
		b.Runner = &recordRunner{name: shellsandbox.BackendBwrap}
		b.Credentials = []string{c.set}
		out, _ := b.Execute(context.Background(), failingCommand(t, c.out))
		if strings.Contains(out, "[sandbox:") {
			t.Errorf("granted %s: out = %q, want no hint", c.set, out)
		}
	}
}

func TestBashCredentialsHintNeedsAnEnforcingSandbox(t *testing.T) {
	cases := map[string]shellsandbox.Runner{
		"no runner":   nil,
		"no sandbox":  &recordRunner{name: shellsandbox.BackendNone},
		"sandbox off": &recordRunner{name: shellsandbox.BackendOff},
	}
	for name, r := range cases {
		t.Run(name, func(t *testing.T) {
			b := NewBash(t.TempDir())
			b.Runner = r
			out, isErr := b.Execute(context.Background(), failingCommand(t, "Permission denied (publickey)."))
			if !isErr || strings.Contains(out, "grant_credentials") {
				t.Fatalf("without an enforcing sandbox the hint would mislead: isErr=%v out=%q", isErr, out)
			}
		})
	}
}

func TestBashCredentialsHintIsNotAddedToASuccess(t *testing.T) {
	b := NewBash(t.TempDir())
	b.Runner = &recordRunner{name: shellsandbox.BackendBwrap}
	out, isErr := b.Execute(context.Background(), `{"command":"echo 'Permission denied (publickey).'"}`)
	if isErr || strings.Contains(out, "grant_credentials") {
		t.Fatalf("a successful command must not get the hint: isErr=%v out=%q", isErr, out)
	}
}

func TestBashNetworkHintWinsOverTheCredentialsHint(t *testing.T) {
	b := NewBash(t.TempDir())
	b.Runner = &recordRunner{name: shellsandbox.BackendBwrap}
	b.Network = false
	out, _ := b.Execute(context.Background(), failingCommand(t, "Could not resolve host: github.com\nPermission denied (publickey)."))
	if !strings.HasSuffix(strings.TrimSpace(out), networkHint) || strings.Contains(out, "grant_credentials") {
		t.Fatalf("out = %q, want only the network hint", out)
	}
}

// A background job that failed on a denied credential set says so in job_output.
func TestJobOutputAddsTheCredentialsHintForAFailedJob(t *testing.T) {
	h := newJobHarness(t)
	b := NewBash(t.TempDir())
	b.Runner = &recordRunner{name: shellsandbox.BackendBwrap}
	bg := NewBashBackground(b, h.m)
	if _, isErr := bg.Execute(context.Background(), failingCommand(t, "Unable to locate credentials.")); isErr {
		t.Fatal("start failed")
	}
	h.untilExit(t, 1)

	out, isErr := NewJobOutput(h.m, "").Execute(context.Background(), `{"id":1}`)
	if isErr || !strings.HasSuffix(strings.TrimSpace(out), credentialsHintLine("aws", ".aws")) {
		t.Fatalf("job_output = %q (error %v), want the aws hint", out, isErr)
	}
}

func TestJobOutputHasNoCredentialsHintWhenTheSetIsGranted(t *testing.T) {
	h := newJobHarness(t)
	b := NewBash(t.TempDir())
	b.Runner = &recordRunner{name: shellsandbox.BackendBwrap}
	b.Credentials = []string{"aws"}
	bg := NewBashBackground(b, h.m)
	if _, isErr := bg.Execute(context.Background(), failingCommand(t, "Unable to locate credentials.")); isErr {
		t.Fatal("start failed")
	}
	h.untilExit(t, 1)

	out, _ := NewJobOutput(h.m, "").Execute(context.Background(), `{"id":1}`)
	if strings.Contains(out, "[sandbox:") {
		t.Fatalf("job_output = %q, the set was granted", out)
	}
}
