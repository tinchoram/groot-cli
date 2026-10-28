package tool

import (
	"context"
	"strings"
	"testing"

	shellsandbox "github.com/tinchoram/groot-cli/internal/sandbox"
)

func TestNetworkHintTextIsTheAgreedLine(t *testing.T) {
	want := "[sandbox: network is off for this space; the user can turn it on with /space network on]"
	if networkHint != want {
		t.Fatalf("networkHint = %q", networkHint)
	}
}

func TestNetworkFailureSignatures(t *testing.T) {
	failures := []string{
		"curl: (6) Could not resolve host: example.com",
		"fatal: unable to access 'https://github.com/x/y': Could not resolve host: github.com",
		"Temporary failure in name resolution",
		"connect: Network is unreachable",
		"ping: nodename nor servname provided, or not known",
		"connect: no route to host",
		"dial tcp 140.82.112.3:443: connect: operation not permitted",
		"DIAL TCP 10.0.0.1:443: CONNECT: OPERATION NOT PERMITTED",
	}
	for _, out := range failures {
		if !networkFailure(out) {
			t.Errorf("networkFailure(%q) = false, want true", out)
		}
	}
	others := []string{
		"permission denied",
		"no such file or directory",
		"dial tcp 10.0.0.1:443: i/o timeout",
		"connection refused",
		"exit status 1",
		"",
	}
	for _, out := range others {
		if networkFailure(out) {
			t.Errorf("networkFailure(%q) = true, want false", out)
		}
	}
}

// The hint is for a failed command that could not reach the network because
// the space turned it off.
func TestBashAddsTheNetworkHintWhenNetworkIsOff(t *testing.T) {
	b := NewBash(t.TempDir())
	b.Runner = &recordRunner{name: shellsandbox.BackendBwrap}
	b.Network = false

	out, isErr := b.Execute(context.Background(), `{"command":"echo 'Could not resolve host: example.com' >&2; exit 6"}`)
	if !isErr {
		t.Fatalf("a failed command must be an error, got %q", out)
	}
	if !strings.HasSuffix(strings.TrimSpace(out), networkHint) {
		t.Fatalf("output must end with the network hint, got %q", out)
	}
}

func TestBashNetworkHintIsNeverAddedWhenNetworkIsOn(t *testing.T) {
	b := NewBash(t.TempDir())
	b.Runner = &recordRunner{name: shellsandbox.BackendBwrap}
	b.Network = true

	out, isErr := b.Execute(context.Background(), `{"command":"echo 'Could not resolve host: example.com' >&2; exit 6"}`)
	if !isErr || strings.Contains(out, "network is off") {
		t.Fatalf("network on must not get the hint: isErr=%v out=%q", isErr, out)
	}
}

func TestBashNetworkHintIsNotAddedToASuccess(t *testing.T) {
	b := NewBash(t.TempDir())
	b.Runner = &recordRunner{name: shellsandbox.BackendBwrap}
	b.Network = false

	out, isErr := b.Execute(context.Background(), `{"command":"echo 'Could not resolve host: example.com'"}`)
	if isErr || strings.Contains(out, "network is off") {
		t.Fatalf("a successful command must not get the hint: isErr=%v out=%q", isErr, out)
	}
}

func TestBashNetworkHintNeedsAnEnforcingSandbox(t *testing.T) {
	const failing = `{"command":"echo 'Could not resolve host: example.com' >&2; exit 6"}`
	cases := map[string]shellsandbox.Runner{
		"no runner":   nil,
		"no sandbox":  &recordRunner{name: shellsandbox.BackendNone},
		"sandbox off": &recordRunner{name: shellsandbox.BackendOff},
	}
	for name, r := range cases {
		t.Run(name, func(t *testing.T) {
			b := NewBash(t.TempDir())
			b.Runner = r
			b.Network = false
			out, isErr := b.Execute(context.Background(), failing)
			if !isErr || strings.Contains(out, "network is off") {
				t.Fatalf("without a network-enforcing sandbox the hint would mislead: isErr=%v out=%q", isErr, out)
			}
		})
	}
}

// A background job that failed for lack of network says so in job_output, the
// result that reports its exit.
func TestJobOutputAddsTheNetworkHintForAFailedJob(t *testing.T) {
	h := newJobHarness(t)
	b := NewBash(t.TempDir())
	b.Runner = &recordRunner{name: shellsandbox.BackendBwrap}
	b.Network = false
	bg := NewBashBackground(b, h.m)
	if _, isErr := bg.Execute(context.Background(), `{"command":"echo 'Could not resolve host: example.com' >&2; exit 6"}`); isErr {
		t.Fatal("start failed")
	}
	h.untilExit(t, 1)

	out, isErr := NewJobOutput(h.m, "").Execute(context.Background(), `{"id":1}`)
	if isErr || !strings.HasSuffix(strings.TrimSpace(out), networkHint) {
		t.Fatalf("job_output = %q (error %v), want the network hint", out, isErr)
	}
}

func TestJobOutputHasNoHintWhenTheJobHadNetwork(t *testing.T) {
	h := newJobHarness(t)
	b := NewBash(t.TempDir())
	b.Runner = &recordRunner{name: shellsandbox.BackendBwrap}
	b.Network = true
	bg := NewBashBackground(b, h.m)
	if _, isErr := bg.Execute(context.Background(), `{"command":"echo 'Could not resolve host: example.com' >&2; exit 6"}`); isErr {
		t.Fatal("start failed")
	}
	h.untilExit(t, 1)

	out, _ := NewJobOutput(h.m, "").Execute(context.Background(), `{"id":1}`)
	if strings.Contains(out, "network is off") {
		t.Fatalf("job_output = %q, the job had network on", out)
	}
}

func TestBashNetworkHintNamesTheOffSettingOnlyForNetworkErrors(t *testing.T) {
	b := NewBash(t.TempDir())
	b.Runner = &recordRunner{name: shellsandbox.BackendBwrap}
	b.Network = false
	out, _ := b.Execute(context.Background(), `{"command":"echo 'permission denied' >&2; exit 1"}`)
	if strings.Contains(out, "network is off") {
		t.Fatalf("an unrelated failure must not get the hint: %q", out)
	}
}
