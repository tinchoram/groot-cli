package tool

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// listOut runs list_dir on input and returns its output lines.
func listOut(t *testing.T, root string, in map[string]any) ([]string, bool) {
	t.Helper()
	out, isErr := NewListDir(root).Execute(context.Background(), mustJSON(t, in))
	if isErr {
		return []string{out}, true
	}
	return strings.Split(out, "\n"), false
}

// writeTree creates the files of a small tree under root. A name ending in / is a directory.
func writeTree(t *testing.T, root string, names ...string) {
	t.Helper()
	for _, n := range names {
		p := filepath.Join(root, n)
		if strings.HasSuffix(n, "/") {
			if err := os.MkdirAll(p, 0o755); err != nil {
				t.Fatal(err)
			}
			continue
		}
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(strings.Repeat("x", len(n))), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

func TestListDirIsReadRiskAndNeedsNoPath(t *testing.T) {
	ld := NewListDir(t.TempDir())
	if ld.Risk() != RiskRead || ld.RiskFor("{}") != RiskRead {
		t.Fatal("list_dir must be RiskRead")
	}
	if got := ld.Definition().Name; got != "list_dir" {
		t.Fatalf("name = %q", got)
	}
	lines, isErr := listOut(t, t.TempDir(), map[string]any{})
	if isErr || len(lines) != 1 || lines[0] != "(empty)" {
		t.Fatalf("empty root and no path = %q, err=%v", lines, isErr)
	}
}

func TestListDirListsDirectoriesFirstWithMarkersAndSizes(t *testing.T) {
	root := t.TempDir()
	writeTree(t, root, "zeta/", "alpha/", "notes.md", "a.txt")
	lines, isErr := listOut(t, root, map[string]any{})
	if isErr {
		t.Fatalf("error: %v", lines)
	}
	want := []string{"d alpha", "d zeta", "f a.txt 5", "f notes.md 8"}
	if strings.Join(lines, "\n") != strings.Join(want, "\n") {
		t.Fatalf("lines = %q, want %q", lines, want)
	}
}

func TestListDirIsOneLevelByDefault(t *testing.T) {
	root := t.TempDir()
	writeTree(t, root, "a/b/c.txt", "a/top.txt")
	lines, _ := listOut(t, root, map[string]any{})
	if strings.Contains(strings.Join(lines, "\n"), "c.txt") || !strings.Contains(strings.Join(lines, "\n"), "d a") {
		t.Fatalf("non-recursive listing = %q", lines)
	}
}

func TestListDirRecursiveRespectsMaxDepth(t *testing.T) {
	root := t.TempDir()
	writeTree(t, root, "a/b/c/deep.txt", "a/b/mid.txt", "a/top.txt")

	lines, _ := listOut(t, root, map[string]any{"recursive": true, "max_depth": 2})
	joined := strings.Join(lines, "\n")
	if !strings.Contains(joined, "d a/b") || !strings.Contains(joined, "f a/top.txt") {
		t.Fatalf("depth 2 lacks depth-2 entries:\n%s", joined)
	}
	if strings.Contains(joined, "mid.txt") || strings.Contains(joined, "a/b/c") {
		t.Fatalf("depth 2 shows depth-3 entries:\n%s", joined)
	}

	// The default depth of a recursive listing is 3: a/b/c is shown, deep.txt (depth 4) is not.
	lines, _ = listOut(t, root, map[string]any{"recursive": true})
	joined = strings.Join(lines, "\n")
	if !strings.Contains(joined, "d a/b/c") || strings.Contains(joined, "deep.txt") {
		t.Fatalf("default depth is not 3:\n%s", joined)
	}

	// A depth above the cap of 8 is clamped, not refused.
	if _, isErr := listOut(t, root, map[string]any{"recursive": true, "max_depth": 99}); isErr {
		t.Fatal("max_depth above 8 must be clamped")
	}
}

func TestListDirHidesDotfilesUnlessAsked(t *testing.T) {
	root := t.TempDir()
	writeTree(t, root, ".env", ".cache/x.txt", "visible.txt")

	lines, _ := listOut(t, root, map[string]any{})
	if joined := strings.Join(lines, "\n"); strings.Contains(joined, ".env") || strings.Contains(joined, ".cache") {
		t.Fatalf("hidden entries listed by default:\n%s", joined)
	}

	lines, _ = listOut(t, root, map[string]any{"include_hidden": true})
	joined := strings.Join(lines, "\n")
	if !strings.Contains(joined, "f .env") || !strings.Contains(joined, "d .cache") {
		t.Fatalf("include_hidden must list dotfiles:\n%s", joined)
	}
}

func TestListDirSkipsTheFixedDenyListAlways(t *testing.T) {
	root := t.TempDir()
	deny := []string{".git", "node_modules", "vendor", ".groot", "target", "dist", "build"}
	for _, d := range deny {
		writeTree(t, root, d+"/inner.txt")
	}
	writeTree(t, root, "src/main.go")

	for _, hidden := range []bool{false, true} {
		lines, isErr := listOut(t, root, map[string]any{"recursive": true, "max_depth": 8, "include_hidden": hidden})
		if isErr {
			t.Fatalf("error: %v", lines)
		}
		joined := strings.Join(lines, "\n")
		for _, d := range deny {
			if strings.Contains(joined, d) {
				t.Fatalf("include_hidden=%v lists denied %s:\n%s", hidden, d, joined)
			}
		}
		if !strings.Contains(joined, "f src/main.go") {
			t.Fatalf("regular entries missing:\n%s", joined)
		}
	}

	// Asking for a denied directory by path is refused, not listed.
	if _, isErr := listOut(t, root, map[string]any{"path": "node_modules"}); !isErr {
		t.Fatal("listing a denied directory must be refused")
	}
}

func TestListDirRefusesEscapes(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	writeTree(t, outside, "secret.txt")
	writeTree(t, root, "ok/file.txt")
	if err := os.Symlink(outside, filepath.Join(root, "out")); err != nil {
		t.Fatal(err)
	}

	for _, p := range []string{"..", "../", "ok/../..", outside, "out", "out/"} {
		lines, isErr := listOut(t, root, map[string]any{"path": p})
		if !isErr {
			t.Errorf("path %q must be refused, got %q", p, lines)
		}
	}

	// A symlink that leaves the root is not listed in its parent either.
	lines, isErr := listOut(t, root, map[string]any{"recursive": true, "max_depth": 8})
	if isErr {
		t.Fatalf("error: %v", lines)
	}
	if joined := strings.Join(lines, "\n"); strings.Contains(joined, "out") || strings.Contains(joined, "secret") {
		t.Fatalf("escaping symlink or its content listed:\n%s", joined)
	}

	// An absolute path inside the root is fine.
	if _, isErr := listOut(t, root, map[string]any{"path": filepath.Join(root, "ok")}); isErr {
		t.Fatal("an absolute path inside the root must be listed")
	}
}

func TestListDirDoesNotDescendIntoSymlinkedDirectories(t *testing.T) {
	root := t.TempDir()
	writeTree(t, root, "real/x.txt")
	if err := os.Symlink(filepath.Join(root, "real"), filepath.Join(root, "alias")); err != nil {
		t.Fatal(err)
	}
	lines, isErr := listOut(t, root, map[string]any{"recursive": true, "max_depth": 8})
	if isErr {
		t.Fatalf("error: %v", lines)
	}
	joined := strings.Join(lines, "\n")
	if strings.Contains(joined, "alias/x.txt") {
		t.Fatalf("a symlinked directory was descended:\n%s", joined)
	}
	if !strings.Contains(joined, "real/x.txt") {
		t.Fatalf("the real directory is missing:\n%s", joined)
	}
}

func TestListDirCapsAtFiveHundredEntries(t *testing.T) {
	root := t.TempDir()
	for i := 0; i < 520; i++ {
		if err := os.WriteFile(filepath.Join(root, fmt.Sprintf("f%04d.txt", i)), nil, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	lines, isErr := listOut(t, root, map[string]any{})
	if isErr {
		t.Fatalf("error: %v", lines)
	}
	if len(lines) != 501 || lines[500] != "… 20 more" {
		t.Fatalf("got %d lines, last %q; want 500 entries and \"… 20 more\"", len(lines), lines[len(lines)-1])
	}
}

func TestListDirStopsWhenTheContextIsCancelled(t *testing.T) {
	root := t.TempDir()
	writeTree(t, root, "a.txt")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	out, isErr := NewListDir(root).Execute(ctx, mustJSON(t, map[string]any{}))
	if !isErr || !strings.Contains(out, "context canceled") {
		t.Fatalf("cancelled listing = (%q, %v), want an error naming the cancellation", out, isErr)
	}
}

func TestListDirRefusesAFile(t *testing.T) {
	root := t.TempDir()
	writeTree(t, root, "a.txt")
	if _, isErr := listOut(t, root, map[string]any{"path": "a.txt"}); !isErr {
		t.Fatal("a file is not a directory: list_dir must refuse it")
	}
}
