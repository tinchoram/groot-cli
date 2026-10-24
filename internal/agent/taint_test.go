package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"

	"github.com/tinchoram/groot-cli/internal/api"
	"github.com/tinchoram/groot-cli/internal/guard"
	"github.com/tinchoram/groot-cli/internal/provider"
)

func TestGateReceivesTheTaintFlag(t *testing.T) {
	g := guard.New(guard.Warn)
	id := idRE.FindStringSubmatch(g.Wrap("fetch_url x", "page"))[1]
	rec := &recordGate{allow: true}
	a := newAgent(provider.NewMock(), registry(t, readOnlyBash{}), rec)
	a.Guard = g

	tainted := api.Block{Type: api.BlockToolUse, ID: "1", Name: "bash", Input: json.RawMessage(fmt.Sprintf(`{"command":"echo %s"}`, id))}
	clean := api.Block{Type: api.BlockToolUse, ID: "2", Name: "bash", Input: json.RawMessage(`{"command":"echo hi"}`)}
	a.runTool(context.Background(), tainted)
	a.runTool(context.Background(), clean)

	if len(rec.reqs) != 2 || !rec.reqs[0].Tainted || rec.reqs[1].Tainted {
		t.Fatalf("the gate must see Tainted only for the call carrying the id: %+v", rec.reqs)
	}
}
