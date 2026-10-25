package ui

import (
	"errors"
	"strings"
	"testing"

	"github.com/tinchoram/groot-cli/internal/workspace"
)

func (f *fakeWorkspace) DeleteSpace(name string) (string, error) {
	f.edits = append(f.edits, "DeleteSpace:"+name)
	return f.editReport, f.editErr
}

func (f *fakeWorkspace) EditSpace(name, newName, path string) (string, error) {
	f.edits = append(f.edits, "EditSpace:"+name+":"+newName+":"+path)
	return f.editReport, f.editErr
}

func (f *fakeWorkspace) DeleteAgent(name string) (string, error) {
	f.edits = append(f.edits, "DeleteAgent:"+name)
	return f.editReport, f.editErr
}

func (f *fakeWorkspace) EditAgent(e workspace.AgentEdit) (string, error) {
	f.edits = append(f.edits, "EditAgent:"+e.Name+":"+e.NewName)
	return f.editReport, f.editErr
}

func TestSpaceDeleteCallsTheWorkspaceAndPrintsTheReport(t *testing.T) {
	m, ws := newTestModel(t, &fakeSession{model: "m"})
	ws.editReport = "deleted space payments from spaces.yaml\nopen tasks: 2"
	m = sendLine(t, m, "/space delete payments")
	if len(ws.edits) != 1 || ws.edits[0] != "DeleteSpace:payments" {
		t.Fatalf("calls = %v", ws.edits)
	}
	out := stripANSI(m.transcriptText())
	if !strings.Contains(out, "deleted space payments") || !strings.Contains(out, "open tasks: 2") {
		t.Fatalf("the report must be printed:\n%s", out)
	}
}

func TestSpaceRenameCallsTheWorkspace(t *testing.T) {
	m, ws := newTestModel(t, &fakeSession{model: "m"})
	ws.editReport = "renamed space payments to billing"
	m = sendLine(t, m, "/space rename payments billing")
	if len(ws.edits) != 1 || ws.edits[0] != "EditSpace:payments:billing:" {
		t.Fatalf("calls = %v", ws.edits)
	}
	if !strings.Contains(stripANSI(m.transcriptText()), "renamed space payments to billing") {
		t.Fatalf("the report must be printed:\n%s", m.transcriptText())
	}
}

func TestSpaceCommandsCheckTheirArguments(t *testing.T) {
	m, ws := newTestModel(t, &fakeSession{model: "m"})
	m = sendLine(t, m, "/space rename payments")
	m = sendLine(t, m, "/space delete")
	if len(ws.edits) != 0 {
		t.Fatalf("incomplete commands must not reach the workspace: %v", ws.edits)
	}
	out := stripANSI(m.transcriptText())
	if !strings.Contains(out, "usage: /space rename <old> <new>") || !strings.Contains(out, "usage: /space delete <name>") {
		t.Fatalf("usage lines missing:\n%s", out)
	}
}

func TestSpaceEditErrorsAreShown(t *testing.T) {
	m, ws := newTestModel(t, &fakeSession{model: "m"})
	ws.editErr = errors.New(`space "root" is the home directory and cannot be deleted`)
	m = sendLine(t, m, "/space delete root")
	if !strings.Contains(stripANSI(m.transcriptText()), "error: space \"root\" is the home directory") {
		t.Fatalf("the refusal must be shown:\n%s", m.transcriptText())
	}
}

func TestAgentDeleteAndRenameCallTheWorkspace(t *testing.T) {
	m, ws := newTestModel(t, &fakeSession{model: "m"})
	ws.editReport = "deleted agent reviewer from the catalog"
	m = sendLine(t, m, "/agent delete reviewer")
	m = sendLine(t, m, "/agent rename reviewer critic")
	if len(ws.edits) != 2 || ws.edits[0] != "DeleteAgent:reviewer" || ws.edits[1] != "EditAgent:reviewer:critic" {
		t.Fatalf("calls = %v", ws.edits)
	}
	if !strings.Contains(stripANSI(m.transcriptText()), "deleted agent reviewer") {
		t.Fatalf("the report must be printed:\n%s", m.transcriptText())
	}
}

func TestAgentCommandsCheckTheirArguments(t *testing.T) {
	m, ws := newTestModel(t, &fakeSession{model: "m"})
	m = sendLine(t, m, "/agent rename reviewer")
	m = sendLine(t, m, "/agent delete")
	if len(ws.edits) != 0 {
		t.Fatalf("incomplete commands must not reach the workspace: %v", ws.edits)
	}
	out := stripANSI(m.transcriptText())
	if !strings.Contains(out, "usage: /agent rename <old> <new>") || !strings.Contains(out, "usage: /agent delete <name>") {
		t.Fatalf("usage lines missing:\n%s", out)
	}
}

func TestEditCommandsAreCompletedAndListed(t *testing.T) {
	m, _ := newTestModel(t, &fakeSession{model: "m"})
	// Sub-command lists show at most six rows, so the edit commands are found by prefix.
	cases := map[string]string{"/space d": "/space delete", "/space r": "/space rename", "/agent d": "/agent delete", "/agent r": "/agent rename"}
	for typed, want := range cases {
		m, _ = newTestModel(t, &fakeSession{model: "m"})
		got := completionInserts(typeText(m, typed))
		if !contains(got, want) {
			t.Fatalf("candidates for %q = %v, missing %s", typed, got, want)
		}
	}
	found := 0
	for _, e := range helpEntries {
		if strings.HasPrefix(e.usage, "/space delete") || strings.HasPrefix(e.usage, "/space rename") ||
			strings.HasPrefix(e.usage, "/agent delete") || strings.HasPrefix(e.usage, "/agent rename") {
			found++
		}
	}
	if found != 4 {
		t.Fatalf("help lists %d of the four edit commands", found)
	}
}
