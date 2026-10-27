package tool

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"strings"

	"github.com/tinchoram/groot-cli/internal/api"
)

// errGlobCap stops the walk once the result cap is passed.
var errGlobCap = errors.New("glob cap reached")

// Glob finds files by pattern inside the working directory.
type Glob struct {
	box     sandbox
	rootErr error
}

// NewGlob returns the glob tool confined to root.
func NewGlob(root string) *Glob {
	box, err := newSandbox(root)
	return &Glob{box: box, rootErr: err}
}

type globInput struct {
	Pattern    string `json:"pattern"`
	Path       string `json:"path"`
	MaxResults int    `json:"max_results"`
}

func parseGlobInput(input string) (globInput, error) {
	var in globInput
	if err := json.Unmarshal([]byte(input), &in); err != nil {
		return in, fmt.Errorf("invalid input: %w", err)
	}
	in.Pattern = strings.TrimSpace(in.Pattern)
	if in.Pattern == "" {
		return in, errors.New("pattern is required")
	}
	if in.Path == "" {
		in.Path = "."
	}
	return in, nil
}

func (*Glob) Definition() api.ToolDef {
	return api.ToolDef{
		Name: "glob",
		Description: "Find files by name pattern under a directory inside the working directory, read-only, and return their paths relative to the working directory. Use it to locate files by name or extension, for example **/*.md; use list_dir to browse one folder and read_file to open a match. " +
			"Skips .git, node_modules, vendor, .groot, target, dist and build, and dotfiles unless the pattern starts with a dot. " +
			"Symlinks that leave the working directory are not returned, and at most 500 results come back.",
		InputSchema: json.RawMessage(`{"type":"object","properties":{` +
			`"pattern":{"type":"string","description":"Glob pattern: ** matches any number of directories and * matches within one name, so **/*.md finds every Markdown file"},` +
			`"path":{"type":"string","description":"Directory to search, relative to the working directory; default . (the working directory)"},` +
			`"max_results":{"type":"integer","description":"Most results to return; default 500"}},` +
			`"required":["pattern"]}`),
	}
}

func (*Glob) Risk() Risk { return RiskRead }

// Untrusted: matched file names are chosen by whoever created the files.
func (*Glob) Untrusted() bool { return true }

func (g *Glob) RiskFor(string) Risk { return g.Risk() }

func (*Glob) Preview(input string) string {
	in, err := parseGlobInput(input)
	if err != nil {
		return "glob: " + err.Error()
	}
	return Redact(fmt.Sprintf("glob %s in %s", in.Pattern, in.Path))
}

func (g *Glob) Execute(ctx context.Context, input string) (string, bool) {
	out, isErr := g.execute(ctx, input)
	return Redact(out), isErr
}

func (g *Glob) execute(ctx context.Context, input string) (string, bool) {
	if g.rootErr != nil {
		return g.rootErr.Error(), true
	}
	in, err := parseGlobInput(input)
	if err != nil {
		return err.Error(), true
	}
	if path.IsAbs(in.Pattern) || filepath.IsAbs(in.Pattern) || hasDotDot(in.Pattern) {
		return "pattern must be relative to the search path and must not contain ..", true
	}
	for _, seg := range strings.Split(in.Pattern, "/") {
		if seg == "**" {
			continue
		}
		if _, err := path.Match(seg, ""); err != nil {
			return fmt.Sprintf("bad pattern %q: %v", in.Pattern, err), true
		}
	}
	limit := listEntryCap
	if in.MaxResults > 0 && in.MaxResults < listEntryCap {
		limit = in.MaxResults
	}

	ctx, cancel := context.WithTimeout(ctx, listBudget)
	defer cancel()

	start, err := g.box.resolve(in.Path)
	if err != nil {
		return err.Error(), true
	}
	startRel, err := g.box.rootRel(start)
	if err != nil {
		return err.Error(), true
	}
	info, err := os.Stat(start)
	if err != nil {
		return err.Error(), true
	}
	if !info.IsDir() {
		return fmt.Sprintf("not a directory: %s", in.Path), true
	}

	showHidden := strings.HasPrefix(in.Pattern, ".")
	var results []string
	capped := false
	walkErr := filepath.WalkDir(start, func(full string, d fs.DirEntry, err error) error {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return ctxErr
		}
		if err != nil || full == start {
			return nil
		}
		name := d.Name()
		if d.IsDir() {
			if excludedDir(name) || (!showHidden && strings.HasPrefix(name, ".")) {
				return filepath.SkipDir
			}
			return nil
		}
		if !showHidden && strings.HasPrefix(name, ".") {
			return nil
		}
		rel, err := filepath.Rel(start, full)
		if err != nil {
			return nil
		}
		rel = filepath.ToSlash(rel)
		if d.Type()&fs.ModeSymlink != 0 {
			target, err := g.box.resolve(path.Join(startRel, rel))
			if err != nil {
				return nil
			}
			if info, err := os.Stat(target); err != nil || !info.Mode().IsRegular() {
				return nil
			}
		} else if !d.Type().IsRegular() {
			return nil
		}
		if !matchPath(in.Pattern, rel) {
			return nil
		}
		if len(results) == limit {
			capped = true
			return errGlobCap
		}
		results = append(results, path.Join(startRel, rel))
		return nil
	})
	if ctxErr := ctx.Err(); ctxErr != nil {
		return fmt.Sprintf("glob stopped: %v", ctxErr), true
	}
	if walkErr != nil && !errors.Is(walkErr, errGlobCap) {
		return walkErr.Error(), true
	}
	if len(results) == 0 {
		return "(no matches)", false
	}
	out := strings.Join(results, "\n")
	if capped {
		out += fmt.Sprintf("\n… result cap of %d reached; narrow the pattern or the path", limit)
	}
	return out, false
}

// hasDotDot reports whether a slash-separated pattern has a .. segment.
func hasDotDot(pattern string) bool {
	for _, seg := range strings.Split(filepath.ToSlash(pattern), "/") {
		if seg == ".." {
			return true
		}
	}
	return false
}

// matchPath matches a slash-separated name against a pattern of segments.
// A segment ** matches zero or more whole segments; any other segment follows
// path.Match. The name and the pattern are both relative to the search path.
func matchPath(pattern, name string) bool {
	return matchSegments(strings.Split(pattern, "/"), strings.Split(name, "/"))
}

func matchSegments(pat, name []string) bool {
	for len(pat) > 0 {
		if pat[0] == "**" {
			rest := pat[1:]
			for i := 0; i <= len(name); i++ {
				if matchSegments(rest, name[i:]) {
					return true
				}
			}
			return false
		}
		if len(name) == 0 {
			return false
		}
		ok, err := path.Match(pat[0], name[0])
		if err != nil || !ok {
			return false
		}
		pat, name = pat[1:], name[1:]
	}
	return len(name) == 0
}
