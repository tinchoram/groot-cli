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
	"unicode/utf8"

	"github.com/tinchoram/groot-cli/internal/api"
)

// readFileLimit is the default and the largest window read_file returns per call.
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

// readInput is the read_file input. Limit is a pointer so an explicit 0 can be
// told apart from an absent limit.
type readInput struct {
	Path   string `json:"path"`
	Offset int64  `json:"offset"`
	Limit  *int64 `json:"limit"`
}

func parseReadInput(input string) (readInput, error) {
	var in readInput
	if err := json.Unmarshal([]byte(input), &in); err != nil {
		return in, fmt.Errorf("invalid input: %w", err)
	}
	if strings.TrimSpace(in.Path) == "" {
		return in, errors.New("path is required")
	}
	if in.Offset < 0 {
		return in, errors.New("offset must be 0 or more")
	}
	if in.Limit != nil && *in.Limit < 1 {
		return in, errors.New("limit must be 1 or more")
	}
	return in, nil
}

// cutAtRuneBoundary moves a window end back so that a rune split by the window
// edge is left for the next read. If the cut would leave nothing, the window is
// kept as it is, so a caller always makes progress.
func cutAtRuneBoundary(b []byte) []byte {
	for i := len(b) - 1; i >= 0 && i >= len(b)-utf8.UTFMax; i-- {
		c := b[i]
		if c < utf8.RuneSelf {
			return b
		}
		if !utf8.RuneStart(c) {
			continue
		}
		need := 1
		switch {
		case c >= 0xF0:
			need = 4
		case c >= 0xE0:
			need = 3
		case c >= 0xC0:
			need = 2
		}
		if i+need > len(b) && i > 0 {
			return b[:i]
		}
		return b
	}
	return b
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
		Description: "Read a text file inside the working directory and return its contents. Use it to read a known path, a window of a long file, or a saved output under .groot/outputs; use glob or list_dir first when you do not know the path. Paths outside the working directory are refused. Each call returns up to 64 KB; a larger file says so in its first line, and you continue with offset.",
		InputSchema: json.RawMessage(`{"type":"object","properties":{"path":{"type":"string","description":"Path relative to the working directory, or an absolute path inside it"},"offset":{"type":"integer","description":"Byte offset to start reading at; default 0"},"limit":{"type":"integer","description":"Maximum bytes to return; default 65536, which is also the maximum"}},"required":["path"]}`),
	}
}

func (*ReadFile) Risk() Risk { return RiskRead }

// Untrusted: a file in the workspace can carry text written by anyone.
func (*ReadFile) Untrusted() bool { return true }

func (r *ReadFile) RiskFor(string) Risk { return r.Risk() }

func (*ReadFile) Preview(input string) string {
	in, err := parseReadInput(input)
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
	in, err := parseReadInput(input)
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
	info, err := f.Stat()
	if err != nil {
		return err.Error(), true
	}
	size := info.Size()
	if in.Offset > size {
		return fmt.Sprintf("offset %d is past the end of the file (%d bytes)", in.Offset, size), true
	}

	limit := int64(readFileLimit)
	clamp := ""
	if in.Limit != nil {
		if *in.Limit > readFileLimit {
			clamp = fmt.Sprintf("limit %d clamped to %d", *in.Limit, readFileLimit)
		} else {
			limit = *in.Limit
		}
	}

	if _, err := f.Seek(in.Offset, io.SeekStart); err != nil {
		return err.Error(), true
	}
	buf, err := io.ReadAll(io.LimitReader(f, limit))
	if err != nil {
		return err.Error(), true
	}
	end := in.Offset + int64(len(buf))
	if end < size {
		buf = cutAtRuneBoundary(buf)
		end = in.Offset + int64(len(buf))
	}

	if in.Offset == 0 && end == size {
		if clamp == "" {
			return string(buf), false
		}
		return "[read_file: " + clamp + "]\n" + string(buf), false
	}
	if len(buf) == 0 {
		return fmt.Sprintf("[read_file: offset %d is the end of the file (%d bytes)]", in.Offset, size), false
	}

	next := "end of file"
	if end < size {
		next = fmt.Sprintf("call again with offset=%d to continue", end)
	}
	parts := []string{}
	if clamp != "" {
		parts = append(parts, clamp)
	}
	parts = append(parts, fmt.Sprintf("showing bytes %d-%d of %d", in.Offset, end-1, size), next)
	return "[read_file: " + strings.Join(parts, "; ") + "]\n" + string(buf), false
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
		Description: "Create or overwrite a text file inside the working directory with the full content you give. Use it for new files and complete rewrites; for a change that is scripted or touches many files, use bash. The user sees a diff and must approve it before anything is written. Paths outside the working directory are refused, and a successful call reports the bytes written.",
		InputSchema: json.RawMessage(`{"type":"object","properties":{"path":{"type":"string","description":"File path relative to the working directory; missing parent directories are created"},"content":{"type":"string","description":"The complete new file content, replacing any previous content"}},"required":["path","content"]}`),
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
