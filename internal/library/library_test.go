package library

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync/atomic"
	"testing"

	"gopkg.in/yaml.v3"

	"github.com/tinchoram/groot-cli/internal/catalog"
	"github.com/tinchoram/groot-cli/library"
)

var wantTinchoram = []string{
	"debug", "developer", "devsecops-platform", "observability", "space-manager", "security-tester", "ux-designer",
}

// shortPromptAgents keep a prompt below the 25-line floor on purpose: debug only
// writes the feedback backlog, so its prompt stays narrow.
var shortPromptAgents = []string{"debug"}

// catalogOf reads only the home directory: no index and no public source, so
// the test never touches the network.
func catalogOf(home string) Catalog { return Catalog{Home: home} }

func builtinOf(t *testing.T) []Entry {
	t.Helper()
	entries, err := catalogOf(t.TempDir()).List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	var out []Entry
	for _, e := range entries {
		if e.Source == SourceBuiltin {
			out = append(out, e)
		}
	}
	return out
}

func TestBuiltinEntriesLoadAndValidate(t *testing.T) {
	var names []string
	for _, e := range builtinOf(t) {
		names = append(names, e.Name)
		if err := catalog.Validate(e.AgentDef); err != nil {
			t.Errorf("%s does not validate: %v", e.Name, err)
		}
		if e.Description == "" || e.Author == "" || e.Collection == "" {
			t.Errorf("%s lacks description, author or collection: %+v", e.Name, e)
		}
		if lines := len(strings.Split(e.Prompt, "\n")); !slices.Contains(shortPromptAgents, e.Name) && (lines < 25 || lines > 50) {
			t.Errorf("%s prompt has %d lines, want 25 to 50", e.Name, lines)
		}
	}
	for _, want := range wantTinchoram {
		if !slices.Contains(names, want) {
			t.Errorf("builtin library lacks %s (have %v)", want, names)
		}
	}
	if len(names) != len(wantTinchoram) {
		t.Errorf("builtin library has %d agents, want %d: %v", len(names), len(wantTinchoram), names)
	}
}

func TestBuiltinIsOnlyTheTinchoramCollection(t *testing.T) {
	for _, e := range builtinOf(t) {
		if e.Collection != "@tinchoram" || e.Author != "@tinchoram" {
			t.Errorf("%s: collection %q author %q, want @tinchoram", e.Name, e.Collection, e.Author)
		}
	}
}

// indexFile mirrors library/index.yaml for the test.
type indexFile struct {
	Collection string `yaml:"collection"`
	Author     string `yaml:"author"`
	Agents     []struct {
		Name        string `yaml:"name"`
		Description string `yaml:"description"`
	} `yaml:"agents"`
}

func TestIndexMatchesEmbeddedAgents(t *testing.T) {
	raw, err := library.Agents.ReadFile("index.yaml")
	if err != nil {
		t.Fatalf("library/index.yaml must be embedded: %v", err)
	}
	var idx indexFile
	if err := yaml.Unmarshal(raw, &idx); err != nil {
		t.Fatalf("index.yaml does not parse: %v", err)
	}
	if idx.Collection != "@tinchoram" {
		t.Fatalf("index collection = %q", idx.Collection)
	}
	byName := map[string]Entry{}
	for _, e := range builtinOf(t) {
		byName[e.Name] = e
	}
	if len(idx.Agents) != len(wantTinchoram) {
		t.Fatalf("index lists %d agents, want %d", len(idx.Agents), len(wantTinchoram))
	}
	for _, a := range idx.Agents {
		e, ok := byName[a.Name]
		if !ok {
			t.Errorf("index lists %s, which is not embedded", a.Name)
			continue
		}
		if e.Description != a.Description {
			t.Errorf("%s: index description %q differs from the file %q", a.Name, a.Description, e.Description)
		}
	}
}

