package config

import (
	"bytes"
	"errors"
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

// SetProviderModel writes provider.model into the configuration file at path. A
// missing file is created from the defaults with the model set. An existing file
// is edited as a YAML tree, so comments and other keys stay as they were. It
// reports whether the file was created.
func SetProviderModel(path, model string) (created bool, err error) {
	raw, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		cfg := Default()
		cfg.Provider.Model = model
		out, err := yaml.Marshal(cfg)
		if err != nil {
			return false, fmt.Errorf("encode %s: %w", path, err)
		}
		return true, os.WriteFile(path, out, 0o644)
	}
	if err != nil {
		return false, fmt.Errorf("read %s: %w", path, err)
	}

	var doc yaml.Node
	if err := yaml.Unmarshal(raw, &doc); err != nil {
		return false, fmt.Errorf("parse %s: %w", path, err)
	}
	if doc.Kind == 0 {
		doc = yaml.Node{Kind: yaml.DocumentNode, Content: []*yaml.Node{{Kind: yaml.MappingNode, Tag: "!!map"}}}
	}
	if len(doc.Content) == 0 || doc.Content[0].Kind != yaml.MappingNode {
		return false, fmt.Errorf("%s: the top level must be a mapping", path)
	}
	provider := mappingChild(doc.Content[0], "provider")
	if provider == nil {
		provider = &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
		appendPair(doc.Content[0], "provider", provider)
	} else if provider.Kind != yaml.MappingNode {
		*provider = yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
	}
	if m := mappingChild(provider, "model"); m != nil {
		m.Kind, m.Tag, m.Value = yaml.ScalarNode, "!!str", model
	} else {
		appendPair(provider, "model", &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: model})
	}

	var buf bytes.Buffer
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)
	if err := enc.Encode(&doc); err != nil {
		return false, fmt.Errorf("encode %s: %w", path, err)
	}
	if err := enc.Close(); err != nil {
		return false, fmt.Errorf("encode %s: %w", path, err)
	}
	info, err := os.Stat(path)
	if err != nil {
		return false, fmt.Errorf("stat %s: %w", path, err)
	}
	return false, os.WriteFile(path, buf.Bytes(), info.Mode().Perm())
}

// mappingChild returns the value node of key in mapping m, or nil.
func mappingChild(m *yaml.Node, key string) *yaml.Node {
	for i := 0; i+1 < len(m.Content); i += 2 {
		if m.Content[i].Value == key {
			return m.Content[i+1]
		}
	}
	return nil
}

func appendPair(m *yaml.Node, key string, value *yaml.Node) {
	m.Content = append(m.Content, &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: key}, value)
}
