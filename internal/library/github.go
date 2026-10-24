package library

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"path"
	"sort"
	"strings"
	"sync"

	"github.com/tinchoram/groot-cli/internal/agentimport"
	"github.com/tinchoram/groot-cli/internal/catalog"
)

// The GitHub endpoints. Tests point them at a local server.
var (
	githubAPI = "https://api.github.com"
	githubRaw = "https://raw.githubusercontent.com"
)

// maxTreeBytes caps the tree response of the GitHub API.
const maxTreeBytes = 1 << 20

// githubUserAgent is sent with every request, as the GitHub API requires.
const githubUserAgent = "groot-cli"

type treeEntry struct {
	Path string `json:"path"`
	Type string `json:"type"`
}

type tree struct {
	Tree []treeEntry `json:"tree"`
}

// treeCache keeps each repository tree for the life of the process.
var treeCache = struct {
	sync.Mutex
	m map[string]tree
}{m: map[string]tree{}}

func githubGet(url string) ([]byte, error) {
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", githubUserAgent)
	req.Header.Set("Accept", "application/vnd.github+json")
	resp, err := indexClient().Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetch %s: %w", url, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("fetch %s: HTTP %d", url, resp.StatusCode)
	}
	return io.ReadAll(io.LimitReader(resp.Body, maxTreeBytes+1))
}

func loadTree(repo, ref string) (tree, error) {
	key := repo + "@" + ref
	treeCache.Lock()
	t, ok := treeCache.m[key]
	treeCache.Unlock()
	if ok {
		return t, nil
	}
	raw, err := githubGet(fmt.Sprintf("%s/repos/%s/git/trees/%s?recursive=1", githubAPI, repo, ref))
	if err != nil {
		return tree{}, err
	}
	if len(raw) > maxTreeBytes {
		return tree{}, errors.New("repository tree is larger than 1 MB")
	}
	if err := json.Unmarshal(raw, &t); err != nil {
		return tree{}, fmt.Errorf("repository tree does not parse: %w", err)
	}
	treeCache.Lock()
	treeCache.m[key] = t
	treeCache.Unlock()
	return t, nil
}

func rawURL(src PublicSource, filePath string) string {
	return fmt.Sprintf("%s/%s/%s/%s", githubRaw, src.Repo, src.Ref, filePath)
}

// listGithub lists the agent files under the source's path. The category is
// the folder that holds each file. README files are skipped.
func listGithub(src PublicSource) ([]Entry, error) {
	t, err := loadTree(src.Repo, src.Ref)
	if err != nil {
		return nil, err
	}
	prefix := strings.Trim(src.Path, "/") + "/"
	var out []Entry
	for _, e := range t.Tree {
		if e.Type != "blob" || !strings.HasPrefix(e.Path, prefix) || !strings.HasSuffix(e.Path, ".md") {
			continue
		}
		base := path.Base(e.Path)
		if strings.EqualFold(base, "README.md") {
			continue
		}
		rel := strings.TrimPrefix(e.Path, prefix)
		category := ""
		if dir := path.Dir(rel); dir != "." {
			category = path.Base(dir)
		}
		out = append(out, Entry{
			AgentDef:    catalog.AgentDef{Name: strings.TrimSuffix(base, ".md")},
			Source:      SourcePublic,
			Collection:  src.Name,
			Author:      src.Repo,
			Category:    category,
			License:     src.License,
			ValidatedBy: src.ValidatedBy,
			URL:         rawURL(src, e.Path),
		})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Category != out[j].Category {
			return out[i].Category < out[j].Category
		}
		return out[i].Name < out[j].Name
	})
	return out, nil
}

// fetchPublic downloads one agent of a public source and validates it with the
// import rules. The catalog name is the file name, as in the listing.
func fetchPublic(src PublicSource, agent string) (Entry, error) {
	entries, err := listGithub(src)
	if err != nil {
		return Entry{}, err
	}
	for _, e := range entries {
		if e.Name != agent {
			continue
		}
		plan, err := agentimport.Load(context.Background(), e.URL)
		if err != nil {
			return Entry{}, fmt.Errorf("download %s/%s: %w", src.Name, agent, err)
		}
		plan.Def.Name = agent
		e.AgentDef = plan.Def
		return e, nil
	}
	return Entry{}, fmt.Errorf("%s has no agent named %q", src.Name, agent)
}
