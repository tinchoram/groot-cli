package ui

import (
	"errors"
	"strings"
	"testing"

	"github.com/tinchoram/groot-cli/internal/workspace"
)

// SpaceFlow answers with the fake's flow, naming the space that was asked for.
func (f *fakeWorkspace) SpaceFlow(name string) (workspace.FlowView, error) {
	if f.flowErr != nil {
		return workspace.FlowView{}, f.flowErr
	}
	v := f.flow
	v.Space = name
	return v, nil
}

func sampleFlow() workspace.FlowView {
	return workspace.FlowView{
		Mode:  workspace.Setting{Value: "advisory", From: workspace.SourceSpace},
		Model: workspace.Setting{Value: "claude-haiku-5-5", From: workspace.SourceSpace},
		Routes: []workspace.SizeRoute{
			{Size: "small", Model: workspace.Setting{Value: "claude-haiku-5-5", From: workspace.SourceSpace}, Agent: workspace.Setting{From: workspace.SourceDefault}},
			{Size: "medium", Model: workspace.Setting{Value: "claude-sonnet-5-5", From: workspace.SourceGlobal}, Agent: workspace.Setting{Value: "reviewer", From: workspace.SourceSpace}},
			{Size: "large", Model: workspace.Setting{Value: "claude-opus-5-5", From: workspace.SourceDefault}, Agent: workspace.Setting{Value: "ops", From: workspace.SourceGlobal}},
		},
	}
}

func TestSpaceRoutesShowsEffectiveValuesAndTheirOrigins(t *testing.T) {
	m, ws := newTestModel(t, &fakeSession{model: "m"})
	ws.flow = sampleFlow()
	m = sendLine(t, m, "/space routes payments")
	out := stripANSI(m.transcriptText())
	for _, want := range []string{
		"routes of space payments",
		"mode     advisory (space)",
		"agents   model claude-haiku-5-5 (space)",
		"small    model claude-haiku-5-5 (space) · agent none (default)",
		"medium   model claude-sonnet-5-5 (global) · agent reviewer (space)",
		"large    model claude-opus-5-5 (default) · agent ops (global)",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("/space routes lacks %q:\n%s", want, out)
		}
	}
}

func TestSpaceRoutesDefaultsToTheActiveSpace(t *testing.T) {
	m, ws := newTestModel(t, &fakeSession{model: "m"})
	ws.flow = sampleFlow()
	m = sendLine(t, m, "/space routes")
	active, _ := ws.ActiveSpace()
	if !strings.Contains(stripANSI(m.transcriptText()), "routes of space "+active.Name) {
		t.Fatalf("without a name the active space is shown:\n%s", m.transcriptText())
	}
}

func TestSpaceRoutesReportsErrors(t *testing.T) {
	m, ws := newTestModel(t, &fakeSession{model: "m"})
	ws.flowErr = errors.New(`no space named "missing"`)
	m = sendLine(t, m, "/space routes missing")
	if !strings.Contains(stripANSI(m.transcriptText()), "error: no space named") {
		t.Fatalf("a failed lookup must be reported:\n%s", m.transcriptText())
	}
}
