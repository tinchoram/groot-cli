package workspace

import (
	"reflect"
	"slices"
	"testing"

	"github.com/tinchoram/groot-cli/internal/catalog"
	"github.com/tinchoram/groot-cli/internal/tool"
)

func TestBuildToolsRegistersTheReadOnlyListing(t *testing.T) {
	w := &Workspace{}
	reg, err := w.buildTools(t.TempDir(), []string{"list_dir", "glob"}, nil, nil, nil, nil, "reader")
	if err != nil {
		t.Fatalf("buildTools: %v", err)
	}
	if got := reg.Names(); !reflect.DeepEqual(got, []string{"list_dir", "glob"}) {
		t.Fatalf("registered = %v", got)
	}
	for _, name := range []string{"list_dir", "glob"} {
		tl, _ := reg.Get(name)
		if tl.Risk() != tool.RiskRead {
			t.Fatalf("%s must be RiskRead", name)
		}
	}
}

func TestListingToolsAreKnownAndInTheBaseSet(t *testing.T) {
	for _, name := range []string{"list_dir", "glob"} {
		if !slices.Contains(catalog.KnownTools, name) {
			t.Fatalf("%s is not a known tool", name)
		}
		if !slices.Contains(catalog.ManagedTools(), name) {
			t.Fatalf("%s is not in the base set that upgrade_agent adds", name)
		}
	}
}
