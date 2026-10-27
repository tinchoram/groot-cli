package tool

import (
	"context"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"

	"github.com/tinchoram/groot-cli/internal/api"
)

const (
	// listBudget bounds the wall-clock time of one list_dir or glob call.
	listBudget = 10 * time.Second
	// listEntryCap is the most lines list_dir and glob print.
	listEntryCap = 500
	// listDepthDefault and listDepthCap bound the levels of a recursive listing.
	listDepthDefault = 3
	listDepthCap     = 8
)

// excludedDirs are never listed or walked, whatever the input asks for.
var excludedDirs = map[string]bool{
	".git": true, "node_modules": true, "vendor": true, ".groot": true,
	"target": true, "dist": true, "build": true,
}

// excludedDir reports whether a directory name is on the fixed deny list.
func excludedDir(name string) bool { return excludedDirs[name] }

// rootRel is the path of a real absolute path, relative to the sandbox root, with slashes.
func (s sandbox) rootRel(real string) (string, error) {
	rel, err := filepath.Rel(s.root, real)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", errOutside
	}
	return filepath.ToSlash(rel), nil
}

// ListDir lists the entries of a directory inside the working directory.
type ListDir struct {
	box     sandbox
	rootErr error
}

// NewListDir returns the list_dir tool confined to root.
func NewListDir(root string) *ListDir {
	box, err := newSandbox(root)
	return &ListDir{box: box, rootErr: err}
}

type listInput struct {
	Path          string `json:"path"`
	Recursive     bool   `json:"recursive"`
	MaxDepth      int    `json:"max_depth"`
	IncludeHidden bool   `json:"include_hidden"`
}

func parseListInput(input string) (listInput, error) {
	var in listInput
	if strings.TrimSpace(input) == "" {
		return in, nil
	}
	if err := json.Unmarshal([]byte(input), &in); err != nil {
		return in, fmt.Errorf("invalid input: %w", err)
	}
	if in.Path == "" {
		in.Path = "."
	}
	return in, nil
}

func (*ListDir) Definition() api.ToolDef {
	return api.ToolDef{
		Name: "list_dir",
		Description: "List the entries of a directory inside the working directory, read-only, without reading file contents. " +
			"Each line is 'd <path>' for a directory or 'f <path> <bytes>' for a file, directories first. " +
			"Use it to see what a folder holds; use glob to find files by name across folders. " +
			"Not recursive unless asked; skips .git, node_modules, vendor, .groot, target, dist and build, and dotfiles unless include_hidden is set. At most 500 entries come back.",
		InputSchema: json.RawMessage(`{"type":"object","properties":{` +
			`"path":{"type":"string","description":"Directory relative to the working directory; default . (the working directory)"},` +
			`"recursive":{"type":"boolean","description":"Also list subdirectories; default false"},` +
			`"max_depth":{"type":"integer","description":"Levels to descend when recursive; default 3, maximum 8"},` +
			`"include_hidden":{"type":"boolean","description":"Include entries whose name starts with a dot; default false"}}}`),
	}
}

func (*ListDir) Risk() Risk { return RiskRead }

// Untrusted: file names are chosen by whoever created the files.
func (*ListDir) Untrusted() bool { return true }

func (l *ListDir) RiskFor(string) Risk { return l.Risk() }

func (*ListDir) Preview(input string) string {
	in, err := parseListInput(input)
	if err != nil {
		return "list_dir: " + err.Error()
	}
	if in.Recursive {
		return Redact(fmt.Sprintf("list %s (recursive)", in.Path))
	}
	return Redact("list " + in.Path)
}

func (l *ListDir) Execute(ctx context.Context, input string) (string, bool) {
	out, isErr := l.execute(ctx, input)
	return Redact(out), isErr
}

