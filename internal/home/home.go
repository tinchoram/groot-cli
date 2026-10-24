// Package home resolves the directory that holds groot's global state:
// spaces.yaml and the agent catalog. GROOT_HOME overrides the default
// ~/.groot, which tests use to run against a temporary directory.
package home

import (
	"fmt"
	"path/filepath"
)

// Resolve returns the groot home directory. getenv and userHome are injected
// so the function is testable without touching the real environment.
func Resolve(getenv func(string) string, userHome func() (string, error)) (string, error) {
	if v := getenv("GROOT_HOME"); v != "" {
		abs, err := filepath.Abs(v)
		if err != nil {
			return "", fmt.Errorf("GROOT_HOME: %w", err)
		}
		return abs, nil
	}
	h, err := userHome()
	if err != nil {
		return "", fmt.Errorf("resolve home directory: %w", err)
	}
	return filepath.Join(h, ".groot"), nil
}
