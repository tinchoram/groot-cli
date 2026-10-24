package workspace

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tinchoram/groot-cli/internal/api"
	"github.com/tinchoram/groot-cli/internal/catalog"
	"github.com/tinchoram/groot-cli/internal/provider"
)

const scoutFile = "---\nname: scout\ndescription: finds code\ntools: [Read, Grep]\n---\nYou scout the code.\n"

func writeScout(t *testing.T, body string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "scout.md")
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestImportWritesMappedAgentAndReportsWarnings(t *testing.T) {
	ws, home, _, _ := adminFixture(t)
	res, err := ws.ImportAgent(context.Background(), writeScout(t, scoutFile), false)
	if err != nil {
		t.Fatalf("ImportAgent: %v", err)
	}
	if res.Name != "scout" || res.Path != catalog.Path(home, "scout") {
		t.Fatalf("result = %+v", res)
	}
	defs, _ := catalog.Load(home)
	d := findDef(defs, "scout")
	if d == nil || strings.Join(d.Tools, ",") != "read_file" || d.Prompt != "You scout the code." {
		t.Fatalf("imported agent = %+v", d)
	}
	if len(res.Warnings) != 1 || !strings.Contains(res.Warnings[0], "Grep") {
		t.Fatalf("warnings = %v", res.Warnings)
	}
}

func TestImportRefusesExistingNameUnlessOverwrite(t *testing.T) {
	ws, _, _, _ := adminFixture(t)
	src := writeScout(t, scoutFile)
	if _, err := ws.ImportAgent(context.Background(), src, false); err != nil {
		t.Fatal(err)
	}
	if _, err := ws.ImportAgent(context.Background(), src, false); err == nil || !strings.Contains(err.Error(), "already exists") {
		t.Fatalf("second import without overwrite must refuse: %v", err)
	}
	if _, err := ws.ImportAgent(context.Background(), src, true); err != nil {
		t.Fatalf("overwrite must replace: %v", err)
	}
}

func TestImportNeverOverwritesGroot(t *testing.T) {
	ws, _, _, _ := adminFixture(t)
	src := writeScout(t, "---\nname: groot\ntools: [Read]\n---\nImpostor.\n")
	_, err := ws.ImportAgent(context.Background(), src, true)
	if err == nil || !strings.Contains(err.Error(), "managed") {
		t.Fatalf("groot must be protected even with overwrite: %v", err)
	}
}

func TestImportRejectsUnreadableSource(t *testing.T) {
	ws, _, _, _ := adminFixture(t)
	if _, err := ws.ImportAgent(context.Background(), "/nonexistent/x.md", false); err == nil {
		t.Fatal("unreadable source must fail")
	}
}

func TestGrootImportsThroughAdminTool(t *testing.T) {
	src := writeScout(t, scoutFile)
	in, _ := json.Marshal(map[string]any{"source": src})
	ws, home, _, _ := adminFixture(t,
		provider.Step{Response: api.Response{Content: []api.Block{{Type: api.BlockToolUse, ID: "t1", Name: "import_agent", Input: in}}, StopReason: api.StopToolUse}},
		provider.Step{Response: api.Response{Content: []api.Block{{Type: api.BlockText, Text: "imported"}}, StopReason: api.StopEndTurn}},
	)
	if _, err := ws.Chat().Send(context.Background(), "import the scout"); err != nil {
		t.Fatal(err)
	}
	if !catalog.Exists(home, "scout") {
		t.Fatal("import_agent did not create the agent")
	}
}
