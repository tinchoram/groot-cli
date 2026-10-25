package workspace

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/tinchoram/groot-cli/internal/config"
)

func TestMemoryPathFor(t *testing.T) {
	space := filepath.Join(string(filepath.Separator), "work", "space")
	abs := filepath.Join(string(filepath.Separator), "var", "groot", "shared.db")
	cases := []struct {
		name, cfg, want string
	}{
		{"default relative", ".groot/groot.db", filepath.Join(space, ".groot", "groot.db")},
		{"custom relative", "data/memory.db", filepath.Join(space, "data", "memory.db")},
		{"absolute", abs, abs},
		{"empty falls back to default", "", filepath.Join(space, ".groot", "groot.db")},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := memoryPathFor(space, c.cfg); got != c.want {
				t.Fatalf("memoryPathFor(%q, %q) = %q, want %q", space, c.cfg, got, c.want)
			}
		})
	}
}

func TestAuditPathFor(t *testing.T) {
	space := filepath.Join(string(filepath.Separator), "work", "space")
	if got, want := auditPathFor(space, ""), filepath.Join(space, ".groot", "audit.jsonl"); got != want {
		t.Fatalf("default audit path = %q, want %q", got, want)
	}
	if got, want := auditPathFor(space, "logs/audit.jsonl"), filepath.Join(space, "logs", "audit.jsonl"); got != want {
		t.Fatalf("custom audit path = %q, want %q", got, want)
	}
}

func TestStoresForUsesConfiguredPaths(t *testing.T) {
	f := newFixture(t)
	f.ws.opts.Config.Memory = config.MemoryConfig{Path: "data/memory.db", PreambleTokens: 1500, PreambleSummaries: 3}
	f.ws.opts.Config.Audit = config.AuditConfig{Path: "logs/audit.jsonl"}

	if _, err := f.ws.storesFor(f.dir); err != nil {
		t.Fatalf("storesFor: %v", err)
	}
	for _, p := range []string{
		filepath.Join(f.dir, "data", "memory.db"),
		filepath.Join(f.dir, "logs", "audit.jsonl"),
	} {
		if _, err := os.Stat(p); err != nil {
			t.Fatalf("configured store not created at %s: %v", p, err)
		}
	}
	if _, err := os.Stat(filepath.Join(f.dir, ".groot", "groot.db")); err == nil {
		t.Fatal("the hardcoded default database must not be created when memory.path is set")
	}
}