func TestUserFileOverridesBuiltinOfSameName(t *testing.T) {
	home := t.TempDir()
	dir := filepath.Join(home, "library")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	body := "---\nname: developer\ndescription: \"my own developer\"\ntools: [read_file]\n---\nMine.\n"
	if err := os.WriteFile(filepath.Join(dir, "developer.md"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	e, err := catalogOf(home).Get("developer")
	if err != nil {
		t.Fatal(err)
	}
	if e.Source != SourceUser || e.Description != "my own developer" || e.Prompt != "Mine." || e.Collection != "local" {
		t.Fatalf("user file did not win: %+v", e)
	}
}

func TestUserFileAddsNewAgent(t *testing.T) {
	home := t.TempDir()
	dir := filepath.Join(home, "library")
	_ = os.MkdirAll(dir, 0o755)
	body := "---\nname: rust-dev\ndescription: \"Rust helper\"\ntools: [read_file, recall]\n---\nHelp with Rust.\n"
	_ = os.WriteFile(filepath.Join(dir, "rust-dev.md"), []byte(body), 0o644)
	e, err := catalogOf(home).Get("rust-dev")
	if err != nil || e.Source != SourceUser {
		t.Fatalf("Get = %+v (%v)", e, err)
	}
}

func TestInvalidUserFileIsReported(t *testing.T) {
	home := t.TempDir()
	dir := filepath.Join(home, "library")
	_ = os.MkdirAll(dir, 0o755)
	_ = os.WriteFile(filepath.Join(dir, "broken.md"), []byte("no frontmatter here"), 0o644)
	if _, err := catalogOf(home).List(); err == nil {
		t.Fatal("a broken user file must be reported")
	}
}

func TestFileNameMustMatchName(t *testing.T) {
	home := t.TempDir()
	dir := filepath.Join(home, "library")
	_ = os.MkdirAll(dir, 0o755)
	_ = os.WriteFile(filepath.Join(dir, "alias.md"), []byte("---\nname: other\ntools: [recall]\n---\nx\n"), 0o644)
	if _, err := catalogOf(home).List(); err == nil {
		t.Fatal("a file whose name differs from its file name must be refused")
	}
}

func TestGetUnknownName(t *testing.T) {
	if _, err := catalogOf(t.TempDir()).Get("nobody"); err == nil {
		t.Fatal("unknown library agent must be an error")
	}
}

// indexServer serves an index document and counts the requests it gets.
func indexServer(t *testing.T, body string, status int, requests *int32) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if requests != nil {
			atomic.AddInt32(requests, 1)
		}
		w.Header().Set("Content-Type", "text/yaml; charset=utf-8")
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}))
}

func indexCatalog(home string, url string) Catalog {
	return Catalog{Home: home, Indexes: []Source{{Name: "@ana", URL: url}}}
}

func TestRemoteIndexAgentsAreListedWithAuthor(t *testing.T) {
	agent := fmt.Sprintf("http://example.invalid/%s/planner.md", t.Name())
	srv := indexServer(t, fmt.Sprintf("collection: \"@ana\"\nauthor: \"@ana\"\nagents:\n  - name: planner\n    description: \"Plans work\"\n    url: %s\n", agent), http.StatusOK, nil)
	defer srv.Close()
	entries, err := indexCatalog(t.TempDir(), srv.URL+"/index.yaml").List()
	if err != nil {
		t.Fatal(err)
	}
	var found *Entry
	for i := range entries {
		if entries[i].Name == "planner" {
			found = &entries[i]
		}
	}
	if found == nil || found.Source != SourceRemote || found.Author != "@ana" || found.Collection != "@ana" || found.URL != agent {
		t.Fatalf("remote entry = %+v", found)
	}
}

func TestRemoteIndexIsFetchedOncePerProcess(t *testing.T) {
	var n int32
	srv := indexServer(t, "collection: \"@cache\"\nauthor: \"@cache\"\nagents: []\n", http.StatusOK, &n)
	defer srv.Close()
	c := Catalog{Home: t.TempDir(), Indexes: []Source{{Name: "@cache", URL: srv.URL + "/index.yaml"}}}
	for i := 0; i < 3; i++ {
		if _, err := c.List(); err != nil {
			t.Fatal(err)
		}
	}
	if got := atomic.LoadInt32(&n); got != 1 {
		t.Fatalf("index requested %d times, want 1", got)
	}
}

func TestBadRemoteIndexIsAWarningNotAnError(t *testing.T) {
	srv := indexServer(t, "agents: [unclosed", http.StatusOK, nil)
	defer srv.Close()
	entries, err := indexCatalog(t.TempDir(), srv.URL+"/index.yaml").List()
	if err != nil {
		t.Fatalf("a bad source must not fail the list: %v", err)
	}
	if !hasWarning(entries, "@ana") {
		t.Fatalf("expected a warning entry for @ana: %+v", entries)
	}
}

func TestOversizedAndFailingIndexesAreWarnings(t *testing.T) {
	big := indexServer(t, strings.Repeat("a", 64*1024+10), http.StatusOK, nil)
	defer big.Close()
	fail := indexServer(t, "collection: x", http.StatusNotFound, nil)
	defer fail.Close()
	entries, err := Catalog{Home: t.TempDir(), Indexes: []Source{
		{Name: "@big", URL: big.URL + "/i.yaml"},
		{Name: "@gone", URL: fail.URL + "/i.yaml"},
	}}.List()
	if err != nil {
		t.Fatal(err)
	}
	if !hasWarning(entries, "@big") || !hasWarning(entries, "@gone") {
		t.Fatalf("both sources should be warnings: %+v", entries)
	}
}

