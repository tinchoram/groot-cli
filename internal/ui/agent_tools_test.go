package ui

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/tinchoram/groot-cli/internal/workspace"
)

func (f *fakeWorkspace) AgentTools(name string) ([]string, error) {
	return f.agentTools[name], nil
}

func (f *fakeWorkspace) EditAgentTools(name string, add, remove []string) error {
	for _, t := range add {
		if t == "teleport" {
			return errors.New("unknown tool \"teleport\"")
		}
	}
	f.toolEdits = append(f.toolEdits, fmt.Sprintf("%s +%s -%s", name, strings.Join(add, ","), strings.Join(remove, ",")))
	return nil
}

func TestAgentToolsAddTakesCommasAndSpaces(t *testing.T) {
	m, ws := newTestModel(t, &fakeSession{model: "m"})
	m = sendLine(t, m, "/agent tools developer add bash,kubectl recall")
	if len(ws.toolEdits) != 1 || ws.toolEdits[0] != "developer +bash,kubectl,recall -" {
		t.Fatalf("edits = %v", ws.toolEdits)
	}
}

func TestAgentToolsRemoveDropsTools(t *testing.T) {
	m, ws := newTestModel(t, &fakeSession{model: "m"})
	m = sendLine(t, m, "/agent tools developer remove bash")
	if len(ws.toolEdits) != 1 || ws.toolEdits[0] != "developer + -bash" {
		t.Fatalf("edits = %v", ws.toolEdits)
	}
}

func TestAgentToolsReportsErrorsAndUsage(t *testing.T) {
	m, ws := newTestModel(t, &fakeSession{model: "m"})
	m = sendLine(t, m, "/agent tools developer add teleport")
	if !strings.Contains(stripANSI(m.transcriptText()), "unknown tool") {
		t.Fatalf("an unknown tool must be reported:\n%s", m.transcriptText())
	}
	m = sendLine(t, m, "/agent tools developer add")
	if !strings.Contains(stripANSI(m.transcriptText()), "usage: /agent tools") {
		t.Fatalf("usage missing:\n%s", m.transcriptText())
	}
	if len(ws.toolEdits) != 0 {
		t.Fatalf("nothing may be saved on a refusal or usage error: %v", ws.toolEdits)
	}
}

func TestAgentShowListsTools(t *testing.T) {
	m, ws := newTestModel(t, &fakeSession{model: "m"})
	ws.agentTools = map[string][]string{"developer": {"read_file", "bash"}}
	ws.catalog = append(ws.catalog, workspace.AgentInfo{Name: "developer"})
	m = sendLine(t, m, "/agent show developer")
	if !strings.Contains(stripANSI(m.transcriptText()), "tools: read_file, bash") {
		t.Fatalf("show must list the tools:\n%s", m.transcriptText())
	}
}
