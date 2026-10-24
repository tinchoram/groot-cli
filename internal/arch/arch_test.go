// Package arch enforces the import boundaries of the hexagonal layout: the
// core packages depend on ports and the standard library, never on vendor
// SDKs or the terminal UI.
package arch

import (
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

const modulePath = "github.com/tinchoram/groot-cli/"

// corePackages are the loop, the ports and the builtin tools.
var corePackages = []string{"agent", "api", "gate", "compact", "tool"}

// forbiddenPrefixes are third-party SDK and UI packages no core package may import.
var forbiddenPrefixes = []string{
	"github.com/anthropics/",
	"github.com/openai/",
	"github.com/sashabaranov/",
	"github.com/charmbracelet/",
	"modernc.org/",
	"gopkg.in/yaml",
}

// forbiddenInternal are internal packages a core package must not reach into.
var forbiddenInternal = []string{
	modulePath + "internal/ui",
	modulePath + "internal/provider/", // adapters only; the port is allowed
	modulePath + "cmd/",
}

// execAllowed lists the only file in core packages allowed to run processes.
var execAllowed = map[string]bool{
	filepath.Join("tool", "exec.go"): true,
}

func importsOf(t *testing.T, path string) []string {
	t.Helper()
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, path, nil, parser.ImportsOnly)
	if err != nil {
		t.Fatalf("parse %s: %v", path, err)
	}
	var out []string
	for _, imp := range f.Imports {
		p, err := strconv.Unquote(imp.Path.Value)
		if err != nil {
			t.Fatalf("unquote import in %s: %v", path, err)
		}
		out = append(out, p)
	}
	return out
}

// goFiles returns the non-test Go files of a package directory.
func goFiles(t *testing.T, dir string) []string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read %s: %v", dir, err)
	}
	var files []string
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		files = append(files, filepath.Join(dir, name))
	}
	if len(files) == 0 {
		t.Fatalf("no Go files in %s", dir)
	}
	return files
}

func TestCorePackagesDoNotImportSDKsOrUI(t *testing.T) {
	for _, pkg := range corePackages {
		for _, file := range goFiles(t, filepath.Join("..", pkg)) {
			for _, imp := range importsOf(t, file) {
				for _, bad := range forbiddenPrefixes {
					if strings.HasPrefix(imp, bad) || strings.Contains(imp, "openai") {
						t.Errorf("%s imports forbidden package %s", file, imp)
					}
				}
				for _, bad := range forbiddenInternal {
					if strings.HasPrefix(imp, bad) {
						t.Errorf("%s imports %s, which the core must not depend on", file, imp)
					}
				}
			}
		}
	}
}

func TestOnlyToolExecRunsProcesses(t *testing.T) {
	for _, pkg := range corePackages {
		for _, file := range goFiles(t, filepath.Join("..", pkg)) {
			key := filepath.Join(pkg, filepath.Base(file))
			if execAllowed[key] {
				continue
			}
			for _, imp := range importsOf(t, file) {
				if imp == "os/exec" {
					t.Errorf("%s imports os/exec; only tool/exec.go may run processes", file)
				}
			}
		}
	}
}

func TestAPIHasNoInternalDependencies(t *testing.T) {
	for _, file := range goFiles(t, filepath.Join("..", "api")) {
		for _, imp := range importsOf(t, file) {
			if strings.HasPrefix(imp, modulePath) {
				t.Errorf("%s imports %s; api must stay dependency-free", file, imp)
			}
		}
	}
}

// TestUIReachesTerminalOnlyThroughTheTermPackage keeps the PTY and the
// emulator behind internal/term: the UI sees the Terminal interface only.
func TestUIReachesTerminalOnlyThroughTheTermPackage(t *testing.T) {
	banned := []string{"github.com/creack/pty", "github.com/charmbracelet/x/vt", "github.com/charmbracelet/ultraviolet"}
	for _, file := range goFiles(t, filepath.Join("..", "ui")) {
		for _, imp := range importsOf(t, file) {
			for _, bad := range banned {
				if strings.HasPrefix(imp, bad) {
					t.Errorf("%s imports %s; use internal/term instead", file, imp)
				}
			}
		}
	}
}
