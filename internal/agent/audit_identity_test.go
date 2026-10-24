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

func TestAuditRecordCarriesSpaceAndAgent(t *testing.T) {
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
	a := newAgent(mock, registry(t, &echoTool{risk: tool.RiskRead, out: "ok"}), gate.AutoApprove{})
	a.Name = "reader"
	a.Space = "work"
	a.Audit = al
	if _, err := a.Send(context.Background(), "go"); err != nil {
		t.Fatal(err)
	}
	raw, _ := os.ReadFile(filepath.Join(dir, "audit.jsonl"))
	if !strings.Contains(string(raw), `"space":"work"`) || !strings.Contains(string(raw), `"agent":"reader"`) {
		t.Fatalf("audit record lacks space or agent: %s", raw)
	}
}
