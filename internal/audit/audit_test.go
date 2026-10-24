package audit

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestWriteProducesValidJSONLines(t *testing.T) {
	path := filepath.Join(t.TempDir(), "logs", "audit.jsonl")
	l, err := Open(path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer l.Close()

	recs := []Record{
		{Time: time.Date(2026, 10, 24, 10, 0, 0, 0, time.UTC), Session: "s1", Tool: "kubectl", Risk: "read", Decision: "allow", InputPreview: `{"args":["get","pods"]}`, IsError: false, DurationMS: 120},
		{Session: "s1", Tool: "bash", Risk: "exec", Decision: "deny", InputPreview: `{"command":"rm -rf /"}`, IsError: true, DurationMS: 0},
	}
	for _, r := range recs {
		if err := l.Write(r); err != nil {
			t.Fatalf("Write: %v", err)
		}
	}
	if err := l.Close(); err != nil {
		t.Fatal(err)
	}

	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	var lines []map[string]any
	for sc.Scan() {
		var m map[string]any
		if err := json.Unmarshal(sc.Bytes(), &m); err != nil {
			t.Fatalf("line is not valid JSON: %q: %v", sc.Text(), err)
		}
		lines = append(lines, m)
	}
	if len(lines) != 2 {
		t.Fatalf("want 2 records, got %d", len(lines))
	}
	first := lines[0]
	for _, k := range []string{"ts", "session", "tool", "risk", "decision", "input_preview", "is_error", "duration_ms"} {
		if _, ok := first[k]; !ok {
			t.Errorf("record missing field %q: %v", k, first)
		}
	}
	if first["tool"] != "kubectl" || first["decision"] != "allow" || first["duration_ms"] != float64(120) {
		t.Fatalf("unexpected first record: %v", first)
	}
	if lines[1]["is_error"] != true {
		t.Fatalf("second record should be an error: %v", lines[1])
	}
}

func TestInputPreviewIsRedactedAndTruncated(t *testing.T) {
	path := filepath.Join(t.TempDir(), "audit.jsonl")
	l, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()

	secret := `{"command":"curl -H 'Authorization: Bearer abc.def.ghi' --data password=hunter2 ` + strings.Repeat("x", 500) + `"}`
	if err := l.Write(Record{Tool: "bash", InputPreview: secret}); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	s := string(raw)
	if strings.Contains(s, "hunter2") || strings.Contains(s, "abc.def.ghi") {
		t.Fatalf("secrets leaked into audit log: %s", s)
	}
	var rec struct {
		InputPreview string `json:"input_preview"`
	}
	if err := json.Unmarshal(raw, &rec); err != nil {
		t.Fatal(err)
	}
	if len([]rune(rec.InputPreview)) > previewLimit+3 {
		t.Fatalf("preview not truncated: %d runes", len([]rune(rec.InputPreview)))
	}
}

func TestNilLogIsNoop(t *testing.T) {
	var l *Log
	if err := l.Write(Record{Tool: "x"}); err != nil {
		t.Fatalf("nil log write must be a no-op: %v", err)
	}
	if err := l.Close(); err != nil {
		t.Fatalf("nil log close must be a no-op: %v", err)
	}
}
