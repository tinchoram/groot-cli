package workspace

import (
	"slices"
	"strings"
	"testing"

	"github.com/tinchoram/groot-cli/internal/catalog"
)

// editTools are the six tools that delete, rename, move and copy spaces and agents.
var editTools = []string{"delete_space", "edit_space", "delete_agent", "edit_agent", "move_agent", "copy_agent"}

func TestGrootHasTheEditToolsAndKnowsThem(t *testing.T) {
	groot := grootDef().Tools
	for _, name := range editTools {
		if !slices.Contains(groot, name) {
			t.Errorf("groot lacks %s", name)
		}
		if !slices.Contains(catalog.KnownTools, name) {
			t.Errorf("%s is not a known tool", name)
		}
		if !slices.Contains(requiredTools(managedAgent), name) {
			t.Errorf("%s is not kept on groot", name)
		}
	}
}

func TestGrootPromptDescribesDeletesAndRenames(t *testing.T) {
	for _, want := range []string{"delete, rename, move and copy", "confirm: true", "unless purge_data is set", "with_memory"} {
		if !strings.Contains(grootPrompt, want) {
			t.Errorf("groot prompt lacks %q", want)
		}
	}
}

func TestEditToolsAreBuiltForAnAgent(t *testing.T) {
	f := newFixture(t)
	f.ws.mu.Lock()
	reg, err := f.ws.buildTools(f.dir, editTools, nil, nil, nil, &fileLog{}, nil, managedAgent)
	f.ws.mu.Unlock()
	if err != nil {
		t.Fatalf("buildTools: %v", err)
	}
	for _, name := range editTools {
		if _, ok := reg.Get(name); !ok {
			t.Errorf("the registry lacks %s", name)
		}
	}
}
