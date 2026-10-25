// Package agentimport reads an agent file from a local path or an http(s)
// URL and turns it into a catalog definition. The file uses the Markdown with
// YAML frontmatter format of agent catalogs (Claude Code and OpenCode style),
// so tool names from those tools are mapped to groot's builtin tools.
package agentimport

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/tinchoram/groot-cli/internal/catalog"
)

// MaxBytes caps the size of an imported file.
const MaxBytes = 256 * 1024

// previewLines is how much of the prompt the preview shows.
const previewLines = 5

// defaultClient fetches URLs with a ten-second limit.
var defaultClient = &http.Client{Timeout: 10 * time.Second}

// defaultTools is used when a file declares no usable tools.
var defaultTools = []string{"read_file", "write_file", "bash", "recall", "remember", "memory_get", "forget", "update_memory"}

// toolMap maps names used by Claude Code and OpenCode to builtin tools. A
// name that is already a groot tool passes through unchanged. Anything else is
// dropped with a warning.
var toolMap = map[string]string{
	"Read":      "read_file",
	"Write":     "write_file",
	"Edit":      "write_file",
	"MultiEdit": "write_file",
	"Bash":      "bash",
}

// noEquivalent names tools groot does not have yet; they get a specific warning.
var noEquivalent = map[string]bool{"Grep": true, "Glob": true}

// Planned is an import ready to save, with what was changed on the way.
type Planned struct {
	Source   string
	Def      catalog.AgentDef
	Warnings []string
}

// MapTools converts declared tool names to groot tool names. It returns the
// mapped names in order, without duplicates, and one warning per dropped name.
func MapTools(declared []string) (tools []string, warnings []string) {
	for _, name := range declared {
		mapped, ok := toolMap[name]
		if !ok && slices.Contains(catalog.KnownTools, name) {
			mapped, ok = name, true
		}
		if !ok {
			if noEquivalent[name] {
				warnings = append(warnings, fmt.Sprintf("%s has no equivalent yet and was dropped", name))
			} else {
				warnings = append(warnings, fmt.Sprintf("unknown tool %q was dropped", name))
			}
			continue
		}
		if !slices.Contains(tools, mapped) {
			tools = append(tools, mapped)
		}
	}
	return tools, warnings
}

// Plan parses data (the contents of source) and maps its tools.
func Plan(source string, data []byte) (Planned, error) {
	def, err := catalog.ParseAgentFile(data)
	if err != nil {
		return Planned{}, fmt.Errorf("%s: %w", source, err)
	}
	if def.Name == "" {
		return Planned{}, fmt.Errorf("%s: the frontmatter has no name", source)
	}
	p := Planned{Source: source}
	if len(def.Tools) == 0 {
		def.Tools = slices.Clone(defaultTools)
	} else {
		tools, warnings := MapTools(def.Tools)
		p.Warnings = warnings
		if len(tools) == 0 {
			tools = slices.Clone(defaultTools)
			p.Warnings = append(p.Warnings, "no declared tool has an equivalent; using the default set")
		}
		def.Tools = tools
	}
	p.Def = def
	return p, nil
}

// Preview is the text the approval shows for an import.
func (p Planned) Preview() string {
	var b strings.Builder
	fmt.Fprintf(&b, "import agent %s: %s\n", p.Def.Name, p.Def.Description)
	fmt.Fprintf(&b, "tools: %s\n", strings.Join(p.Def.Tools, ", "))
	for _, w := range p.Warnings {
		fmt.Fprintf(&b, "warning: %s\n", w)
	}
	lines := strings.Split(p.Def.Prompt, "\n")
	if len(lines) > previewLines {
		lines = append(lines[:previewLines], "…")
	}
	b.WriteString("prompt:\n" + strings.Join(lines, "\n"))
	return b.String()
}

// Load fetches source (a local path or an http(s) URL) and plans the import.
func Load(ctx context.Context, source string) (Planned, error) {
	return loadWith(ctx, defaultClient, source)
}

func loadWith(ctx context.Context, client *http.Client, source string) (Planned, error) {
	var data []byte
	var err error
	if isURL(source) {
		data, err = fetchURL(ctx, client, source)
	} else {
		data, err = readFile(source)
	}
	if err != nil {
		return Planned{}, err
	}
	return Plan(source, data)
}

func isURL(s string) bool {
	return strings.HasPrefix(s, "http://") || strings.HasPrefix(s, "https://")
}

func readFile(path string) ([]byte, error) {
	if strings.HasPrefix(path, "~/") {
		home, err := os.UserHomeDir()
		if err != nil {
			return nil, err
		}
		path = filepath.Join(home, path[2:])
	}
	info, err := os.Stat(path)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
	if info.Size() > MaxBytes {
		return nil, fmt.Errorf("%s is larger than 256 KB", path)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
	return checkText(path, data)
}

func fetchURL(ctx context.Context, client *http.Client, url string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetch %s: %w", url, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("fetch %s: HTTP %d", url, resp.StatusCode)
	}
	if ct := resp.Header.Get("Content-Type"); ct != "" && !isTextType(ct) {
		return nil, fmt.Errorf("fetch %s: content type %s is not text", url, ct)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, MaxBytes+1))
	if err != nil {
		return nil, fmt.Errorf("fetch %s: %w", url, err)
	}
	if len(data) > MaxBytes {
		return nil, fmt.Errorf("fetch %s: larger than 256 KB", url)
	}
	return checkText(url, data)
}

func isTextType(ct string) bool {
	media, _, err := mime.ParseMediaType(ct)
	if err != nil {
		return false
	}
	return strings.HasPrefix(media, "text/") || media == "application/markdown" || media == "application/x-markdown"
}

// checkText refuses content that is not UTF-8 text.
func checkText(source string, data []byte) ([]byte, error) {
	if !utf8.Valid(data) || bytes.IndexByte(data, 0) >= 0 {
		return nil, fmt.Errorf("%s is not text", source)
	}
	return data, nil
}
