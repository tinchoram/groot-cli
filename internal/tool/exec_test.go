package tool

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestBashRunsCommand(t *testing.T) {
	b := NewBash("")
	if b.Risk() != RiskExec {
		t.Fatal("bash must be RiskExec")
	}
	if got := b.Preview(mustJSON(t, map[string]string{"command": "kubectl get pods"})); got != "kubectl get pods" {
		t.Fatalf("preview should be the command, got %q", got)
	}
	out, isErr := b.Execute(context.Background(), mustJSON(t, map[string]string{"command": "echo hi"}))
	if isErr || strings.TrimSpace(out) != "hi" {
		t.Fatalf("got (%q, %v)", out, isErr)
	}
}

func TestBashNonZeroExitIsError(t *testing.T) {
	out, isErr := NewBash("").Execute(context.Background(), mustJSON(t, map[string]string{"command": "echo boom; exit 3"}))
	if !isErr || !strings.Contains(out, "boom") || !strings.Contains(out, "exit") {
		t.Fatalf("got (%q, %v)", out, isErr)
	}
}

func TestBashTimeout(t *testing.T) {
	b := NewBash("")
	b.Timeout = 200 * time.Millisecond
	start := time.Now()
	out, isErr := b.Execute(context.Background(), mustJSON(t, map[string]string{"command": "sleep 5"}))
	if !isErr || !strings.Contains(out, "timed out") {
		t.Fatalf("expected timeout, got (%q, %v)", out, isErr)
	}
	if time.Since(start) > 3*time.Second {
		t.Fatal("timeout did not stop the process promptly")
	}
}

func TestBashSpillsLongOutputAndKeepsTheFullText(t *testing.T) {
	space := t.TempDir()
	out, isErr := NewBash(space).Execute(context.Background(), mustJSON(t, map[string]string{"command": "yes x | head -c 100000; printf END"}))
	if isErr {
		t.Fatalf("unexpected error: %.200q", out)
	}
	if !strings.Contains(out, "[output: 100003 characters total;") || !strings.HasSuffix(out, "END") {
		t.Fatalf("long output must spill with the note and keep the end: tail %q", out[len(out)-60:])
	}
	saved, err := os.ReadFile(spilledPath(t, out))
	if err != nil {
		t.Fatal(err)
	}
	if len(saved) != 100003 {
		t.Fatalf("saved %d bytes, want the full 100003", len(saved))
	}
}

func TestBashExitLineSurvivesSpill(t *testing.T) {
	out, isErr := NewBash(t.TempDir()).Execute(context.Background(), mustJSON(t, map[string]string{"command": "yes x | head -c 60000; exit 4"}))
	if !isErr || !strings.HasSuffix(out, "[exit 4]") {
		t.Fatalf("exit line must stay at the end: isErr=%v tail %q", isErr, out[len(out)-40:])
	}
}

func TestKubectlSpillsLongOutputIntoItsSpace(t *testing.T) {
	space := t.TempDir()
	k := NewKubectl(fakeBinary(t, `head -c 40000 /dev/zero | tr '\0' y`), nil)
	k.OutDir = space
	out, isErr := k.Execute(context.Background(), mustJSON(t, map[string]any{"args": []string{"get", "pods"}}))
	if isErr || !strings.Contains(out, "[output: 40000 characters total;") {
		t.Fatalf("kubectl must use the same spill: isErr=%v head %.80q", isErr, out)
	}
	if !strings.HasPrefix(spilledPath(t, out), outputsDirOf(space)+string(filepath.Separator)+"kubectl-") {
		t.Fatalf("kubectl output saved outside the space: %q", spilledPath(t, out))
	}
}

func TestKubectlValidateArgs(t *testing.T) {
	cases := []struct {
		name    string
		args    []string
		allowNS []string
		wantErr string // substring; empty means allowed
	}{
		{"get pods any namespace when no allowlist", []string{"get", "pods", "-A"}, nil, ""},
		{"logs with namespace in allowlist", []string{"logs", "api-0", "-n", "prod"}, []string{"prod"}, ""},
		{"namespace long form", []string{"get", "pods", "--namespace=prod"}, []string{"prod"}, ""},
		{"namespace concatenated short form", []string{"get", "pods", "-nprod"}, []string{"prod"}, ""},
		{"version needs no namespace", []string{"version"}, []string{"prod"}, ""},
		{"verb not allowed", []string{"delete", "pod", "x"}, nil, "not allowed"},
		{"verb must come first", []string{"-n", "prod", "get", "pods"}, nil, "not allowed"},
		{"empty args", []string{}, nil, "empty"},
		{"kubeconfig override", []string{"get", "pods", "--kubeconfig=/tmp/x"}, nil, "--kubeconfig"},
		{"token override", []string{"get", "pods", "--token", "abc"}, nil, "--token"},
		{"as impersonation", []string{"get", "pods", "--as=admin"}, nil, "--as"},
		{"server override", []string{"get", "pods", "--server=https://evil"}, nil, "--server"},
		{"namespace outside allowlist", []string{"get", "pods", "-n", "kube-system"}, []string{"prod"}, "kube-system"},
		{"namespace missing with allowlist", []string{"get", "pods"}, []string{"prod"}, "namespace"},
		{"all namespaces with allowlist", []string{"get", "pods", "-A"}, []string{"prod"}, "all-namespaces"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			err := ValidateKubectlArgs(c.args, c.allowNS)
			if c.wantErr == "" {
				if err != nil {
					t.Fatalf("expected allowed, got %v", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), c.wantErr) {
				t.Fatalf("expected error containing %q, got %v", c.wantErr, err)
			}
		})
	}
}

func fakeBinary(t *testing.T, script string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "kubectl")
	if err := os.WriteFile(p, []byte("#!/bin/sh\n"+script+"\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestKubectlExecutesBinaryWithArgs(t *testing.T) {
	k := NewKubectl(fakeBinary(t, `echo "argv: $*"`), []string{"prod"})
	if k.Risk() != RiskRead {
		t.Fatal("kubectl must be RiskRead")
	}
	in := mustJSON(t, map[string]any{"args": []string{"get", "pods", "-n", "prod"}})
	if got := k.Preview(in); got != "kubectl get pods -n prod" {
		t.Fatalf("preview: %q", got)
	}
	out, isErr := k.Execute(context.Background(), in)
	if isErr || strings.TrimSpace(out) != "argv: get pods -n prod" {
		t.Fatalf("got (%q, %v)", out, isErr)
	}
}

func TestKubectlRefusesDeniedArgsBeforeRunning(t *testing.T) {
	k := NewKubectl(fakeBinary(t, `echo SHOULD-NOT-RUN`), nil)
	out, isErr := k.Execute(context.Background(), mustJSON(t, map[string]any{"args": []string{"delete", "ns", "prod"}}))
	if !isErr || strings.Contains(out, "SHOULD-NOT-RUN") {
		t.Fatalf("denied verb must not execute: (%q, %v)", out, isErr)
	}
}

func TestKubectlMissingBinaryIsClearError(t *testing.T) {
	k := NewKubectl("groot-definitely-missing-kubectl", nil)
	out, isErr := k.Execute(context.Background(), mustJSON(t, map[string]any{"args": []string{"version"}}))
	if !isErr || !strings.Contains(out, "not found on PATH") {
		t.Fatalf("got (%q, %v)", out, isErr)
	}
}
