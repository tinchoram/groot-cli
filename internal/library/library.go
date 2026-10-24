// Package library lists the agents that can be installed into the catalog.
// A Catalog reads four kinds of source:
//
//   - the builtin agents embedded in the binary (collection @tinchoram),
//   - the user's own files in <home>/library (collection local),
//   - index sources: index.yaml files, from the config and from sources.yaml,
//   - public GitHub sources listed in sources.yaml.
//
// A user file replaces a builtin of the same name, and a builtin replaces an
// index entry. Public agents are never merged by name: they are addressed as
// "<source>/<agent>". A source that cannot be read becomes a warning entry,
// never an error.
package library

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"gopkg.in/yaml.v3"

	"github.com/tinchoram/groot-cli/internal/agentimport"
	"github.com/tinchoram/groot-cli/internal/catalog"
	"github.com/tinchoram/groot-cli/library"
)

// Where an entry comes from.
const (
	SourceBuiltin = "builtin"
	SourceUser    = "user"
	SourceRemote  = "remote"
	SourcePublic  = "public"
	SourceWarning = "warning"
)

const (
	builtinCollection = "@tinchoram"
	localCollection   = "local"
	maxIndexBytes     = 64 * 1024
	indexTimeout      = 10 * time.Second
)

// Source is an index.yaml source: a display name and the URL of the index.
type Source struct {
	Name string
	URL  string
}

// Entry is one installable agent. For a warning, Collection names the source
// that failed and Description says why.
type Entry struct {
	catalog.AgentDef
	Source     string
	Author     string
	Collection string
	// URL is the agent file of a remote or public entry.
	URL string
	// Category is the folder a public agent lives in.
	Category string
	// License and ValidatedBy describe a public source.
	License     string
	ValidatedBy string
}

// Catalog is what the library reads: the local home, the index sources, and the
// public GitHub sources.
type Catalog struct {
	Home    string
	Indexes []Source
	Public  []PublicSource
}

// Load builds the catalog for a home directory. configSources are the user's
// extra index sources from groot.yaml; the public sources come from the
// embedded sources.yaml. Index-kind entries there join the index sources.
func Load(home string, configSources []Source) (Catalog, error) {
	srcs, err := EmbeddedSources()
	if err != nil {
		return Catalog{}, err
	}
	c := Catalog{Home: home, Indexes: append([]Source(nil), configSources...)}
	for _, s := range srcs {
		switch s.Kind {
		case "github":
			c.Public = append(c.Public, s)
		case "index":
			c.Indexes = append(c.Indexes, Source{Name: s.Name, URL: s.URL})
		}
	}
	return c, nil
}

// priority decides which entry wins when two sources name the same agent.
func priority(source string) int {
	switch source {
	case SourceUser:
		return 3
	case SourceBuiltin:
		return 2
	case SourceRemote:
		return 1
	}
	return 0
}

// List returns every installable agent: the merged builtin, user and index
// agents sorted by name, then the public agents in source order, then one
// warning for each source that could not be read. The only error is for a
// broken builtin or user file.
func (c Catalog) List() ([]Entry, error) {
	merged, warnings, err := c.listMerged()
	if err != nil {
		return nil, err
	}
	out := merged
	for _, src := range c.Public {
		entries, err := listGithub(src)
		if err != nil {
			warnings = append(warnings, warning(src.Name, err))
			continue
		}
		out = append(out, entries...)
	}
	return append(out, warnings...), nil
}

// Get returns one agent. "<source>/<agent>" names a public agent, which is
// downloaded and validated through the import rules. Any other name is looked up
// among the builtin, user and index agents.
func (c Catalog) Get(name string) (Entry, error) {
	if src, agent, ok := strings.Cut(name, "/"); ok {
		for _, p := range c.Public {
			if p.Name == src {
				return fetchPublic(p, agent)
			}
		}
		return Entry{}, fmt.Errorf("no public source named %q", src)
	}
	merged, _, err := c.listMerged()
	if err != nil {
		return Entry{}, err
	}
	for _, e := range merged {
		if e.Name != name {
			continue
		}
		if e.Source == SourceRemote {
			return fetchRemote(e)
		}
		return e, nil
	}
	return Entry{}, fmt.Errorf("no library agent named %q", name)
}

func warning(source string, err error) Entry {
	return Entry{
		Source:     SourceWarning,
		Collection: source,
		AgentDef:   catalog.AgentDef{Description: fmt.Sprintf("could not read %s: %v", source, err)},
	}
}

