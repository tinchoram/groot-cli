package audit

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

// FileSink appends JSON lines to another file, such as a central log directory.
type FileSink struct {
	mu sync.Mutex
	f  *os.File
}

// NewFileSink opens path for appending. A leading "~/" is the user's home.
func NewFileSink(path string) (*FileSink, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return nil, errors.New("audit sink file: path is empty")
	}
	if strings.HasPrefix(path, "~/") {
		home, err := os.UserHomeDir()
		if err != nil {
			return nil, fmt.Errorf("audit sink file: home directory: %w", err)
		}
		path = filepath.Join(home, path[2:])
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, fmt.Errorf("audit sink file: directory: %w", err)
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return nil, fmt.Errorf("audit sink file: open: %w", err)
	}
	return &FileSink{f: f}, nil
}

// Write appends one record as a JSON line.
func (s *FileSink) Write(_ context.Context, r Record) error {
	line, err := encodeLine(r)
	if err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.f == nil {
		return errors.New("audit sink file: closed")
	}
	if _, err := s.f.Write(line); err != nil {
		return fmt.Errorf("audit sink file: write: %w", err)
	}
	return nil
}

// Close closes the file.
func (s *FileSink) Close(context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.f == nil {
		return nil
	}
	err := s.f.Close()
	s.f = nil
	return err
}
