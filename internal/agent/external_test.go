package agent

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/tinchoram/groot-cli/internal/api"
	"github.com/tinchoram/groot-cli/internal/provider"
	"github.com/tinchoram/groot-cli/internal/tool"
)

// notExternal answers the External interface with false, as a wrapper of a
// local tool does. Its presence must not make the call external.
type notExternal struct{ *echoTool }

func (notExternal) External() bool { return false }

func TestExternalIsReadFromTheAnswerNotFromThePresenceOfTheMethod(t *testing.T) {
	rec := &recordGate{allow: false}
	a := newAgent(provider.NewMock(), registry(t, notExternal{&echoTool{risk: tool.RiskRead, out: "ok"}}), rec)
	a.runTool(context.Background(), api.Block{Type: api.BlockToolUse, ID: "1", Name: "echo", Input: json.RawMessage(`{"x":1}`)})
	if len(rec.reqs) != 1 || rec.reqs[0].External {
		t.Fatalf("a tool that answers External()=false must not be external: %+v", rec.reqs)
	}
}
