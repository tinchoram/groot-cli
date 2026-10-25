package workspace

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tinchoram/groot-cli/internal/flow"
)

func TestPlanPathStaysInsideTheSpaceAndIsARegularFile(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "docs", "tasks"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "docs", "tasks", "1.md"), []byte("# plan"), 0o644); err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name string
		rel  string
		ok   bool
	}{
		{"a plan file of the space", "docs/tasks/1.md", true},
		{"a directory is not a plan", "docs/tasks", false},
		{"a missing file is not a plan", "docs/tasks/2.md", false},
		{"a parent path leaves the space", "../1.md", false},
		{"a path that cleans to a parent leaves the space", "docs/../../1.md", false},
		{"an absolute path is refused", filepath.Join(root, "docs", "tasks", "1.md"), false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := planPath(root, tt.rel)
			if (err == nil) != tt.ok {
				t.Fatalf("planPath(%q) err = %v, want ok %v", tt.rel, err, tt.ok)
			}
		})
	}
}

func TestReviewShowsTheSpecVerdictsOfALargeTask(t *testing.T) {
	tk := flow.Task{
		ID: 4, Size: flow.SizeLarge, Agent: "developer", Phase: flow.PhaseDeliver,
		Specs: []flow.SpecState{
			{Spec: flow.Spec{N: 1, Text: "split the module"}, Status: flow.VerdictPass},
			{Spec: flow.Spec{N: 2, Text: "keep the totals"}},
		},
	}
	out := reviewText(context.Background(), tk, t.TempDir(), []string{"billing/x.go"})
	for _, want := range []string{"Specs:", "S1 pass — split the module", "S2 pending — keep the totals"} {
		if !strings.Contains(out, want) {
			t.Fatalf("review misses %q:\n%s", want, out)
		}
	}
}