func TestBuiltinWinsOverRemoteWithSameName(t *testing.T) {
	agent := "http://example.invalid/developer.md"
	srv := indexServer(t, fmt.Sprintf("collection: \"@other\"\nauthor: \"@other\"\nagents:\n  - name: developer\n    description: \"Impostor\"\n    url: %s\n", agent), http.StatusOK, nil)
	defer srv.Close()
	e, err := Catalog{Home: t.TempDir(), Indexes: []Source{{Name: "@other", URL: srv.URL + "/i.yaml"}}}.Get("developer")
	if err != nil {
		t.Fatal(err)
	}
	if e.Source != SourceBuiltin {
		t.Fatalf("builtin must win over remote, got %s", e.Source)
	}
}

func TestGetRemoteDownloadsAndValidatesAgent(t *testing.T) {
	md := "---\nname: planner\ndescription: \"Plans work\"\ntools: [read_file, recall]\n---\nPlan the work.\n"
	agentSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/markdown")
		_, _ = w.Write([]byte(md))
	}))
	defer agentSrv.Close()
	idx := indexServer(t, fmt.Sprintf("collection: \"@dl\"\nauthor: \"@dl\"\nagents:\n  - name: planner\n    description: \"Plans work\"\n    url: %s/planner.md\n", agentSrv.URL), http.StatusOK, nil)
	defer idx.Close()
	e, err := indexCatalog(t.TempDir(), idx.URL+"/i.yaml").Get("planner")
	if err != nil {
		t.Fatalf("Get remote: %v", err)
	}
	if e.Source != SourceRemote || e.Prompt != "Plan the work." || strings.Join(e.Tools, ",") != "read_file,recall" {
		t.Fatalf("downloaded entry = %+v", e)
	}
}

func TestRemoteAgentNameMustMatchIndex(t *testing.T) {
	agentSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("---\nname: other\ntools: [recall]\n---\nx\n"))
	}))
	defer agentSrv.Close()
	idx := indexServer(t, fmt.Sprintf("collection: \"@mm\"\nauthor: \"@mm\"\nagents:\n  - name: planner\n    description: d\n    url: %s/x.md\n", agentSrv.URL), http.StatusOK, nil)
	defer idx.Close()
	if _, err := indexCatalog(t.TempDir(), idx.URL+"/i.yaml").Get("planner"); err == nil {
		t.Fatal("an agent file with a different name than the index must be refused")
	}
}

func TestPublicSourcesAreListedAfterTheTinchoramCollection(t *testing.T) {
	repo := "acme/publicly"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, treeJSON("categories/devops/devops-engineer.md"))
	}))
	defer srv.Close()
	useGithub(t, srv)
	c := catalogOf(t.TempDir())
	c.Public = []PublicSource{sampleSource(repo)}
	entries, err := c.List()
	if err != nil {
		t.Fatal(err)
	}
	var sawPublic bool
	for _, e := range entries {
		if e.Source == SourcePublic {
			sawPublic = true
			if e.Name != "devops-engineer" || e.Category != "devops" || e.License != "MIT" {
				t.Fatalf("public entry = %+v", e)
			}
		}
	}
	if !sawPublic {
		t.Fatal("the public agent is missing from the list")
	}
}

func TestPublicSourceFailureIsAWarning(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "down", http.StatusServiceUnavailable)
	}))
	defer srv.Close()
	useGithub(t, srv)
	c := catalogOf(t.TempDir())
	c.Public = []PublicSource{sampleSource("acme/down")}
	entries, err := c.List()
	if err != nil {
		t.Fatalf("a failing public source must not fail the list: %v", err)
	}
	if !hasWarning(entries, sampleSource("acme/down").Name) {
		t.Fatalf("expected a warning for the public source: %+v", entries)
	}
}

func TestGetPublicAgentByQualifiedName(t *testing.T) {
	repo := "acme/qualified"
	md := "---\nname: devops-engineer\ndescription: \"Pipelines\"\ntools: [Read]\n---\nBuild pipelines.\n"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/repos/acme/qualified/git/trees/main":
			fmt.Fprint(w, treeJSON("categories/devops/devops-engineer.md"))
		case "/raw/acme/qualified/main/categories/devops/devops-engineer.md":
			_, _ = w.Write([]byte(md))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	useGithub(t, srv)
	src := sampleSource(repo)
	c := catalogOf(t.TempDir())
	c.Public = []PublicSource{src}
	e, err := c.Get(src.Name + "/devops-engineer")
	if err != nil {
		t.Fatalf("Get public: %v", err)
	}
	if e.Name != "devops-engineer" || e.Prompt != "Build pipelines." {
		t.Fatalf("public agent = %+v", e.AgentDef)
	}
	if _, err := c.Get("no-such-source/x"); err == nil {
		t.Fatal("an unknown source must be an error")
	}
}

func hasWarning(entries []Entry, source string) bool {
	for _, e := range entries {
		if e.Source == SourceWarning && e.Collection == source {
			return true
		}
	}
	return false
}
