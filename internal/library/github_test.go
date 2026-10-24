package library

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
)

// useGithub points the GitHub clients at a test server for one test.
func useGithub(t *testing.T, srv *httptest.Server) {
	t.Helper()
	oldAPI, oldRaw := githubAPI, githubRaw
	githubAPI, githubRaw = srv.URL, srv.URL+"/raw"
	t.Cleanup(func() { githubAPI, githubRaw = oldAPI, oldRaw })
}

func treeJSON(paths ...string) string {
	var entries []map[string]string
	for _, p := range paths {
		typ := "blob"
		if !strings.Contains(p, ".") {
			typ = "tree"
		}
		entries = append(entries, map[string]string{"path": p, "type": typ})
	}
	b, _ := json.Marshal(map[string]any{"tree": entries, "truncated": false})
	return string(b)
}

func sampleSource(repo string) PublicSource {
	name := "sample-" + strings.ReplaceAll(repo, "/", "-")
	return PublicSource{Name: name, Kind: "github", Repo: repo, Ref: "main", Path: "categories",
		License: "MIT", ValidatedBy: "@tinchoram"}
}

func TestGithubListingFiltersFolderAndCategorises(t *testing.T) {
	repo := "acme/listing"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/repos/acme/listing/git/trees/main" || r.URL.Query().Get("recursive") != "1" {
			http.NotFound(w, r)
			return
		}
		if r.Header.Get("User-Agent") == "" {
			http.Error(w, "missing user agent", http.StatusForbidden)
			return
		}
		fmt.Fprint(w, treeJSON(
			"categories", "categories/devops", "categories/ux",
			"categories/devops/devops-engineer.md", "categories/devops/README.md",
			"categories/ux/ux-tester.md", "categories/notes.txt", "other/outside.md",
		))
	}))
	defer srv.Close()
	useGithub(t, srv)

	entries, err := listGithub(sampleSource(repo))
	if err != nil {
		t.Fatalf("listGithub: %v", err)
	}
	got := map[string]string{}
	for _, e := range entries {
		got[e.Name] = e.Category
		if e.Source != SourcePublic || e.License != "MIT" || e.Author != repo {
			t.Errorf("entry metadata wrong: %+v", e)
		}
	}
	if len(got) != 2 || got["devops-engineer"] != "devops" || got["ux-tester"] != "ux" {
		t.Fatalf("entries = %v", got)
	}
}

func TestGithubTreeIsFetchedOncePerProcess(t *testing.T) {
	repo := "acme/cached"
	var n int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&n, 1)
		fmt.Fprint(w, treeJSON("categories/devops/a.md"))
	}))
	defer srv.Close()
	useGithub(t, srv)
	for i := 0; i < 3; i++ {
		if _, err := listGithub(sampleSource(repo)); err != nil {
			t.Fatal(err)
		}
	}
	if atomic.LoadInt32(&n) != 1 {
		t.Fatalf("tree requested %d times, want 1", n)
	}
}

func TestGithubTreeOversizedOrFailingIsAnError(t *testing.T) {
	big := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, strings.Repeat("x", maxTreeBytes+10))
	}))
	defer big.Close()
	useGithub(t, big)
	if _, err := listGithub(sampleSource("acme/big")); err == nil || !strings.Contains(err.Error(), "1 MB") {
		t.Fatalf("oversized tree must be an error: %v", err)
	}

	fail := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "rate limited", http.StatusForbidden)
	}))
	defer fail.Close()
	useGithub(t, fail)
	if _, err := listGithub(sampleSource("acme/limited")); err == nil {
		t.Fatal("a failing tree request must be an error")
	}
}

func TestGithubFetchDownloadsAndMapsOneAgent(t *testing.T) {
	repo := "acme/fetch"
	md := "---\nname: devops-engineer\ndescription: \"Pipelines and containers\"\ntools: [Read, Bash]\n---\nYou build pipelines.\n"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/repos/acme/fetch/git/trees/main":
			fmt.Fprint(w, treeJSON("categories/devops/devops-engineer.md"))
		case "/raw/acme/fetch/main/categories/devops/devops-engineer.md":
			_, _ = w.Write([]byte(md))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	useGithub(t, srv)

	e, err := fetchPublic(sampleSource(repo), "devops-engineer")
	if err != nil {
		t.Fatalf("fetchPublic: %v", err)
	}
	if e.Name != "devops-engineer" || e.Description != "Pipelines and containers" || e.Prompt != "You build pipelines." {
		t.Fatalf("downloaded agent = %+v", e.AgentDef)
	}
	if strings.Join(e.Tools, ",") != "read_file,bash" {
		t.Fatalf("tools were not mapped: %v", e.Tools)
	}
}

func TestGithubFetchUnknownAgentIsAnError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, treeJSON("categories/devops/a.md"))
	}))
	defer srv.Close()
	useGithub(t, srv)
	if _, err := fetchPublic(sampleSource("acme/unknown"), "nobody"); err == nil {
		t.Fatal("an agent that is not in the listing must be an error")
	}
}
