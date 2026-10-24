package admintool

import (
	"context"
	"strings"
	"testing"

	"github.com/tinchoram/groot-cli/internal/tool"
)

// grantManager records the tool edits made through the admin tools.
type grantManager struct {
	fakeManager
	edits []string
	err   error
}

func (g *grantManager) AgentTools(string) ([]string, error) { return []string{"read_file"}, nil }

func (g *grantManager) EditAgentTools(name string, add, remove []string) error {
	for _, t := range add {
		g.edits = append(g.edits, "+"+t+"@"+name)
	}
	for _, t := range remove {
		g.edits = append(g.edits, "-"+t+"@"+name)
	}
	return g.err
}

func TestGrantToolAddsATool(t *testing.T) {
	g := &grantManager{}
	out, isErr := NewGrantTool(g).Execute(context.Background(), `{"agent":"developer","tool":"bash"}`)
	if isErr || !strings.Contains(out, "granted bash to developer") {
		t.Fatalf("got %q (err=%v)", out, isErr)
	}
	if len(g.edits) != 1 || g.edits[0] != "+bash@developer" {
		t.Fatalf("edits = %v", g.edits)
	}
	if NewGrantTool(g).Risk() != tool.RiskWrite {
		t.Fatal("grant_tool is a write")
	}
}

func TestRevokeToolRemovesATool(t *testing.T) {
	g := &grantManager{}
	out, isErr := NewRevokeTool(g).Execute(context.Background(), `{"agent":"developer","tool":"bash"}`)
	if isErr || !strings.Contains(out, "revoked bash from developer") {
		t.Fatalf("got %q (err=%v)", out, isErr)
	}
	if len(g.edits) != 1 || g.edits[0] != "-bash@developer" {
		t.Fatalf("edits = %v", g.edits)
	}
}

func TestGrantToolReportsRefusals(t *testing.T) {
	g := &grantManager{err: errString("tool is required for groot")}
	out, isErr := NewRevokeTool(g).Execute(context.Background(), `{"agent":"groot","tool":"create_space"}`)
	if !isErr || !strings.Contains(out, "required") {
		t.Fatalf("got %q (err=%v)", out, isErr)
	}
}

func TestGrantToolNeedsBothFields(t *testing.T) {
	g := &grantManager{}
	if _, isErr := NewGrantTool(g).Execute(context.Background(), `{"agent":"developer"}`); !isErr {
		t.Fatal("a grant without a tool is refused")
	}
}

type errString string

func (e errString) Error() string { return string(e) }
