package library

import (
	"strings"
	"testing"
)

func TestParseSourcesGithubAndIndexKinds(t *testing.T) {
	raw := []byte(`
sources:
  - name: "Example catalog"
    kind: github
    repo: example/agents
    ref: main
    path: categories
    license: MIT
    validated_by: "@tinchoram"
  - name: "Index catalog"
    kind: index
    url: https://example.com/index.yaml
`)
	srcs, err := ParseSources(raw)
	if err != nil {
		t.Fatalf("ParseSources: %v", err)
	}
	if len(srcs) != 2 {
		t.Fatalf("got %d sources", len(srcs))
	}
	g := srcs[0]
	if g.Kind != "github" || g.Repo != "example/agents" || g.Ref != "main" || g.Path != "categories" || g.License != "MIT" || g.ValidatedBy != "@tinchoram" {
		t.Fatalf("github source = %+v", g)
	}
	if srcs[1].Kind != "index" || srcs[1].URL != "https://example.com/index.yaml" {
		t.Fatalf("index source = %+v", srcs[1])
	}
}

func TestParseSourcesRejectsBadEntries(t *testing.T) {
	cases := map[string]string{
		"unknown kind":   "sources:\n  - name: x\n    kind: svn\n    url: https://a.example/i.yaml\n",
		"no name":        "sources:\n  - kind: index\n    url: https://a.example/i.yaml\n",
		"github no repo": "sources:\n  - name: x\n    kind: github\n    license: MIT\n",
		"index no url":   "sources:\n  - name: x\n    kind: index\n",
		"bad url":        "sources:\n  - name: x\n    kind: index\n    url: file:///etc/passwd\n",
		"slash in name":  "sources:\n  - name: a/b\n    kind: index\n    url: https://a.example/i.yaml\n",
		"duplicate name": "sources:\n  - name: x\n    kind: index\n    url: https://a.example/i.yaml\n  - name: x\n    kind: index\n    url: https://b.example/i.yaml\n",
	}
	for label, raw := range cases {
		if _, err := ParseSources([]byte(raw)); err == nil {
			t.Errorf("%s: expected an error", label)
		}
	}
}

func TestEmbeddedSourcesSeedOnlyVoltAgent(t *testing.T) {
	srcs, err := EmbeddedSources()
	if err != nil {
		t.Fatalf("embedded sources.yaml: %v", err)
	}
	if len(srcs) != 1 {
		t.Fatalf("seed must hold one source, got %d", len(srcs))
	}
	s := srcs[0]
	if s.Name != "VoltAgent awesome-claude-code-subagents" || s.Kind != "github" || s.Repo != "VoltAgent/awesome-claude-code-subagents" ||
		s.Ref != "main" || s.Path != "categories" || s.License != "MIT" || s.ValidatedBy != "@tinchoram" {
		t.Fatalf("seed entry = %+v", s)
	}
	if !strings.HasPrefix(s.Name, "VoltAgent") {
		t.Fatal("unexpected seed")
	}
}
