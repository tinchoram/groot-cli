package workspace

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tinchoram/groot-cli/internal/catalog"
	"github.com/tinchoram/groot-cli/internal/config"
	"github.com/tinchoram/groot-cli/internal/gate"
	"github.com/tinchoram/groot-cli/internal/provider"
	"github.com/tinchoram/groot-cli/internal/space"
)

// remoteFixture is adminFixture with a remote library source configured.
func remoteFixture(t *testing.T, indexURL string) *Workspace {
	t.Helper()
	home, userHome := t.TempDir(), t.TempDir()
	if _, err := Bootstrap(home, userHome, t0); err != nil {
		t.Fatal(err)
	}
	sp, err := space.Open(filepath.Join(home, "spaces.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	cfg := config.Default()
	cfg.Library.Sources = []config.LibrarySource{{Name: "@ana", URL: indexURL}}
	ws, err := New(Options{
		Home:          home,
		UserHome:      userHome,
		DisablePublic: true,
		Config:        cfg,
		Spaces:        sp,
		Gate:          gate.AutoApprove{},
		NewProvider:   func(string) provider.Provider { return provider.NewMock() },
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ws.Close(context.Background()) })
	return ws
}

func serve(t *testing.T, handler http.HandlerFunc) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	return srv
}

func TestRemoteSourceAppearsInLibrary(t *testing.T) {
	agentSrv := serve(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("---\nname: planner\ndescription: \"Plans work\"\ntools: [Read]\n---\nPlan.\n"))
	})
	idx := serve(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = fmt.Fprintf(w, "collection: \"@ana\"\nauthor: \"@ana\"\nagents:\n  - name: planner\n    description: \"Plans work\"\n    url: %s/planner.md\n", agentSrv.URL)
	})
	ws := remoteFixture(t, idx.URL+"/index.yaml")
	entries, err := ws.LibraryAgents()
	if err != nil {
		t.Fatal(err)
	}
	var got *LibraryInfo
	for i := range entries {
		if entries[i].Name == "planner" {
			got = &entries[i]
		}
	}
	if got == nil || got.Source != "remote" || got.Collection != "@ana" || got.Author != "@ana" || got.URL == "" {
		t.Fatalf("remote agent not listed well: %+v", got)
	}
}

func TestInstallRemoteAgentWritesMappedCatalogFile(t *testing.T) {
	agentSrv := serve(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("---\nname: planner\ndescription: \"Plans work\"\ntools: [Read, Grep]\n---\nPlan.\n"))
	})
	idx := serve(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = fmt.Fprintf(w, "collection: \"@ana\"\nauthor: \"@ana\"\nagents:\n  - name: planner\n    description: \"Plans work\"\n    url: %s/planner.md\n", agentSrv.URL)
	})
	ws := remoteFixture(t, idx.URL+"/index.yaml")
	if err := ws.InstallLibraryAgent("planner", "", false); err != nil {
		t.Fatalf("install remote: %v", err)
	}
	defs, _ := catalog.Load(ws.opts.Home)
	d := findDef(defs, "planner")
	if d == nil || strings.Join(d.Tools, ",") != "read_file" {
		t.Fatalf("installed remote agent = %+v", d)
	}
	if !catalog.Exists(ws.opts.Home, "planner") || !strings.HasSuffix(catalog.Path(ws.opts.Home, "planner"), filepath.Join("agents", "planner.md")) {
		t.Fatal("remote agent not written to the catalog")
	}
}

func TestBrokenRemoteSourceIsAWarningInTheListing(t *testing.T) {
	idx := serve(t, func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "down", http.StatusServiceUnavailable)
	})
	ws := remoteFixture(t, idx.URL+"/index.yaml")
	entries, err := ws.LibraryAgents()
	if err != nil {
		t.Fatalf("a broken source must not fail the listing: %v", err)
	}
	warned := false
	for _, e := range entries {
		if e.Source == "warning" && strings.Contains(e.Description, "@ana") {
			warned = true
		}
	}
	if !warned {
		t.Fatalf("expected a warning for @ana: %+v", entries)
	}
}