func (l *ListDir) execute(ctx context.Context, input string) (string, bool) {
	if l.rootErr != nil {
		return l.rootErr.Error(), true
	}
	in, err := parseListInput(input)
	if err != nil {
		return err.Error(), true
	}
	ctx, cancel := context.WithTimeout(ctx, listBudget)
	defer cancel()

	real, err := l.box.resolve(in.Path)
	if err != nil {
		return err.Error(), true
	}
	rel, err := l.box.rootRel(real)
	if err != nil {
		return err.Error(), true
	}
	for _, seg := range strings.Split(rel, "/") {
		if excludedDir(seg) {
			return fmt.Sprintf("%s is excluded from listing", in.Path), true
		}
	}
	info, err := os.Stat(real)
	if err != nil {
		return err.Error(), true
	}
	if !info.IsDir() {
		return fmt.Sprintf("not a directory: %s (read_file reads files)", in.Path), true
	}

	w := &lister{ctx: ctx, box: l.box, hidden: in.IncludeHidden, recursive: in.Recursive, depth: listDepth(in)}
	if err := w.walk(real, rel, 1); err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return fmt.Sprintf("list_dir stopped: %v", ctxErr), true
		}
		return err.Error(), true
	}
	return w.render(), false
}

// listDepth is the number of levels a listing descends: 1 for a plain listing.
func listDepth(in listInput) int {
	if !in.Recursive {
		return 1
	}
	switch {
	case in.MaxDepth <= 0:
		return listDepthDefault
	case in.MaxDepth > listDepthCap:
		return listDepthCap
	}
	return in.MaxDepth
}

// lister collects the lines of a listing and counts the ones past the cap.
type lister struct {
	ctx       context.Context
	box       sandbox
	hidden    bool
	recursive bool
	depth     int
	lines     []string
	total     int
}

func (w *lister) add(line string) {
	w.total++
	if len(w.lines) < listEntryCap {
		w.lines = append(w.lines, line)
	}
}

func (w *lister) render() string {
	if w.total == 0 {
		return "(empty)"
	}
	out := strings.Join(w.lines, "\n")
	if more := w.total - len(w.lines); more > 0 {
		out += fmt.Sprintf("\n… %d more", more)
	}
	return out
}

// listedEntry is one entry of a directory that passes the rules.
type listedEntry struct {
	name  string
	shown string // root-relative path
	real  string // absolute path to descend into; empty for symlinks
	dir   bool
	size  int64
}

// walk lists one directory: directories first, then files, then the
// subdirectories when the listing is recursive and the depth allows.
func (w *lister) walk(real, rel string, level int) error {
	if err := w.ctx.Err(); err != nil {
		return err
	}
	entries, err := os.ReadDir(real)
	if err != nil {
		return err
	}
	var dirs, files []listedEntry
	for _, e := range entries {
		if err := w.ctx.Err(); err != nil {
			return err
		}
		name := e.Name()
		if !w.hidden && strings.HasPrefix(name, ".") {
			continue
		}
		shown := path.Join(rel, name)
		full := filepath.Join(real, name)
		mode := e.Type()
		switch {
		case mode&fs.ModeSymlink != 0:
			// A symlink is listed only when it stays inside the root, and it
			// is never descended into.
			target, err := w.resolveLink(shown)
			if err != nil {
				continue
			}
			info, err := os.Stat(target)
			if err != nil {
				continue
			}
			if info.IsDir() {
				if !excludedDir(name) {
					dirs = append(dirs, listedEntry{name: name, shown: shown, dir: true})
				}
			} else if info.Mode().IsRegular() {
				files = append(files, listedEntry{name: name, shown: shown, size: info.Size()})
			}
		case e.IsDir():
			if excludedDir(name) {
				continue
			}
			dirs = append(dirs, listedEntry{name: name, shown: shown, real: full, dir: true})
		case mode.IsRegular():
			info, err := e.Info()
			if err != nil {
				continue
			}
			files = append(files, listedEntry{name: name, shown: shown, size: info.Size()})
		}
	}
	for _, d := range dirs {
		w.add("d " + d.shown)
	}
	for _, f := range files {
		w.add(fmt.Sprintf("f %s %d", f.shown, f.size))
	}
	if !w.recursive || level >= w.depth {
		return nil
	}
	for _, d := range dirs {
		if d.real == "" {
			continue
		}
		if err := w.walk(d.real, d.shown, level+1); err != nil && w.ctx.Err() != nil {
			return err
		}
	}
	return nil
}

// resolveLink returns the real path of a symlink at the root-relative path
// shown, or an error when it leaves the root or does not resolve.
func (w *lister) resolveLink(shown string) (string, error) {
	return w.box.resolve(shown)
}
