package space

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
)

// Remove deletes a space from the list. The directory and its data are not
// touched. Removing the active space clears the selection.
func (s *Store) Remove(name string) error {
	i := s.index(name)
	if i < 0 {
		return fmt.Errorf("no space named %q", name)
	}
	s.data.Spaces = slices.Delete(s.data.Spaces, i, i+1)
	if s.data.Active == name {
		s.data.Active = ""
	}
	return s.save()
}

// Rename gives a space a new name. The name must be a valid slug and unused.
// The selection follows the space.
func (s *Store) Rename(oldName, newName string) error {
	i := s.index(oldName)
	if i < 0 {
		return fmt.Errorf("no space named %q", oldName)
	}
	if !nameRe.MatchString(newName) {
		return fmt.Errorf("space name %q must be a lowercase slug (letters, digits, dashes)", newName)
	}
	if s.index(newName) >= 0 {
		return fmt.Errorf("space %q already exists", newName)
	}
	s.data.Spaces[i].Name = newName
	if s.data.Active == oldName {
		s.data.Active = newName
	}
	return s.save()
}

// SetPath points a space at another directory. The path must be an existing
// absolute directory.
func (s *Store) SetPath(name, path string) error {
	i := s.index(name)
	if i < 0 {
		return fmt.Errorf("no space named %q", name)
	}
	if err := CheckDir(path); err != nil {
		return err
	}
	s.data.Spaces[i].Path = path
	return s.save()
}

// CheckDir reports why path cannot be a space directory: it must be an existing absolute directory.
func CheckDir(path string) error {
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
	return nil
}

// Move takes agent off the from space and puts it on the to space, in one save.
func (s *Store) Move(agent, from, to string) error {
	fi, ti := s.index(from), s.index(to)
	if fi < 0 {
		return fmt.Errorf("no space named %q", from)
	}
	if ti < 0 {
		return fmt.Errorf("no space named %q", to)
	}
	if !slices.Contains(s.data.Spaces[fi].Agents, agent) {
		return fmt.Errorf("agent %q is not assigned to space %q", agent, from)
	}
	if slices.Contains(s.data.Spaces[ti].Agents, agent) {
		return fmt.Errorf("agent %q is already assigned to space %q", agent, to)
	}
	s.data.Spaces[fi].Agents = slices.DeleteFunc(s.data.Spaces[fi].Agents, func(a string) bool { return a == agent })
	s.data.Spaces[ti].Agents = append(s.data.Spaces[ti].Agents, agent)
	return s.save()
}

// RenameAgent renames an agent in every space that refers to it: its
// assignment, the routes of each size, and the delegation manager. One save.
func (s *Store) RenameAgent(oldName, newName string) error {
	for i := range s.data.Spaces {
		sp := &s.data.Spaces[i]
		for j, a := range sp.Agents {
			if a == oldName {
				sp.Agents[j] = newName
			}
		}
		if sp.Flow.Manager == oldName {
			sp.Flow.Manager = newName
		}
		for _, r := range []*SpaceRoute{&sp.Flow.Routes.Small, &sp.Flow.Routes.Medium, &sp.Flow.Routes.Large} {
			if r.Agent == oldName {
				r.Agent = newName
			}
		}
	}
	return s.save()
}

// UnassignEverywhere removes agent from every space and clears the routes that
// name it. It returns the spaces it was removed from, in list order, and saves
// once.
func (s *Store) UnassignEverywhere(agent string) ([]string, error) {
	var removed []string
	for i := range s.data.Spaces {
		sp := &s.data.Spaces[i]
		if slices.Contains(sp.Agents, agent) {
			removed = append(removed, sp.Name)
			sp.Agents = slices.DeleteFunc(sp.Agents, func(a string) bool { return a == agent })
		}
		for _, r := range []*SpaceRoute{&sp.Flow.Routes.Small, &sp.Flow.Routes.Medium, &sp.Flow.Routes.Large} {
			if r.Agent == agent {
				r.Agent = ""
			}
		}
	}
	return removed, s.save()
}
