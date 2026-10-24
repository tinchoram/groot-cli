package agentimport

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

const claudeStyle = `---
name: code-scout
description: "Finds the code that matters for a task"
tools: Read, Grep, Glob, Bash, Edit
model: sonnet
---
You scout the codebase.
`

func TestMapToolsTable(t *testing.T) {
	cases := []struct {
		in       []string
		want     []string
		warnings int
	}{
		{[]string{"Read"}, []string{"read_file"}, 0},
		{[]string{"Write", "Edit", "MultiEdit"}, []string{"write_file"}, 0},
		{[]string{"Bash"}, []string{"bash"}, 0},
		{[]string{"Grep", "Glob"}, nil, 2},
		{[]string{"WebFetch"}, nil, 1},
		{[]string{"kubectl", "recall"}, []string{"kubectl", "recall"}, 0},
		{[]string{"Read", "Read", "Bash"}, []string{"read_file", "bash"}, 0},
	}
	for _, c := range cases {
		got, warnings := MapTools(c.in)
		if strings.Join(got, ",") != strings.Join(c.want, ",") {
			t.Errorf("MapTools(%v) = %v, want %v", c.in, got, c.want)
		}
		if len(warnings) != c.warnings {
			t.Errorf("MapTools(%v) warnings = %v", c.in, warnings)
		}
	}
}

func TestPlanMapsToolsAndKeepsWarnings(t *testing.T) {
	p, err := Plan("local.md", []byte(claudeStyle))
	if err != nil {
		t.Fatalf("Plan: %v", err)
	}
	if p.Def.Name != "code-scout" || p.Def.Description != "Finds the code that matters for a task" {
		t.Fatalf("def = %+v", p.Def)
	}
	if strings.Join(p.Def.Tools, ",") != "read_file,bash,write_file" {
		t.Fatalf("tools = %v", p.Def.Tools)
	}
	if p.Def.Model != "claude-sonnet-5-5" {
		t.Fatalf("model alias not resolved: %q", p.Def.Model)
	}
	if len(p.Warnings) != 2 || !strings.Contains(strings.Join(p.Warnings, " "), "Grep") {
		t.Fatalf("warnings = %v", p.Warnings)
	}
}

func TestPlanUsesDefaultToolsWhenNoneDeclared(t *testing.T) {
	p, err := Plan("x.md", []byte("---\nname: plain\ndescription: d\n---\nBody.\n"))
	if err != nil {
		t.Fatal(err)
	}
	want := "read_file,write_file,bash,recall,remember"
	if strings.Join(p.Def.Tools, ",") != want || len(p.Warnings) != 0 {
		t.Fatalf("default tools = %v warnings=%v", p.Def.Tools, p.Warnings)
	}
}

func TestPlanFallsBackWhenEveryToolIsDropped(t *testing.T) {
	p, err := Plan("x.md", []byte("---\nname: searcher\ntools: [Grep]\n---\nBody.\n"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(p.Def.Tools, ",") != "read_file,write_file,bash,recall,remember" {
		t.Fatalf("tools = %v", p.Def.Tools)
	}
	if !strings.Contains(strings.Join(p.Warnings, " "), "default set") {
		t.Fatalf("fallback must be reported: %v", p.Warnings)
	}
}

func TestPlanRejectsMissingNameAndBadFrontmatter(t *testing.T) {
	if _, err := Plan("x.md", []byte("---\ndescription: no name\n---\n")); err == nil {
		t.Fatal("a file without a name must be refused")
	}
	if _, err := Plan("x.md", []byte("no frontmatter")); err == nil {
		t.Fatal("a file without frontmatter must be refused")
	}
}

func TestPreviewShowsNameToolsWarningsAndFirstLines(t *testing.T) {
	prompt := "L1\nL2\nL3\nL4\nL5\nL6 hidden"
	p, err := Plan("x.md", []byte("---\nname: n\ntools: [Read, Grep]\n---\n"+prompt+"\n"))
	if err != nil {
		t.Fatal(err)
	}
	out := p.Preview()
	for _, want := range []string{"n", "read_file", "Grep", "L5"} {
		if !strings.Contains(out, want) {
			t.Errorf("preview missing %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "L6") {
		t.Errorf("preview should show only five prompt lines:\n%s", out)
	}
}

func TestLoadFromLocalFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "scout.md")
	if err := os.WriteFile(path, []byte(claudeStyle), 0o644); err != nil {
		t.Fatal(err)
	}
	p, err := Load(context.Background(), path)
	if err != nil || p.Def.Name != "code-scout" {
		t.Fatalf("Load local = %+v (%v)", p.Def, err)
	}
}

func TestLoadRefusesLargeAndBinaryLocalFiles(t *testing.T) {
	dir := t.TempDir()
	big := filepath.Join(dir, "big.md")
	_ = os.WriteFile(big, []byte(strings.Repeat("a", MaxBytes+1)), 0o644)
	if _, err := Load(context.Background(), big); err == nil || !strings.Contains(err.Error(), "256 KB") {
		t.Fatalf("large file must be refused: %v", err)
	}
	bin := filepath.Join(dir, "bin.md")
	_ = os.WriteFile(bin, []byte("---\nname: x\x00\n---\n"), 0o644)
	if _, err := Load(context.Background(), bin); err == nil {
		t.Fatal("binary content must be refused")
	}
}

func fastClient() *http.Client { return &http.Client{Timeout: 200 * time.Millisecond} }

func TestLoadFromURL(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/markdown; charset=utf-8")
		_, _ = w.Write([]byte(claudeStyle))
	}))
	defer srv.Close()
	p, err := loadWith(context.Background(), fastClient(), srv.URL+"/scout.md")
	if err != nil || p.Def.Name != "code-scout" {
		t.Fatalf("URL load = %+v (%v)", p.Def, err)
	}
}

func TestURLErrorsAreReported(t *testing.T) {
	cases := map[string]http.HandlerFunc{
		"not found": func(w http.ResponseWriter, _ *http.Request) { http.NotFound(w, nil) },
		"image": func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "image/png")
			_, _ = w.Write([]byte(claudeStyle))
		},
		"too large": func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "text/plain")
			_, _ = w.Write([]byte(strings.Repeat("a", MaxBytes+10)))
		},
		"slow": func(w http.ResponseWriter, r *http.Request) {
			select {
			case <-time.After(2 * time.Second):
			case <-r.Context().Done():
			}
		},
	}
	for label, h := range cases {
		srv := httptest.NewServer(h)
		_, err := loadWith(context.Background(), fastClient(), srv.URL+"/a.md")
		srv.Close()
		if err == nil {
			t.Errorf("%s: expected an error", label)
		}
	}
}

func TestDefaultClientHasTenSecondTimeout(t *testing.T) {
	if defaultClient.Timeout != 10*time.Second {
		t.Fatalf("timeout = %v", defaultClient.Timeout)
	}
}
