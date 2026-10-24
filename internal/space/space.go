// Package space keeps the list of spaces in <home>/spaces.yaml. A space is a
// named working directory with the agents assigned to it. Every mutation is
// saved to disk immediately.
package space

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"time"

	"gopkg.in/yaml.v3"
)

// Space is one working context.
type Space struct {
	Name      string    `yaml:"name"`
	Path      string    `yaml:"path"`
	Agents    []string  `yaml:"agents"`
	CreatedAt time.Time `yaml:"created_at"`
}

type file struct {
	Active string  `yaml:"active"`
	Spaces []Space `yaml:"spaces"`
}

// Store is the persisted list of spaces.
type Store struct {
	path string
	data file
}

var nameRe = regexp.MustCompile(`^[a-z][a-z0-9-]{0,39}$`)

// Open loads path. A missing file is an empty store.
func Open(path string) (*Store, error) {
	s := &Store{path: path}
	raw, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return s, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
	if err := yaml.Unmarshal(raw, &s.data); err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}
	return s, nil
}

// List returns copies of all spaces in insertion order.
func (s *Store) List() []Space {
	out := make([]Space, len(s.data.Spaces))
	for i, sp := range s.data.Spaces {
		out[i] = clone(sp)
	}
	return out
}

// Get returns the named space.
func (s *Store) Get(name string) (Space, bool) {
	i := s.index(name)
	if i < 0 {
		return Space{}, false
	}
	return clone(s.data.Spaces[i]), true
}

// Current returns the active space.
func (s *Store) Current() (Space, bool) {
	return s.Get(s.data.Active)
}

// Add registers a new space. The path must be an existing absolute directory.
func (s *Store) Add(name, path string, now time.Time) error {
	if !nameRe.MatchString(name) {
		return fmt.Errorf("space name %q must be a lowercase slug (letters, digits, dashes)", name)
	}
	if s.index(name) >= 0 {
		return fmt.Errorf("space %q already exists", name)
	}
	if !filepath.IsAbs(path) {
		return fmt.Errorf("space path %q must be absolute", path)
	}
	info, err := os.Stat(path)
	if err != nil {
		return fmt.Errorf("space path: %w", err)
	}
	if !info.IsDir() {
		return fmt.Errorf("space path %q is not a directory", path)
	}
	s.data.Spaces = append(s.data.Spaces, Space{Name: name, Path: path, CreatedAt: now.UTC()})
	return s.save()
}

// Use makes name the active space.
func (s *Store) Use(name string) error {
	if s.index(name) < 0 {
		return fmt.Errorf("no space named %q", name)
	}
	s.data.Active = name
	return s.save()
}

// Assign adds agent to the space's agent list.
func (s *Store) Assign(spaceName, agent string) error {
	i := s.index(spaceName)
	if i < 0 {
		return fmt.Errorf("no space named %q", spaceName)
	}
	if slices.Contains(s.data.Spaces[i].Agents, agent) {
		return fmt.Errorf("agent %q is already assigned to space %q", agent, spaceName)
	}
	s.data.Spaces[i].Agents = append(s.data.Spaces[i].Agents, agent)
	return s.save()
}

// Unassign removes agent from the space's agent list.
func (s *Store) Unassign(spaceName, agent string) error {
	i := s.index(spaceName)
	if i < 0 {
		return fmt.Errorf("no space named %q", spaceName)
	}
	if !slices.Contains(s.data.Spaces[i].Agents, agent) {
		return fmt.Errorf("agent %q is not assigned to space %q", agent, spaceName)
	}
	s.data.Spaces[i].Agents = slices.DeleteFunc(s.data.Spaces[i].Agents, func(a string) bool { return a == agent })
	return s.save()
}

func (s *Store) index(name string) int {
	for i, sp := range s.data.Spaces {
		if sp.Name == name {
			return i
		}
	}
	return -1
}

func (s *Store) save() error {
	if err := os.MkdirAll(filepath.Dir(s.path), 0o755); err != nil {
		return fmt.Errorf("create home directory: %w", err)
	}
	raw, err := yaml.Marshal(s.data)
	if err != nil {
		return fmt.Errorf("encode spaces: %w", err)
	}
	if err := os.WriteFile(s.path, raw, 0o644); err != nil {
		return fmt.Errorf("write %s: %w", s.path, err)
	}
	return nil
}

func clone(sp Space) Space {
	sp.Agents = slices.Clone(sp.Agents)
	return sp
}

// EnsureFirst creates the space at the front of the list when it does not
// exist yet, and reports whether it did. Existing spaces are left alone.
func (s *Store) EnsureFirst(name, path string, now time.Time) (bool, error) {
	if s.index(name) >= 0 {
		return false, nil
	}
	if err := s.Add(name, path, now); err != nil {
		return false, err
	}
	sp := s.data.Spaces[len(s.data.Spaces)-1]
	s.data.Spaces = append([]Space{sp}, s.data.Spaces[:len(s.data.Spaces)-1]...)
	return true, s.save()
}
