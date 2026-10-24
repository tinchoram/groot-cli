// Package audit appends one JSON line per tool call to a local log file.
package audit

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/tinchoram/groot-cli/internal/tool"
)

const previewLimit = 200

// Record is one audited tool call.
type Record struct {
	Time         time.Time `json:"ts"`
	Session      string    `json:"session"`
	Tool         string    `json:"tool"`
	Risk         string    `json:"risk"`
	Decision     string    `json:"decision"`
	InputPreview string    `json:"input_preview"`
	IsError      bool      `json:"is_error"`
	DurationMS   int64     `json:"duration_ms"`
	// Sandbox is the backend that confined the call (sandbox-exec, bwrap, none,
	// off). Empty for tools that run no process.
	Sandbox string `json:"sandbox,omitempty"`
	// Guard lists the kinds of suspicious text the untrusted-content guard
	// found in the result, such as "override" or "command". Empty when none.
	Guard []string `json:"guard,omitempty"`
}

// Log is an append-only JSONL file. A nil *Log discards records.
type Log struct {
	mu sync.Mutex
	f  *os.File
}

// Open creates the parent directory and opens path for appending.
func Open(path string) (*Log, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, fmt.Errorf("audit dir: %w", err)
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return nil, fmt.Errorf("open audit log: %w", err)
	}
	return &Log{f: f}, nil
}

// Write appends r. The input preview is redacted and cut to 200 characters
// before it is stored, so the log never holds raw secrets.
func (l *Log) Write(r Record) error {
	if l == nil {
		return nil
	}
	if r.Time.IsZero() {
		r.Time = time.Now().UTC()
	}
	r.InputPreview = preview(r.InputPreview)

	line, err := json.Marshal(r)
	if err != nil {
		return err
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.f == nil {
		return fmt.Errorf("audit log is closed")
	}
	_, err = l.f.Write(append(line, '\n'))
	return err
}

// Close flushes and closes the file.
func (l *Log) Close() error {
	if l == nil {
		return nil
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.f == nil {
		return nil
	}
	err := l.f.Close()
	l.f = nil
	return err
}

func preview(s string) string {
	s = tool.Redact(s)
	runes := []rune(s)
	if len(runes) > previewLimit {
		return string(runes[:previewLimit]) + "..."
	}
	return s
}
