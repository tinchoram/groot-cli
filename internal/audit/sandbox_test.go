package audit

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRecordCarriesTheSandboxBackend(t *testing.T) {
	path := filepath.Join(t.TempDir(), "audit.jsonl")
	l, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	if err := l.Write(Record{Tool: "bash", Sandbox: "bwrap"}); err != nil {
		t.Fatal(err)
	}
	if err := l.Write(Record{Tool: "kubectl"}); err != nil {
		t.Fatal(err)
	}
	if err := l.Close(); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(string(raw)), "\n")
	if len(lines) != 2 {
		t.Fatalf("want 2 lines, got %d: %s", len(lines), raw)
	}
	if !strings.Contains(lines[0], `"sandbox":"bwrap"`) {
		t.Fatalf("bash record lacks the sandbox backend: %s", lines[0])
	}
	if strings.Contains(lines[1], `"sandbox"`) {
		t.Fatalf("a call outside the sandbox must not carry the field: %s", lines[1])
	}
}
