package ui

import (
	"strings"
	"testing"

	"github.com/tinchoram/groot-cli/internal/workspace"
)

// An empty route model is shown as the agent model, not as a model name.
func TestSpaceRoutesBlockNamesTheAgentModelWhenRouteIsEmpty(t *testing.T) {
	v := workspace.FlowView{
		Space: "payments",
		Mode:  workspace.Setting{Value: "strict", From: workspace.SourceDefault},
		Routes: []workspace.SizeRoute{
			{Size: "small", Model: workspace.Setting{From: workspace.SourceDefault}, Agent: workspace.Setting{From: workspace.SourceDefault}},
			{Size: "medium", Model: workspace.Setting{Value: "claude-haiku-5-5", From: workspace.SourceSpace}, Agent: workspace.Setting{From: workspace.SourceDefault}},
		},
	}
	out := spaceRoutesBlock(v)
	if !strings.Contains(out, "small    model agent model (default)") {
		t.Fatalf("empty route must say agent model:\n%s", out)
	}
	if !strings.Contains(out, "medium   model claude-haiku-5-5 (space)") {
		t.Fatalf("explicit route must show its model:\n%s", out)
	}
}
