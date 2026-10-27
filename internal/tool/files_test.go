package tool

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func mustJSON(t *testing.T, v any) string {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestReadFileInsideRoot(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "hello.txt"), []byte("hello world"), 0o644); err != nil {
		t.Fatal(err)
	}
	rf := NewReadFile(root)
	if rf.Risk() != RiskRead {
		t.Fatal("read_file must be RiskRead")
	}
	out, isErr := rf.Execute(context.Background(), mustJSON(t, map[string]string{"path": "hello.txt"}))
	if isErr || out != "hello world" {
		t.Fatalf("got (%q, %v)", out, isErr)
	}
}

func TestReadFileRejectsEscape(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	secret := filepath.Join(outside, "secret.txt")
	_ = os.WriteFile(secret, []byte("nope"), 0o644)
	// A symlink inside the root that points outside must also be refused.
	_ = os.Symlink(outside, filepath.Join(root, "link"))

	rf := NewReadFile(root)
	for _, p := range []string{"../etc/passwd", secret, "link/secret.txt"} {
		out, isErr := rf.Execute(context.Background(), mustJSON(t, map[string]string{"path": p}))
		if !isErr || !strings.Contains(out, "outside") {
			t.Errorf("path %q should be refused, got (%q, %v)", p, out, isErr)
		}
	}
}

// patternBytes returns n bytes that repeat a-z, so windows can be compared exactly.
func patternBytes(n int) []byte {
	b := make([]byte, n)
	for i := range b {
		b[i] = byte('a' + i%26)
	}
	return b
}

// largeDocSize is 68342 bytes: one 64 KiB window plus a 6806-byte tail.
const largeDocSize = 68342

func writeDoc(t *testing.T, root, name string, data []byte) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(root, name), data, 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestReadFileSmallOrExactWindowHasNoHeader(t *testing.T) {
	tests := []struct {
		name string
		size int
	}{
		{"small file", 11},
		{"file exactly one window", 64 * 1024},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			root := t.TempDir()
			data := patternBytes(tt.size)
			writeDoc(t, root, "f.txt", data)
			out, isErr := NewReadFile(root).Execute(context.Background(), mustJSON(t, map[string]any{"path": "f.txt"}))
			if isErr || out != string(data) {
				t.Fatalf("want the whole file with no header, got len=%d isErr=%v", len(out), isErr)
			}
		})
	}
}

func TestReadFileLargeFileHeaderAndContinuation(t *testing.T) {
	root := t.TempDir()
	data := patternBytes(largeDocSize)
	writeDoc(t, root, "doc.md", data)
	rf := NewReadFile(root)

	out, isErr := rf.Execute(context.Background(), mustJSON(t, map[string]any{"path": "doc.md"}))
	wantFirst := "[read_file: showing bytes 0-65535 of 68342; call again with offset=65536 to continue]\n" + string(data[:65536])
	if isErr || out != wantFirst {
		t.Fatalf("first window: isErr=%v len=%d, header or body mismatch", isErr, len(out))
	}

	out, isErr = rf.Execute(context.Background(), mustJSON(t, map[string]any{"path": "doc.md", "offset": 65536}))
	wantRest := "[read_file: showing bytes 65536-68341 of 68342; end of file]\n" + string(data[65536:])
	if isErr || out != wantRest {
		t.Fatalf("second window: isErr=%v len=%d, want the tail with an end-of-file header", isErr, len(out))
	}
	if strings.Contains(out, "call again") {
		t.Fatal("the last window must not ask for another call")
	}
}

func TestReadFileOffsetPastEOFIsAnError(t *testing.T) {
	root := t.TempDir()
	writeDoc(t, root, "doc.md", patternBytes(largeDocSize))
	out, isErr := NewReadFile(root).Execute(context.Background(), mustJSON(t, map[string]any{"path": "doc.md", "offset": 70000}))
	if !isErr || out != "offset 70000 is past the end of the file (68342 bytes)" {
		t.Fatalf("got (%q, %v)", out, isErr)
	}
}

func TestReadFileRejectsNegativeAndZeroWindowValues(t *testing.T) {
	root := t.TempDir()
	writeDoc(t, root, "doc.md", patternBytes(largeDocSize))
	rf := NewReadFile(root)
	tests := []struct {
		name   string
		fields map[string]any
	}{
		{"negative offset", map[string]any{"path": "doc.md", "offset": -1}},
		{"negative limit", map[string]any{"path": "doc.md", "limit": -5}},
		{"zero limit", map[string]any{"path": "doc.md", "limit": 0}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			out, isErr := rf.Execute(context.Background(), mustJSON(t, tt.fields))
			if !isErr {
				t.Fatalf("want an error, got output of len %d: %q", len(out), out[:min(len(out), 60)])
			}
		})
	}
}

func TestReadFileLimitAboveMaxIsClampedAndReported(t *testing.T) {
	root := t.TempDir()
	data := patternBytes(largeDocSize)
	writeDoc(t, root, "doc.md", data)
	out, isErr := NewReadFile(root).Execute(context.Background(), mustJSON(t, map[string]any{"path": "doc.md", "limit": 100000}))
	want := "[read_file: limit 100000 clamped to 65536; showing bytes 0-65535 of 68342; call again with offset=65536 to continue]\n" + string(data[:65536])
	if isErr || out != want {
		t.Fatalf("clamped read: isErr=%v len=%d", isErr, len(out))
	}
}

