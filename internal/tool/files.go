package tool

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/tinchoram/groot-cli/internal/api"
)

const readFileLimit = 64 * 1024

// sandbox confines file paths to a root directory, following symlinks.
type sandbox struct {
	root string // absolute, symlink-resolved
}

func newSandbox(root string) (sandbox, error) {
	if root == "" {
		root = "."
	}
	abs, err := filepath.Abs(root)
	if err != nil {
		return sandbox{}, err
	}
	real, err := filepath.EvalSymlinks(abs)
	if err != nil {
		return sandbox{}, fmt.Errorf("working directory: %w", err)
	}
	return sandbox{root: real}, nil
}

var errOutside = errors.New("path is outside the working directory")

// resolve returns the real absolute path for p, or errOutside when it escapes
// the root. Paths that do not exist yet are checked through their deepest
// existing ancestor.
func (s sandbox) resolve(p string) (string, error) {
	candidate := p
	if !filepath.IsAbs(candidate) {
		candidate = filepath.Join(s.root, candidate)
	}
	candidate = filepath.Clean(candidate)

	existing, rest := candidate, ""
	for {
		if _, err := os.Lstat(existing); err == nil {
			break
		}
		parent := filepath.Dir(existing)
		if parent == existing {
			break
		}
		rest = filepath.Join(filepath.Base(existing), rest)
		existing = parent
	}
	real, err := filepath.EvalSymlinks(existing)
	if err != nil {
		return "", err
	}
	full := filepath.Join(real, rest)

	rel, err := filepath.Rel(s.root, full)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || filepath.IsAbs(rel) {
		return "", errOutside
	}
	return full, nil
}

type pathInput struct {
	Path    string `json:"path"`
	Content string `json:"content"`
}

func parsePathInput(input string) (pathInput, error) {
	var in pathInput
	if err := json.Unmarshal([]byte(input), &in); err != nil {
		return in, fmt.Errorf("invalid input: %w", err)
	}
	if strings.TrimSpace(in.Path) == "" {
		return in, errors.New("path is required")
	}
	return in, nil
}

// ReadFile reads a file inside the working directory.
type ReadFile struct {
	box     sandbox
	rootErr error
}

// NewReadFile returns the read_file tool confined to root.
func NewReadFile(root string) *ReadFile {
	box, err := newSandbox(root)
	return &ReadFile{box: box, rootErr: err}
}

func (*ReadFile) Definition() api.ToolDef {
	return api.ToolDef{
		Name:        "read_file",
		Description: "Read a text file inside the working directory. Output is capped at 64 KB.",
		InputSchema: json.RawMessage(`{"type":"object","properties":{"path":{"type":"string","description":"Path relative to the working directory"}},"required":["path"]}`),
	}
}

func (*ReadFile) Risk() Risk { return RiskRead }

// Untrusted: a file in the workspace can carry text written by anyone.
func (*ReadFile) Untrusted() bool { return true }

func (r *ReadFile) RiskFor(string) Risk { return r.Risk() }

func (*ReadFile) Preview(input string) string {
	in, err := parsePathInput(input)
	if err != nil {
		return "read_file: " + err.Error()
	}
	return Redact("read " + in.Path)
}

func (r *ReadFile) Execute(_ context.Context, input string) (string, bool) {
	out, isErr := r.execute(input)
	return Redact(out), isErr
}

func (r *ReadFile) execute(input string) (string, bool) {
	if r.rootErr != nil {
		return r.rootErr.Error(), true
	}
	in, err := parsePathInput(input)
	if err != nil {
		return err.Error(), true
	}
	full, err := r.box.resolve(in.Path)
	if err != nil {
		return err.Error(), true
	}
	f, err := os.Open(full)
	if err != nil {
		return err.Error(), true
	}
	defer f.Close()
	buf, err := io.ReadAll(io.LimitReader(f, readFileLimit+1))
	if err != nil {
		return err.Error(), true
	}
	if len(buf) > readFileLimit {
		return string(buf[:readFileLimit]) + "\n... [output truncated at 64 KB]", false
	}
	return string(buf), false
}

// WriteFile creates or replaces a file inside the working directory.
type WriteFile struct {
	box     sandbox
	rootErr error
}

// NewWriteFile returns the write_file tool confined to root.
func NewWriteFile(root string) *WriteFile {
	box, err := newSandbox(root)
	return &WriteFile{box: box, rootErr: err}
}

func (*WriteFile) Definition() api.ToolDef {
	return api.ToolDef{
		Name:        "write_file",
		Description: "Create or overwrite a file inside the working directory. The user reviews a diff and must approve.",
		InputSchema: json.RawMessage(`{"type":"object","properties":{"path":{"type":"string"},"content":{"type":"string"}},"required":["path","content"]}`),
	}
}

func (*WriteFile) Risk() Risk { return RiskWrite }

func (w *WriteFile) RiskFor(string) Risk { return w.Risk() }

// Preview shows the unified diff between the current file and the new content.
func (w *WriteFile) Preview(input string) string {
	in, err := parsePathInput(input)
	if err != nil {
		return "write_file: " + err.Error()
	}
	oldText := ""
	label := in.Path + " (new file)"
	if w.rootErr == nil {
		if full, err := w.box.resolve(in.Path); err == nil {
			if b, err := os.ReadFile(full); err == nil {
				oldText = string(b)
				label = in.Path
			}
		}
	}
	diff := UnifiedDiff(label, in.Path, oldText, in.Content)
	if diff == "" {
		diff = "(no changes)"
	}
	return Redact(diff)
}

func (w *WriteFile) Execute(_ context.Context, input string) (string, bool) {
	out, isErr := w.execute(input)
	return Redact(out), isErr
}

func (w *WriteFile) execute(input string) (string, bool) {
	if w.rootErr != nil {
		return w.rootErr.Error(), true
	}
	in, err := parsePathInput(input)
	if err != nil {
		return err.Error(), true
	}
	full, err := w.box.resolve(in.Path)
	if err != nil {
		return err.Error(), true
	}
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		return err.Error(), true
	}
	if err := os.WriteFile(full, []byte(in.Content), 0o644); err != nil {
		return err.Error(), true
	}
	return fmt.Sprintf("wrote %d bytes to %s", len(in.Content), in.Path), false
}
