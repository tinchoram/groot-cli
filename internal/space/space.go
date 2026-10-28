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

// Space is one working context. Model and Flow are optional overrides of the
// global configuration; an empty value means "use the global one".
type Space struct {
	Name      string    `yaml:"name"`
	Path      string    `yaml:"path"`
	Agents    []string  `yaml:"agents"`
	CreatedAt time.Time `yaml:"created_at"`
	// Model is the default model of agents in this space that set none.
	Model string    `yaml:"model,omitempty"`
	Flow  SpaceFlow `yaml:"flow,omitempty"`
	// BudgetUSD caps the cumulative spend of all agents in this space. Zero
	// means no budget.
	BudgetUSD float64 `yaml:"budget_usd,omitempty"`
	// Network overrides bash.sandbox.network for this space. Nil means the
	// global value applies; true or false wins over it.
	Network *bool `yaml:"network,omitempty"`
}

// SpaceFlow overrides the ODD flow settings of a space.
type SpaceFlow struct {
	Mode   string      `yaml:"mode,omitempty"` // strict | advisory
	Routes SpaceRoutes `yaml:"routes,omitempty"`
	// Delegation is auto, hint or off; Manager is the agent that tracks the
	// handovers, or "none". Empty values use the global settings.
	Delegation string `yaml:"delegation,omitempty"`
	Manager    string `yaml:"manager,omitempty"`
}

// SpaceRoutes overrides the route of each size.
type SpaceRoutes struct {
	Small  SpaceRoute `yaml:"small,omitempty"`
	Medium SpaceRoute `yaml:"medium,omitempty"`
	Large  SpaceRoute `yaml:"large,omitempty"`
}

// SpaceRoute is the model and agent of one size in a space.
type SpaceRoute struct {
	Model string `yaml:"model,omitempty"`
	Agent string `yaml:"agent,omitempty"`
}

// Sizes are the task sizes that have a route, in order.
var Sizes = []string{"small", "medium", "large"}

// ValidSize reports whether s names a task size.
func ValidSize(s string) bool {
	return slices.Contains(Sizes, s)
}

// route returns the route of a size, or nil for an unknown size.
func (r *SpaceRoutes) route(size string) *SpaceRoute {
	switch size {
	case "small":
		return &r.Small
	case "medium":
		return &r.Medium
	case "large":
		return &r.Large
	}
	return nil
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

// SetFlowMode sets the flow mode of a space. An empty mode clears it, so the
// global mode applies again. The mode is validated where the flow is built.
func (s *Store) SetFlowMode(spaceName, mode string) error {
	i := s.index(spaceName)
	if i < 0 {
		return fmt.Errorf("no space named %q", spaceName)
	}
	s.data.Spaces[i].Flow.Mode = mode
	return s.save()
}

// SetModel sets the space's default model. An empty model clears it.
func (s *Store) SetModel(spaceName, model string) error {
	i := s.index(spaceName)
	if i < 0 {
		return fmt.Errorf("no space named %q", spaceName)
	}
	s.data.Spaces[i].Model = model
	return s.save()
}

// SetNetwork sets the network override of a space. Nil clears it, so the global
// bash.sandbox.network applies again.
func (s *Store) SetNetwork(spaceName string, on *bool) error {
	i := s.index(spaceName)
	if i < 0 {
		return fmt.Errorf("no space named %q", spaceName)
	}
	if on == nil {
		s.data.Spaces[i].Network = nil
	} else {
		v := *on
		s.data.Spaces[i].Network = &v
	}
	return s.save()
}

// SetBudget sets the spend budget of a space. Zero clears it.
func (s *Store) SetBudget(spaceName string, usd float64) error {
	i := s.index(spaceName)
	if i < 0 {
		return fmt.Errorf("no space named %q", spaceName)
	}
	s.data.Spaces[i].BudgetUSD = usd
	return s.save()
}

// SetRoute sets the model and agent a size runs on in a space. An empty model
// or agent clears that field, so the global value applies again.
func (s *Store) SetRoute(spaceName, size, model, agent string) error {
	i := s.index(spaceName)
	if i < 0 {
		return fmt.Errorf("no space named %q", spaceName)
	}
	route := s.data.Spaces[i].Flow.Routes.route(size)
	if route == nil {
		return fmt.Errorf("size %q is invalid (use small, medium or large)", size)
	}
	route.Model, route.Agent = model, agent
	return s.save()
}

// SetDelegation sets the delegation mode and the manager of a space. Empty
// values clear them, so the global settings apply again. The values are
// validated where the flow is built and by the workspace.
func (s *Store) SetDelegation(spaceName, mode, manager string) error {
	i := s.index(spaceName)
	if i < 0 {
		return fmt.Errorf("no space named %q", spaceName)
	}
	s.data.Spaces[i].Flow.Delegation = mode
	s.data.Spaces[i].Flow.Manager = manager
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
	if sp.Network != nil {
		v := *sp.Network
		sp.Network = &v
	}
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