func TestReadFileHonoursSmallLimit(t *testing.T) {
	root := t.TempDir()
	data := patternBytes(largeDocSize)
	writeDoc(t, root, "doc.md", data)
	out, isErr := NewReadFile(root).Execute(context.Background(), mustJSON(t, map[string]any{"path": "doc.md", "limit": 10}))
	want := "[read_file: showing bytes 0-9 of 68342; call again with offset=10 to continue]\n" + string(data[:10])
	if isErr || out != want {
		t.Fatalf("limit 10: isErr=%v out=%q", isErr, out)
	}
}

// A window that ends inside a multi-byte rune must stop before the rune, and the
// next read must return it whole. The header reports the real range.
func TestReadFileNeverSplitsARune(t *testing.T) {
	runes := []string{"é", "€", "😀"}
	for _, r := range runes {
		for inWindow := 1; inWindow < len(r); inWindow++ {
			t.Run(fmt.Sprintf("%d-byte rune with %d byte(s) in window", len(r), inWindow), func(t *testing.T) {
				root := t.TempDir()
				start := 65536 - inWindow
				tail := strings.Repeat("b", 100)
				data := strings.Repeat("a", start) + r + tail
				writeDoc(t, root, "u.txt", []byte(data))
				rf := NewReadFile(root)

				out, isErr := rf.Execute(context.Background(), mustJSON(t, map[string]any{"path": "u.txt"}))
				wantHeader := fmt.Sprintf("[read_file: showing bytes 0-%d of %d; call again with offset=%d to continue]\n", start-1, len(data), start)
				if isErr || out != wantHeader+strings.Repeat("a", start) {
					t.Fatalf("first window: isErr=%v, body must stop before the split rune", isErr)
				}

				out, isErr = rf.Execute(context.Background(), mustJSON(t, map[string]any{"path": "u.txt", "offset": start}))
				wantRest := fmt.Sprintf("[read_file: showing bytes %d-%d of %d; end of file]\n", start, len(data)-1, len(data)) + r + tail
				if isErr || out != wantRest {
					t.Fatalf("second window: isErr=%v, the rune must come back whole", isErr)
				}
			})
		}
	}
}

func TestReadFileSchemaAdvertisesWindowFields(t *testing.T) {
	def := NewReadFile(t.TempDir()).Definition()
	var schema struct {
		Properties map[string]struct {
			Type string `json:"type"`
		} `json:"properties"`
		Required []string `json:"required"`
	}
	if err := json.Unmarshal(def.InputSchema, &schema); err != nil {
		t.Fatal(err)
	}
	for _, field := range []string{"path", "offset", "limit"} {
		if _, ok := schema.Properties[field]; !ok {
			t.Errorf("schema is missing %q", field)
		}
	}
	if schema.Properties["offset"].Type != "integer" || schema.Properties["limit"].Type != "integer" {
		t.Errorf("offset and limit must be integers: %+v", schema.Properties)
	}
	if !slices.Equal(schema.Required, []string{"path"}) {
		t.Errorf("required = %v, want [path]", schema.Required)
	}
	for _, phrase := range []string{"Use it to read a known path", "Each call returns up to 64 KB", "continue with offset"} {
		if !strings.Contains(def.Description, phrase) {
			t.Errorf("read_file description must say %q: %q", phrase, def.Description)
		}
	}
}

func TestWriteFileCreatesAndPreviewsDiff(t *testing.T) {
	root := t.TempDir()
	wf := NewWriteFile(root)
	if wf.Risk() != RiskWrite {
		t.Fatal("write_file must be RiskWrite")
	}
	_ = os.WriteFile(filepath.Join(root, "conf.yaml"), []byte("replicas: 2\n"), 0o644)

	in := mustJSON(t, map[string]string{"path": "conf.yaml", "content": "replicas: 3\n"})
	preview := wf.Preview(in)
	if !strings.Contains(preview, "-replicas: 2") || !strings.Contains(preview, "+replicas: 3") {
		t.Fatalf("preview is not a diff:\n%s", preview)
	}
	// Preview must not touch the file.
	if b, _ := os.ReadFile(filepath.Join(root, "conf.yaml")); string(b) != "replicas: 2\n" {
		t.Fatal("Preview modified the file")
	}

	out, isErr := wf.Execute(context.Background(), in)
	if isErr {
		t.Fatalf("write failed: %s", out)
	}
	if b, _ := os.ReadFile(filepath.Join(root, "conf.yaml")); string(b) != "replicas: 3\n" {
		t.Fatalf("file not written: %q", b)
	}

	out, isErr = wf.Execute(context.Background(), mustJSON(t, map[string]string{"path": "sub/dir/new.txt", "content": "hi"}))
	if isErr {
		t.Fatalf("nested create failed: %s", out)
	}
	if _, err := os.Stat(filepath.Join(root, "sub", "dir", "new.txt")); err != nil {
		t.Fatalf("nested file missing: %v", err)
	}
}

func TestWriteFileRejectsEscape(t *testing.T) {
	root := t.TempDir()
	out, isErr := NewWriteFile(root).Execute(context.Background(), mustJSON(t, map[string]string{"path": "../evil.sh", "content": "rm -rf"}))
	if !isErr || !strings.Contains(out, "outside") {
		t.Fatalf("escape must be refused, got (%q, %v)", out, isErr)
	}
}
