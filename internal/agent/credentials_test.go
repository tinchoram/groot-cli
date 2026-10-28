package agent

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/tinchoram/groot-cli/internal/audit"
	"github.com/tinchoram/groot-cli/internal/provider"
	"github.com/tinchoram/groot-cli/internal/tool"
)

// credentialedEcho is a tool that runs its commands under credential sets.
type credentialedEcho struct {
	*echoTool
	sets []string
}

func (c credentialedEcho) CredentialSets() []string { return c.sets }

// The gate and the audit see the credential sets of a call that runs under them.
func TestGateAndAuditCarryTheCredentialSetsOfACall(t *testing.T) {
	dir := t.TempDir()
	al, err := audit.Open(filepath.Join(dir, "audit.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	defer al.Close()
	rec := &recordGate{allow: true}
	mock := provider.NewMock(
		toolUseResponse("tu_1", "echo", `{"x":1}`),
		textResponse("done"),
	)
	tl := credentialedEcho{&echoTool{risk: tool.RiskExec, out: "ok"}, []string{"aws"}}
	a := newAgent(mock, registry(t, tl), rec)
	a.Audit = al
	if _, err := a.Send(context.Background(), "go"); err != nil {
		t.Fatal(err)
	}

	if len(rec.reqs) != 1 || !slices.Equal(rec.reqs[0].Credentials, []string{"aws"}) {
		t.Fatalf("gate request credentials = %+v, want [aws]", rec.reqs)
	}
	raw, _ := os.ReadFile(filepath.Join(dir, "audit.jsonl"))
	if !strings.Contains(string(raw), `"credentials":["aws"]`) {
		t.Fatalf("audit record lacks the credential sets: %s", raw)
	}
}

// A call that runs without credential sets carries none, in the gate or the audit.
func TestCallsWithoutCredentialSetsCarryNone(t *testing.T) {
	dir := t.TempDir()
	al, err := audit.Open(filepath.Join(dir, "audit.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	defer al.Close()
	rec := &recordGate{allow: true}
	mock := provider.NewMock(
		toolUseResponse("tu_1", "echo", `{"x":1}`),
		textResponse("done"),
	)
	a := newAgent(mock, registry(t, &echoTool{risk: tool.RiskExec, out: "ok"}), rec)
	a.Audit = al
	if _, err := a.Send(context.Background(), "go"); err != nil {
		t.Fatal(err)
	}
	if len(rec.reqs) != 1 || len(rec.reqs[0].Credentials) != 0 {
		t.Fatalf("gate request credentials = %+v, want none", rec.reqs)
	}
	raw, _ := os.ReadFile(filepath.Join(dir, "audit.jsonl"))
	if strings.Contains(string(raw), "credentials") {
		t.Fatalf("audit record must omit credentials when there are none: %s", raw)
	}
}
