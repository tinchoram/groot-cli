package workspace

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tinchoram/groot-cli/internal/catalog"
	"github.com/tinchoram/groot-cli/internal/space"
)

func TestLibraryListsBuiltinAgents(t *testing.T) {
	ws, _, _, _ := adminFixture(t)
	entries, err := ws.LibraryAgents()
	if err != nil {
		t.Fatalf("LibraryAgents: %v", err)
	}
	names := map[string]bool{}
	for _, e := range entries {
		names[e.Name] = true
	}
	for _, want := range []string{"developer", "devsecops-platform", "observability", "project-manager", "security-tester", "ux-designer"} {
		if !names[want] {
			t.Errorf("library lacks %s", want)
		}
	}
}

func TestInstallCopiesAgentIntoCatalog(t *testing.T) {
	ws, home, _, _ := adminFixture(t)
	if err := ws.InstallLibraryAgent("developer", "", false); err != nil {
		t.Fatalf("InstallLibraryAgent: %v", err)
	}
	if !catalog.Exists(home, "developer") {
		t.Fatal("the agent was not copied into the catalog")
	}
	defs, _ := catalog.Load(home)
	d := findDef(defs, "developer")
	if d == nil || d.Prompt == "" || d.Model != "" {
		t.Fatalf("installed agent = %+v", d)
	}
	for _, a := range ws.Agents() {
		if a.Name == "developer" {
			t.Fatal("install without a space must not assign the agent")
		}
	}
}

func TestInstallAssignsToSpace(t *testing.T) {
	ws, home, _, _ := adminFixture(t)
	if err := ws.InstallLibraryAgent("observability", "root", false); err != nil {
		t.Fatal(err)
	}
	sp, _ := space.Open(filepath.Join(home, "spaces.yaml"))
	root, _ := sp.Get("root")
	if !contains(root.Agents, "observability") {
		t.Fatalf("root agents = %v, want observability", root.Agents)
	}
}

func TestInstallRefusesExistingWithoutOverwrite(t *testing.T) {
	ws, _, _, _ := adminFixture(t)
	if err := ws.InstallLibraryAgent("developer", "", false); err != nil {
		t.Fatal(err)
	}
	if err := ws.InstallLibraryAgent("developer", "", false); err == nil || !strings.Contains(err.Error(), "already exists") {
		t.Fatalf("second install must refuse: %v", err)
	}
	if err := ws.InstallLibraryAgent("developer", "", true); err != nil {
		t.Fatalf("overwrite must replace: %v", err)
	}
}

func TestInstallNeverTouchesGroot(t *testing.T) {
	ws, _, _, _ := adminFixture(t)
	if err := ws.InstallLibraryAgent("groot", "", true); err == nil || !strings.Contains(err.Error(), "managed") {
		t.Fatalf("groot must be refused even with overwrite: %v", err)
	}
}

func TestInstallUnknownSpaceWritesNothing(t *testing.T) {
	ws, home, _, _ := adminFixture(t)
	if err := ws.InstallLibraryAgent("developer", "nowhere", false); err == nil {
		t.Fatal("an unknown space must be refused")
	}
	if catalog.Exists(home, "developer") {
		t.Fatal("a refused install must not leave the agent behind")
	}
}

func TestAdminManagerInstallsThroughTheSamePath(t *testing.T) {
	ws, home, _, _ := adminFixture(t)
	if err := ws.Manager().InstallAgent("ux-designer", "", false); err != nil {
		t.Fatalf("manager install: %v", err)
	}
	if !catalog.Exists(home, "ux-designer") {
		t.Fatal("manager install did not write the agent")
	}
}

func TestInstallKeepsAuthorAndCollection(t *testing.T) {
	ws, home, _, _ := adminFixture(t)
	if err := ws.InstallLibraryAgent("developer", "", false); err != nil {
		t.Fatalf("InstallLibraryAgent: %v", err)
	}
	raw, err := os.ReadFile(catalog.Path(home, "developer"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), "author: '@tinchoram'") && !strings.Contains(string(raw), `author: "@tinchoram"`) {
		t.Fatalf("installed file lost the author:\n%s", raw)
	}
	if !strings.Contains(string(raw), "collection:") {
		t.Fatalf("installed file lost the collection:\n%s", raw)
	}
}
