package library

import (
	"fmt"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/tinchoram/groot-cli/library"
)

// PublicSource is one entry of library/sources.yaml or of the config's
// library.sources list. Kind "github" lists agent files from a repository
// folder; kind "index" reads an index.yaml in the format of library/index.yaml.
type PublicSource struct {
	Name        string `yaml:"name"`
	Kind        string `yaml:"kind"`
	Repo        string `yaml:"repo"`
	Ref         string `yaml:"ref"`
	Path        string `yaml:"path"`
	License     string `yaml:"license"`
	ValidatedBy string `yaml:"validated_by"`
	URL         string `yaml:"url"`
}

// ParseSources reads a sources document and checks every entry.
func ParseSources(raw []byte) ([]PublicSource, error) {
	var doc struct {
		Sources []PublicSource `yaml:"sources"`
	}
	if err := yaml.Unmarshal(raw, &doc); err != nil {
		return nil, fmt.Errorf("sources do not parse: %w", err)
	}
	seen := map[string]bool{}
	for _, s := range doc.Sources {
		if strings.TrimSpace(s.Name) == "" {
			return nil, fmt.Errorf("a source has no name")
		}
		if strings.Contains(s.Name, "/") {
			return nil, fmt.Errorf("source %q: names cannot contain a slash", s.Name)
		}
		if seen[s.Name] {
			return nil, fmt.Errorf("source %q is listed twice", s.Name)
		}
		seen[s.Name] = true
		switch s.Kind {
		case "github":
			if s.Repo == "" || !strings.Contains(s.Repo, "/") {
				return nil, fmt.Errorf("source %q needs repo as owner/name", s.Name)
			}
			if s.Ref == "" {
				return nil, fmt.Errorf("source %q needs a ref", s.Name)
			}
		case "index":
			if !strings.HasPrefix(s.URL, "https://") && !strings.HasPrefix(s.URL, "http://") {
				return nil, fmt.Errorf("source %q needs an http(s) url", s.Name)
			}
		default:
			return nil, fmt.Errorf("source %q has unknown kind %q", s.Name, s.Kind)
		}
	}
	return doc.Sources, nil
}

// EmbeddedSources returns the public catalogs shipped in library/sources.yaml.
func EmbeddedSources() ([]PublicSource, error) {
	raw, err := library.Agents.ReadFile("sources.yaml")
	if err != nil {
		return nil, fmt.Errorf("read embedded sources: %w", err)
	}
	return ParseSources(raw)
}
