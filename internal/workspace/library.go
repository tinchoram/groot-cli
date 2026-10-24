package workspace

import (
	"fmt"
	"slices"

	"github.com/tinchoram/groot-cli/internal/admintool"
	"github.com/tinchoram/groot-cli/internal/catalog"
	"github.com/tinchoram/groot-cli/internal/library"
)

// LibraryInfo describes an agent that can be installed from the library.
type LibraryInfo = admintool.LibraryInfo

// LibraryDetail is a library agent with its prompt, for /agent show.
type LibraryDetail struct {
	LibraryInfo
	Prompt string
}

// libraryCatalog builds the catalog for this workspace: the home directory,
// the configured index sources, and the embedded public sources (unless the
// options turn them off).
func (w *Workspace) libraryCatalog() (library.Catalog, error) {
	var sources []library.Source
	for _, src := range w.opts.Config.Library.Sources {
		sources = append(sources, library.Source{Name: src.Name, URL: src.URL})
	}
	c, err := library.Load(w.opts.Home, sources)
	if err != nil {
		return library.Catalog{}, err
	}
	if w.opts.DisablePublic {
		c.Public = nil
	}
	return c, nil
}

func toLibraryInfo(e library.Entry) LibraryInfo {
	return LibraryInfo{
		Name: e.Name, Description: e.Description, Tools: e.Tools, Source: e.Source,
		Collection: e.Collection, Author: e.Author, URL: e.URL,
		License: e.License, ValidatedBy: e.ValidatedBy,
	}
}

// LibraryAgents lists the builtin, local, configured and public library agents
// in catalog order. A source that failed appears as a warning entry.
func (w *Workspace) LibraryAgents() ([]LibraryInfo, error) {
	c, err := w.libraryCatalog()
	if err != nil {
		return nil, err
	}
	entries, err := c.List()
	if err != nil {
		return nil, err
	}
	out := make([]LibraryInfo, 0, len(entries))
	for _, e := range entries {
		if e.Source == library.SourceWarning {
			out = append(out, LibraryInfo{Name: "source " + e.Collection, Description: e.Description, Source: library.SourceWarning, Collection: e.Collection})
			continue
		}
		out = append(out, toLibraryInfo(e))
	}
	return out, nil
}

// LibraryAgent returns one library agent with its prompt. A public agent is
// named "<source>/<agent>".
func (w *Workspace) LibraryAgent(name string) (LibraryDetail, error) {
	c, err := w.libraryCatalog()
	if err != nil {
		return LibraryDetail{}, err
	}
	e, err := c.Get(name)
	if err != nil {
		return LibraryDetail{}, err
	}
	return LibraryDetail{LibraryInfo: toLibraryInfo(e), Prompt: e.Prompt}, nil
}

// InstallLibraryAgent copies a library agent into the catalog and, when a space
// is named, assigns it there. The space is checked first, so a refused install
// writes nothing. An existing agent is replaced only with overwrite, and groot
// is never replaced. A public agent is named "<source>/<agent>"; it is installed
// under the agent's own name.
func (w *Workspace) InstallLibraryAgent(name, spaceName string, overwrite bool) error {
	entryName := name
	if i := indexSlash(name); i >= 0 {
		entryName = name[i+1:]
	}
	if entryName == managedAgent {
		return fmt.Errorf("agent %q is managed and cannot be installed over", managedAgent)
	}
	if spaceName != "" {
		if _, ok := w.opts.Spaces.Get(spaceName); !ok {
			return fmt.Errorf("no space named %q", spaceName)
		}
	}
	c, err := w.libraryCatalog()
	if err != nil {
		return err
	}
	entry, err := c.Get(name)
	if err != nil {
		return err
	}
	if catalog.Exists(w.opts.Home, entry.Name) && !overwrite {
		return fmt.Errorf("agent %q already exists; pass overwrite to replace it", entry.Name)
	}
	// Public files rarely carry an author; stamp the source so the catalog
	// keeps where the agent came from.
	if entry.AgentDef.Author == "" {
		entry.AgentDef.Author = entry.Author
	}
	if entry.AgentDef.Collection == "" {
		entry.AgentDef.Collection = entry.Collection
	}
	if err := catalog.Save(w.opts.Home, entry.AgentDef, entry.Prompt); err != nil {
		return err
	}
	if spaceName != "" {
		sp, _ := w.opts.Spaces.Get(spaceName)
		if !slices.Contains(sp.Agents, entry.Name) {
			if err := w.opts.Spaces.Assign(spaceName, entry.Name); err != nil {
				return err
			}
		}
	}
	return w.reload()
}

func indexSlash(s string) int {
	for i := range s {
		if s[i] == '/' {
			return i
		}
	}
	return -1
}
