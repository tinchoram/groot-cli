package tool

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
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

func TestReadFileTruncatesAt64KB(t *testing.T) {
	root := t.TempDir()
	big := strings.Repeat("x", 100*1024)
	_ = os.WriteFile(filepath.Join(root, "big.txt"), []byte(big), 0o644)
	out, isErr := NewReadFile(root).Execute(context.Background(), mustJSON(t, map[string]string{"path": "big.txt"}))
	if isErr {
		t.Fatalf("unexpected error: %s", out)
	}
	if len(out) > 64*1024+200 || !strings.Contains(out, "truncated") {
		t.Fatalf("expected truncation note, len=%d", len(out))
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
