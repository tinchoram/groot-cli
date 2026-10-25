package config

import (
	"fmt"
	"reflect"
	"strings"
	"sync"

	"gopkg.in/yaml.v3"
)

// projectAllowed is the complete list of configuration a project file (./groot.yaml)
// may set. An entry covers its whole subtree: "compaction" allows every
// compaction key. Everything else is refused: a key that exists in the schema
// fails with "can only be set in the global config", and a key that does not
// exist fails with "unknown key". Keep this table the single place that opens
// a key to projects; a new setting is global-only until it is added here.
//
// Excluded on purpose, even though they look harmless: work.always_ask (a
// project could drop the approval guards), gate.policy (readonly to auto),
// provider.name, system_prompt, library.sources, kubectl.namespaces and
// models.context_window (a list or map in a project replaces the global one
// whole), feedback, and every security key (audit.sinks, bash.sandbox,
// fetch, guard, updates, user, mcp.servers).
var projectAllowed = []string{
	"max_turns",
	"provider.model",
	"memory.path",
	"memory.preamble_tokens",
	"memory.preamble_summaries",
	"audit.path",
	"compaction",
	"flow",
	"work.max_turns",
	"work.max_auto_turns",
	"work.max_usd",
	"bash.timeout",
	"bash.max_timeout",
	"mcp.enable",
	"ui",
}

// configPaths is every key path of the Config schema, read from its yaml tags.
// Map-valued keys are recorded in configOpenMaps: any key under them is a
// legitimate entry, not an unknown one.
var (
	configPaths    map[string]bool
	configOpenMaps []string
	schemaOnce     sync.Once
)

func schemaKeys() {
	configPaths = map[string]bool{}
	collectSchemaKeys(reflect.TypeOf(Config{}), "")
}

func collectSchemaKeys(t reflect.Type, prefix string) {
	if t.Kind() != reflect.Struct {
		return
	}
	for i := 0; i < t.NumField(); i++ {
		f := t.Field(i)
		name := strings.Split(f.Tag.Get("yaml"), ",")[0]
		if name == "" || name == "-" {
			continue
		}
		p := joinKey(prefix, name)
		configPaths[p] = true
		if f.Type.Kind() == reflect.Map {
			configOpenMaps = append(configOpenMaps, p)
		}
		collectSchemaKeys(f.Type, p)
	}
}

func joinKey(prefix, key string) string {
	if prefix == "" {
		return key
	}
	return prefix + "." + key
}

func isSchemaKey(p string) bool {
	schemaOnce.Do(schemaKeys)
	if configPaths[p] {
		return true
	}
	for _, m := range configOpenMaps {
		if strings.HasPrefix(p, m+".") {
			return true
		}
	}
	return false
}

// isProjectAllowed reports whether p is an allowed key or inside an allowed subtree.
func isProjectAllowed(p string) bool {
	for _, a := range projectAllowed {
		if p == a || strings.HasPrefix(p, a+".") {
			return true
		}
	}
	return false
}

// isAllowedAncestor reports whether an allowed key lies below p, so p is a
// section a project may partly set.
func isAllowedAncestor(p string) bool {
	for _, a := range projectAllowed {
		if strings.HasPrefix(a, p+".") {
			return true
		}
	}
	return false
}

// checkProjectFile applies the allow-list to a parsed project document. It runs
// before any expansion, so a project value keeps no environment reference.
func checkProjectFile(path string, doc *yaml.Node) error {
	root := doc
	if root.Kind == yaml.DocumentNode {
		if len(root.Content) == 0 {
			return nil
		}
		root = root.Content[0]
	}
	return checkProjectMapping(path, "", root)
}

func checkProjectMapping(path, prefix string, n *yaml.Node) error {
	if n.Kind != yaml.MappingNode {
		return nil
	}
	for i := 0; i+1 < len(n.Content); i += 2 {
		p := joinKey(prefix, n.Content[i].Value)
		val := n.Content[i+1]
		switch {
		case !isSchemaKey(p):
			return fmt.Errorf("%s: unknown key %s in a project config", path, p)
		case isProjectAllowed(p) || (isAllowedAncestor(p) && val.Kind == yaml.MappingNode):
			if val.Kind == yaml.MappingNode {
				if err := checkProjectMapping(path, p, val); err != nil {
					return err
				}
				continue
			}
			if containsEnvRef(val) {
				return fmt.Errorf("%s: %s: project configs cannot reference environment variables", path, p)
			}
		default:
			return fmt.Errorf("%s: %s can only be set in the global config (~/.groot/groot.yaml): a project file cannot change security settings", path, firstLeafKey(p, val))
		}
	}
	return nil
}

// firstLeafKey names the most specific key a refused section starts with, so
// bash: {sandbox: {mode: off}} is reported as bash.sandbox.mode.
func firstLeafKey(p string, n *yaml.Node) string {
	if n.Kind != yaml.MappingNode || len(n.Content) == 0 {
		return p
	}
	return firstLeafKey(joinKey(p, n.Content[0].Value), n.Content[1])
}

// containsEnvRef reports whether any scalar under n holds a ${...} reference.
func containsEnvRef(n *yaml.Node) bool {
	if n.Kind == yaml.ScalarNode && strings.Contains(n.Value, "${") {
		return true
	}
	for _, c := range n.Content {
		if containsEnvRef(c) {
			return true
		}
	}
	return false
}
