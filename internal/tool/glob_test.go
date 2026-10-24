package tool

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// globOut runs glob on input and returns its output lines.
func globOut(t *testing.T, root string, in map[string]any) ([]string, bool) {
	t.Helper()
	out, isErr := NewGlob(root).Execute(context.Background(), mustJSON(t, in))
	if isErr {
		return []string{out}, true
	}
	if out == "" || out == "(no matches)" {
		return nil, false
	}
	return strings.Split(out, "\n"), false
}

func TestGlobIsReadRisk(t *testing.T) {
	g := NewGlob(t.TempDir())
	if g.Risk() != RiskRead || g.RiskFor("{}") != RiskRead {
		t.Fatal("glob must be RiskRead")
	}
	if got := g.Definition().Name; got != "glob" {
		t.Fatalf("name = %q", got)
	}
}

func TestGlobMatchesDoubleStarAndSingleSegments(t *testing.T) {
	root := t.TempDir()
	writeTree(t, root, "a.md", "docs/b.md", "docs/deep/c.md", "docs/x.txt", "src/main.go")

	cases := map[string][]string{
		"**/*.md":        {"a.md", "docs/b.md", "docs/deep/c.md"},
		"*.md":           {"a.md"},
		"docs/*.md":      {"docs/b.md"},
		"docs/**/*.md":   {"docs/b.md", "docs/deep/c.md"},
		"**/deep/*.md":   {"docs/deep/c.md"},
		"src/**":         {"src/main.go"},
		"**/main.go":     {"src/main.go"},
		"nothing/**/*.x": nil,
	}
	for pattern, want := range cases {
		got, isErr := globOut(t, root, map[string]any{"pattern": pattern})
		if isErr {
			t.Fatalf("pattern %q: error %v", pattern, got)
		}
		if strings.Join(got, "|") != strings.Join(want, "|") {
			t.Errorf("pattern %q = %v, want %v", pattern, got, want)
		}
	}
}

func TestGlobPathScopesTheWalkAndMatchesRelativeToIt(t *testing.T) {
	root := t.TempDir()
	writeTree(t, root, "a.md", "docs/b.md", "docs/deep/c.md")
	got, isErr := globOut(t, root, map[string]any{"pattern": "**/*.md", "path": "docs"})
	if isErr {
		t.Fatalf("error: %v", got)
	}
	if strings.Join(got, "|") != "docs/b.md|docs/deep/c.md" {
		t.Fatalf("scoped glob = %v", got)
	}
}

func TestGlobSkipsDenyListAndHiddenFiles(t *testing.T) {
	root := t.TempDir()
	writeTree(t, root, "node_modules/x.md", ".git/y.md", ".secret/z.md", ".hidden.md", "keep.md")

	got, _ := globOut(t, root, map[string]any{"pattern": "**/*.md"})
	if strings.Join(got, "|") != "keep.md" {
		t.Fatalf("default glob = %v, want only keep.md", got)
	}

	// A pattern that starts with a dot segment asks for dotfiles on purpose.
	got, _ = globOut(t, root, map[string]any{"pattern": ".hidden.md"})
	if strings.Join(got, "|") != ".hidden.md" {
		t.Fatalf("dot pattern = %v, want .hidden.md", got)
	}

	// The deny list holds even when the pattern names it.
	got, _ = globOut(t, root, map[string]any{"pattern": ".git/*.md"})
	if len(got) != 0 {
		t.Fatalf("denied directory matched: %v", got)
	}
}

func TestGlobRefusesEscapes(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	writeTree(t, outside, "secret.md")
	writeTree(t, root, "ok.md")
	if err := os.Symlink(outside, filepath.Join(root, "out")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(outside, "secret.md"), filepath.Join(root, "linkfile.md")); err != nil {
		t.Fatal(err)
	}

	for _, in := range []map[string]any{
		{"pattern": "*.md", "path": ".."},
		{"pattern": "*.md", "path": outside},
		{"pattern": "../*.md"},
		{"pattern": "/etc/*.md"},
	} {
		got, isErr := globOut(t, root, in)
		if !isErr {
			t.Errorf("input %v must be refused, got %v", in, got)
		}
	}

	// Walking the root must not follow the symlinked directory or return the escaping file.
	got, isErr := globOut(t, root, map[string]any{"pattern": "**/*.md"})
	if isErr {
		t.Fatalf("error: %v", got)
	}
	if joined := strings.Join(got, "|"); strings.Contains(joined, "secret") || strings.Contains(joined, "linkfile") {
		t.Fatalf("escaping paths returned: %s", joined)
	}
	if strings.Join(got, "|") != "ok.md" {
		t.Fatalf("root walk = %v, want ok.md", got)
	}
}

func TestGlobCapsResults(t *testing.T) {
	root := t.TempDir()
	for i := 0; i < 520; i++ {
		if err := os.WriteFile(filepath.Join(root, fmt.Sprintf("f%04d.md", i)), nil, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	got, isErr := globOut(t, root, map[string]any{"pattern": "*.md"})
	if isErr {
		t.Fatalf("error: %v", got)
	}
	if len(got) != 501 || !strings.HasPrefix(got[500], "… result cap") {
		t.Fatalf("got %d lines, last %q; want 500 results and a cap note", len(got), got[len(got)-1])
	}

	got, _ = globOut(t, root, map[string]any{"pattern": "*.md", "max_results": 3})
	if len(got) != 4 || !strings.HasPrefix(got[3], "…") {
		t.Fatalf("max_results 3 = %v", got)
	}
}

func TestGlobRejectsABadPattern(t *testing.T) {
	root := t.TempDir()
	writeTree(t, root, "a.md")
	if _, isErr := globOut(t, root, map[string]any{"pattern": "[*.md"}); !isErr {
		t.Fatal("a malformed pattern must be an error")
	}
	if _, isErr := globOut(t, root, map[string]any{}); !isErr {
		t.Fatal("a missing pattern must be an error")
	}
}

func TestGlobStopsWhenTheContextIsCancelled(t *testing.T) {
	root := t.TempDir()
	writeTree(t, root, "a.md")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	out, isErr := NewGlob(root).Execute(ctx, mustJSON(t, map[string]any{"pattern": "**/*.md"}))
	if !isErr || !strings.Contains(out, "context canceled") {
		t.Fatalf("cancelled glob = (%q, %v), want an error naming the cancellation", out, isErr)
	}
}

func TestMatchPathSegments(t *testing.T) {
	cases := []struct {
		pattern, name string
		want          bool
	}{
		{"**/*.md", "a.md", true},
		{"**/*.md", "a/b/c.md", true},
		{"**/*.md", "a/b/c.txt", false},
		{"a/**", "a", true},
		{"a/**", "a/b/c", true},
		{"a/**/b", "a/b", true},
		{"a/**/b", "a/x/y/b", true},
		{"*.md", "a/b.md", false},
		{"a/*/c", "a/b/c", true},
		{"a?c", "abc", true},
	}
	for _, c := range cases {
		if got := matchPath(c.pattern, c.name); got != c.want {
			t.Errorf("matchPath(%q, %q) = %v, want %v", c.pattern, c.name, got, c.want)
		}
	}
}
