package arch

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestEveryDirectRequireIsInThirdPartyNotices fails when a module is added to
// go.mod as a direct dependency without a row in THIRD_PARTY_NOTICES.md. It
// reads both files as text, so it needs no external tool.
func TestEveryDirectRequireIsInThirdPartyNotices(t *testing.T) {
	gomod, err := os.ReadFile(filepath.Join("..", "..", "go.mod"))
	if err != nil {
		t.Fatal(err)
	}
	notices, err := os.ReadFile(filepath.Join("..", "..", "THIRD_PARTY_NOTICES.md"))
	if err != nil {
		t.Fatal(err)
	}
	direct := directRequires(string(gomod))
	if len(direct) == 0 {
		t.Fatal("no direct requires found in go.mod: the parser is broken")
	}
	table := string(notices)
	for _, mod := range direct {
		if !strings.Contains(table, "| "+mod+" |") {
			t.Errorf("direct dependency %s is missing from THIRD_PARTY_NOTICES.md; run `make notices`", mod)
		}
	}
}

// directRequires returns the module paths of the require lines that carry no
// "// indirect" comment, in both single-line and block form.
func directRequires(gomod string) []string {
	var out []string
	inBlock := false
	for _, line := range strings.Split(gomod, "\n") {
		line = strings.TrimSpace(line)
		switch {
		case line == "require (":
			inBlock = true
			continue
		case inBlock && line == ")":
			inBlock = false
			continue
		case !inBlock && strings.HasPrefix(line, "require "):
			line = strings.TrimSpace(strings.TrimPrefix(line, "require "))
		case !inBlock:
			continue
		}
		if line == "" || strings.HasPrefix(line, "//") || strings.Contains(line, "// indirect") {
			continue
		}
		if fields := strings.Fields(line); len(fields) >= 2 {
			out = append(out, fields[0])
		}
	}
	return out
}
