package workspace

import (
	"context"
	"reflect"
	"slices"
	"testing"

	"github.com/tinchoram/groot-cli/internal/catalog"
	"github.com/tinchoram/groot-cli/internal/jobs"
	"github.com/tinchoram/groot-cli/internal/tool"
)

var jobToolNames = []string{"bash_background", "monitor", "job_list", "job_output", "job_stop"}

func TestBuildToolsRegistersTheJobToolsOnTheConversationManager(t *testing.T) {
	w := &Workspace{}
	jm := jobs.NewManager(jobs.DefaultLimit, nil)
	t.Cleanup(jm.StopAll)

	reg, err := w.buildTools(t.TempDir(), jobToolNames, nil, nil, nil, nil, jm, "reader")
	if err != nil {
		t.Fatalf("buildTools: %v", err)
	}
	if got := reg.Names(); !reflect.DeepEqual(got, jobToolNames) {
		t.Fatalf("registered = %v", got)
	}
	for _, name := range jobToolNames {
		if !slices.Contains(catalog.KnownTools, name) {
			t.Fatalf("%s is not a known tool", name)
		}
		tl, _ := reg.Get(name)
		if _, ok := tl.(tool.Destructive); ok != (name == "bash_background" || name == "monitor") {
			t.Fatalf("%s Destructive = %v", name, ok)
		}
	}
}

func TestJobToolsNeedAConversationManager(t *testing.T) {
	w := &Workspace{}
	if _, err := w.buildTools(t.TempDir(), []string{"job_list"}, nil, nil, nil, nil, nil, "reader"); err == nil {
		t.Fatal("job_list without a jobs manager was registered")
	}
}

func TestBashBackgroundStartsOnTheConversationManager(t *testing.T) {
	w := &Workspace{}
	jm := jobs.NewManager(jobs.DefaultLimit, nil)
	t.Cleanup(jm.StopAll)
	dir := t.TempDir()

	reg, err := w.buildTools(dir, []string{"bash_background"}, nil, nil, nil, nil, jm, "reader")
	if err != nil {
		t.Fatalf("buildTools: %v", err)
	}
	tl, _ := reg.Get("bash_background")
	out, isErr := tl.Execute(context.Background(), `{"command":"true"}`)
	if isErr {
		t.Fatalf("bash_background: %s", out)
	}
	if got, _ := jm.Get(1); got.ID != 1 {
		t.Fatalf("job 1 not started on the conversation manager: %s", out)
	}
}
