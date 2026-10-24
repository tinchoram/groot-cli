package agent

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tinchoram/groot-cli/internal/audit"
	"github.com/tinchoram/groot-cli/internal/gate"
	"github.com/tinchoram/groot-cli/internal/provider"
	"github.com/tinchoram/groot-cli/internal/tool"
)

// sandboxedEcho is a tool that reports the sandbox it ran in.
type sandboxedEcho struct{ *echoTool }

func (sandboxedEcho) SandboxName() string { return "bwrap" }

func TestAuditRecordsTheSandboxOfAToolThatReportsOne(t *testing.T) {
	dir := t.TempDir()
	al, err := audit.Open(filepath.Join(dir, "audit.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	defer al.Close()
	mock := provider.NewMock(
		toolUseResponse("tu_1", "echo", `{"x":1}`),
		textResponse("done"),
	)
	a := newAgent(mock, registry(t, sandboxedEcho{&echoTool{risk: tool.RiskRead, out: "ok"}}), gate.AutoApprove{})
	a.Audit = al
	if _, err := a.Send(context.Background(), "go"); err != nil {
		t.Fatal(err)
	}
	raw, _ := os.ReadFile(filepath.Join(dir, "audit.jsonl"))
	if !strings.Contains(string(raw), `"sandbox":"bwrap"`) {
		t.Fatalf("audit record lacks the sandbox: %s", raw)
	}
}
