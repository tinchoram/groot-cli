package ui

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

// Layout is the size and visibility of the three columns. It is kept in
// <GROOT_HOME>/layout.yaml so the screen comes back the way it was left.
// Widths are in cells; a terminal width of 0 means the default share.
type Layout struct {
	SidebarWidth    int  `yaml:"sidebar_width"`
	TerminalWidth   int  `yaml:"terminal_width"`
	SidebarVisible  bool `yaml:"sidebar_visible"`
	TerminalVisible bool `yaml:"terminal_visible"`
}

// DefaultLayout is the layout before anything was changed.
func DefaultLayout() Layout {
	return Layout{SidebarWidth: sidebarWidth, SidebarVisible: true}
}

// LoadLayout reads path. A missing file gives DefaultLayout.
func LoadLayout(path string) (Layout, error) {
	raw, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return DefaultLayout(), nil
	}
	if err != nil {
		return Layout{}, err
	}
	l := DefaultLayout()
	if err := yaml.Unmarshal(raw, &l); err != nil {
		return Layout{}, fmt.Errorf("parse %s: %w", path, err)
	}
	return l, nil
}

// SaveLayout writes l to path, creating its folder.
func SaveLayout(path string, l Layout) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	raw, err := yaml.Marshal(l)
	if err != nil {
		return err
	}
	return os.WriteFile(path, raw, 0o644)
}
