package ui

import (
	"strings"
	"testing"

	"github.com/tinchoram/groot-cli/internal/flow"
	"github.com/tinchoram/groot-cli/internal/workspace"
)

func TestTasksCommandShowsTheDelegateAndTheParentChain(t *testing.T) {
	m, ws := newTestModel(t, &fakeSession{model: "m"})
	ws.tasks = []workspace.TaskRow{
		{Space: "default", Agent: "reader", ID: 1, Size: flow.SizeMedium, Phase: flow.PhaseImplement,
			Goal: "split billing", Delegate: "developer"},
		{Space: "default", Agent: "developer", ID: 2, Size: flow.SizeMedium, Phase: flow.PhaseDeliver,
			Goal: "split billing", Parent: "default/reader #1"},
	}
	m = sendLine(t, m, "/tasks")
	out := stripANSI(m.transcriptText())
	for _, want := range []string{"reader → developer", "developer ← default/reader #1"} {
		if !strings.Contains(out, want) {
			t.Fatalf("/tasks lacks %q:\n%s", want, out)
		}
	}
}

func TestSpaceRoutesShowsDelegationAndManagerWithTheirOrigins(t *testing.T) {
	m, ws := newTestModel(t, &fakeSession{model: "m"})
	ws.flow = sampleFlow()
	ws.flow.Delegation = workspace.Setting{Value: "auto", From: workspace.SourceGlobal}
	ws.flow.Manager = workspace.Setting{Value: "space-manager", From: workspace.SourceSpace}
	ws.flow.ManagerNote = "manager space-manager lacks list_tasks: add it to its tools to answer status questions"
	m = sendLine(t, m, "/space routes payments")
	out := stripANSI(m.transcriptText())
	for _, want := range []string{
		"delegate auto (global)",
		"manager  space-manager (space)",
		"lacks list_tasks",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("/space routes lacks %q:\n%s", want, out)
		}
	}
}