// listMerged returns the builtin, user and index agents merged by name, and the
// warnings for index sources that failed.
func (c Catalog) listMerged() ([]Entry, []Entry, error) {
	byName := map[string]Entry{}
	put := func(e Entry) {
		cur, ok := byName[e.Name]
		if !ok || priority(e.Source) > priority(cur.Source) {
			byName[e.Name] = e
		}
	}

	builtin, err := readBuiltin()
	if err != nil {
		return nil, nil, err
	}
	for _, e := range builtin {
		put(e)
	}

	var warnings []Entry
	for _, src := range c.Indexes {
		entries, err := remoteEntries(src)
		if err != nil {
			warnings = append(warnings, warning(src.Name, err))
			continue
		}
		for _, e := range entries {
			put(e)
		}
	}

	user, err := readUser(c.Home)
	if err != nil {
		return nil, nil, err
	}
	for _, e := range user {
		put(e)
	}

	out := make([]Entry, 0, len(byName))
	for _, e := range byName {
		out = append(out, e)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, warnings, nil
}

// readBuiltin parses the embedded @tinchoram agents.
func readBuiltin() ([]Entry, error) {
	files, err := fs.ReadDir(library.Agents, "agents")
	if err != nil {
		return nil, fmt.Errorf("read builtin library: %w", err)
	}
	var out []Entry
	for _, f := range files {
		raw, err := fs.ReadFile(library.Agents, path.Join("agents", f.Name()))
		if err != nil {
			return nil, fmt.Errorf("read builtin %s: %w", f.Name(), err)
		}
		e, err := parseFile(raw, f.Name(), SourceBuiltin, builtinCollection)
		if err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, nil
}

// readUser parses <home>/library/*.md. Its files belong to the local collection.
func readUser(home string) ([]Entry, error) {
	dir := filepath.Join(home, "library")
	files, err := os.ReadDir(dir)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return nil, fmt.Errorf("read %s: %w", dir, err)
	}
	var out []Entry
	for _, f := range files {
		if f.IsDir() || !strings.HasSuffix(f.Name(), ".md") {
			continue
		}
		raw, err := os.ReadFile(filepath.Join(dir, f.Name()))
		if err != nil {
			return nil, fmt.Errorf("read %s: %w", f.Name(), err)
		}
		e, err := parseFile(raw, f.Name(), SourceUser, localCollection)
		if err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, nil
}

// parseFile reads one agent file. Its name must equal the file name without
// ".md", and the definition must validate.
func parseFile(raw []byte, fileName, source, defaultCollection string) (Entry, error) {
	def, err := catalog.ParseAgentFile(raw)
	if err != nil {
		return Entry{}, fmt.Errorf("%s (%s): %w", fileName, source, err)
	}
	if def.Name != strings.TrimSuffix(fileName, ".md") {
		return Entry{}, fmt.Errorf("%s (%s) names the agent %q; file name and name must match", fileName, source, def.Name)
	}
	if err := catalog.Validate(def); err != nil {
		return Entry{}, fmt.Errorf("%s (%s): %w", fileName, source, err)
	}
	meta, err := catalog.ParseMeta(raw)
	if err != nil {
		return Entry{}, fmt.Errorf("%s (%s): %w", fileName, source, err)
	}
	collection := firstNonEmpty(meta.Collection, defaultCollection)
	author := firstNonEmpty(meta.Author, defaultCollection)
	return Entry{AgentDef: def, Source: source, Author: author, Collection: collection}, nil
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}
	return ""
}

// index is an index.yaml document: a collection and its agent files.
type index struct {
	Collection string `yaml:"collection"`
	Author     string `yaml:"author"`
	Agents     []struct {
		Name        string `yaml:"name"`
		Description string `yaml:"description"`
		URL         string `yaml:"url"`
	} `yaml:"agents"`
}

// indexCache keeps each index for the life of the process. A failed fetch is
// not cached, so a later call tries again.
var indexCache = struct {
	sync.Mutex
	m map[string]index
}{m: map[string]index{}}

func indexClient() *http.Client { return &http.Client{Timeout: indexTimeout} }

// remoteEntries lists the agents of one index source.
func remoteEntries(src Source) ([]Entry, error) {
	idx, err := loadIndex(src.URL)
	if err != nil {
		return nil, err
	}
	collection := firstNonEmpty(idx.Collection, src.Name)
	author := firstNonEmpty(idx.Author, src.Name)
	var out []Entry
	for _, a := range idx.Agents {
		if a.Name == "" || strings.ContainsAny(a.Name, " /\\") {
			return nil, fmt.Errorf("index entry has an invalid name %q", a.Name)
		}
		if !strings.HasPrefix(a.URL, "https://") && !strings.HasPrefix(a.URL, "http://") {
			return nil, fmt.Errorf("index entry %q has no http(s) url", a.Name)
		}
		out = append(out, Entry{
			AgentDef:   catalog.AgentDef{Name: a.Name, Description: a.Description},
			Source:     SourceRemote,
			Author:     author,
			Collection: collection,
			URL:        a.URL,
		})
	}
	return out, nil
}

func loadIndex(url string) (index, error) {
	indexCache.Lock()
	idx, ok := indexCache.m[url]
	indexCache.Unlock()
	if ok {
		return idx, nil
	}
	idx, err := fetchIndex(url)
	if err != nil {
		return index{}, err
	}
	indexCache.Lock()
	indexCache.m[url] = idx
	indexCache.Unlock()
	return idx, nil
}

func fetchIndex(url string) (index, error) {
	resp, err := indexClient().Get(url)
	if err != nil {
		return index{}, fmt.Errorf("fetch index: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return index{}, fmt.Errorf("fetch index: HTTP %d", resp.StatusCode)
	}
	raw, err := io.ReadAll(io.LimitReader(resp.Body, maxIndexBytes+1))
	if err != nil {
		return index{}, fmt.Errorf("fetch index: %w", err)
	}
	if len(raw) > maxIndexBytes {
		return index{}, errors.New("index is larger than 64 KB")
	}
	var idx index
	if err := yaml.Unmarshal(raw, &idx); err != nil {
		return index{}, fmt.Errorf("index does not parse: %w", err)
	}
	return idx, nil
}

// fetchRemote downloads an index agent through the agent-import rules.
func fetchRemote(e Entry) (Entry, error) {
	plan, err := agentimport.Load(context.Background(), e.URL)
	if err != nil {
		return Entry{}, fmt.Errorf("download %s: %w", e.Name, err)
	}
	if plan.Def.Name != e.Name {
		return Entry{}, fmt.Errorf("the file at %s names %q; the index says %q", e.URL, plan.Def.Name, e.Name)
	}
	e.AgentDef = plan.Def
	return e, nil
}
