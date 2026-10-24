// Package config loads groot.yaml, applies defaults and expands environment
// variables, and builds the system prompt from project context.
package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"gopkg.in/yaml.v3"
)

// DefaultSystemPrompt is the base instruction used when system_prompt is unset.
const DefaultSystemPrompt = `You are Groot, a terminal assistant for Developers and Platform engineering.
Work methodically: inspect before you change anything, explain what you are about to do,
and prefer read-only commands (kubectl get/describe/logs, reading files) to gather facts.
Every write or command is shown to the user for approval. Keep answers short and concrete,
and never ask for or print secrets.`

// Config is the full groot.yaml schema.
type Config struct {
	Provider     ProviderConfig   `yaml:"provider"`
	MaxTurns     int              `yaml:"max_turns"`
	Compaction   CompactionConfig `yaml:"compaction"`
	Memory       PathConfig       `yaml:"memory"`
	Audit        PathConfig       `yaml:"audit"`
	Gate         GateConfig       `yaml:"gate"`
	Kubectl      KubectlConfig    `yaml:"kubectl"`
	Library      LibraryConfig    `yaml:"library"`
	SystemPrompt string           `yaml:"system_prompt"`
	Flow         FlowConfig       `yaml:"flow"`
	Work         WorkConfig       `yaml:"work"`
	Fetch        FetchConfig      `yaml:"fetch"`
	Feedback     FeedbackConfig   `yaml:"feedback"`
}

// FeedbackConfig shapes how debug answers reports. By default it records them
// silently; confirm makes it reply to every reporter.
type FeedbackConfig struct {
	Confirm bool `yaml:"confirm"`
}

// FetchConfig constrains the fetch_url tool. Allow and Deny are domain lists
// matched by suffix; Deny wins and an empty Allow means any domain. AllowPrivate
// lets the tool reach loopback and private network addresses.
type FetchConfig struct {
	Allow        []string `yaml:"allow"`
	Deny         []string `yaml:"deny"`
	AllowPrivate bool     `yaml:"allow_private"`
}

// FlowConfig is the ODD task flow: strict denies writes that skip it, advisory
// only warns. Routes name the model a size runs on, applied when its task opens.
type FlowConfig struct {
	Mode   string     `yaml:"mode"` // strict | advisory
	Routes FlowRoutes `yaml:"routes"`
}

// FlowRoutes holds the route of each size.
type FlowRoutes struct {
	Small  FlowRoute `yaml:"small"`
	Medium FlowRoute `yaml:"medium"`
	Large  FlowRoute `yaml:"large"`
}

// FlowRoute is the model and agent of one size.
type FlowRoute struct {
	Model string `yaml:"model"`
	Agent string `yaml:"agent"`
}

// LibraryConfig names the remote agent collections to list alongside the
// builtin and local ones.
type LibraryConfig struct {
	Sources []LibrarySource `yaml:"sources"`
}

// LibrarySource is one remote index: a display name and the URL of its index.yaml.
type LibrarySource struct {
	Name string `yaml:"name"`
	URL  string `yaml:"url"`
}

// ProviderConfig selects the model backend.
type ProviderConfig struct {
	Name  string `yaml:"name"`
	Model string `yaml:"model"`
}

// CompactionConfig selects the history compaction strategy.
type CompactionConfig struct {
	Strategy  string `yaml:"strategy"` // none | window | summarize
	Keep      int    `yaml:"keep"`
	Threshold int    `yaml:"threshold"`
}

// PathConfig holds a file path.
type PathConfig struct {
	Path string `yaml:"path"`
}

// GateConfig selects the permission policy.
type GateConfig struct {
	Policy string `yaml:"policy"` // interactive | readonly | auto
}

// KubectlConfig constrains the kubectl tool.
type KubectlConfig struct {
	// Namespaces is an allowlist. Empty means any namespace.
	Namespaces []string `yaml:"namespaces"`
}

// Default returns the configuration used when no file exists.
func Default() Config {
	return Config{
		Provider:   ProviderConfig{Name: "anthropic"},
		MaxTurns:   20,
		Compaction: CompactionConfig{Strategy: "none", Keep: 40, Threshold: 60},
		Memory:     PathConfig{Path: ".groot/groot.db"},
		Audit:      PathConfig{Path: ".groot/audit.jsonl"},
		Gate:       GateConfig{Policy: "interactive"},
		Work:       WorkConfig{MaxTurns: 50, MaxAutoTurns: 10, MaxUSD: 5.0, AlwaysAsk: DefaultAlwaysAsk()},
		Flow: FlowConfig{Mode: "strict", Routes: FlowRoutes{
			Small:  FlowRoute{Model: "claude-haiku-5-5"},
			Medium: FlowRoute{Model: "claude-sonnet-5-5"},
			Large:  FlowRoute{Model: "claude-opus-5-5"},
		}},
	}
}

// Load reads path. A missing file yields the defaults; a present file is
// parsed over the defaults, expanded, and validated.
func Load(path string) (Config, error) {
	cfg := Default()
	raw, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return cfg, cfg.Validate()
	}
	if err != nil {
		return cfg, fmt.Errorf("read %s: %w", path, err)
	}

	var doc yaml.Node
	if err := yaml.Unmarshal(raw, &doc); err != nil {
		return cfg, fmt.Errorf("parse %s: %w", path, err)
	}
	expandNode(&doc, os.LookupEnv)
	if err := doc.Decode(&cfg); err != nil {
		return cfg, fmt.Errorf("decode %s: %w", path, err)
	}
	// An unset ${VAR:-} expands to "", which must not become an allowlist entry.
	cfg.Kubectl.Namespaces = dropEmpty(cfg.Kubectl.Namespaces)
	return cfg, cfg.Validate()
}

func dropEmpty(in []string) []string {
	var out []string
	for _, s := range in {
		if strings.TrimSpace(s) != "" {
			out = append(out, s)
		}
	}
	return out
}

// Validate checks enumerated fields and numeric bounds.
func (c Config) Validate() error {
	if c.Provider.Name != "anthropic" {
		return fmt.Errorf("provider.name %q is not supported (MVP supports: anthropic)", c.Provider.Name)
	}
	switch c.Gate.Policy {
	case "interactive", "readonly", "auto":
	default:
		return fmt.Errorf("gate.policy %q is invalid (use interactive, readonly or auto)", c.Gate.Policy)
	}
	if err := validateWork(c.Work); err != nil {
		return err
	}
	switch c.Flow.Mode {
	case "", "strict", "advisory":
	default:
		return fmt.Errorf("flow.mode %q is invalid (use strict or advisory)", c.Flow.Mode)
	}
	if c.MaxTurns <= 0 {
		return fmt.Errorf("max_turns must be positive, got %d", c.MaxTurns)
	}
	seen := map[string]bool{}
	for _, src := range c.Library.Sources {
		if strings.TrimSpace(src.Name) == "" {
			return errors.New("library.sources: every source needs a name")
		}
		if seen[src.Name] {
			return fmt.Errorf("library.sources: %q is listed twice", src.Name)
		}
		seen[src.Name] = true
		if !strings.HasPrefix(src.URL, "https://") && !strings.HasPrefix(src.URL, "http://") {
			return fmt.Errorf("library.sources: %q needs an http(s) url", src.Name)
		}
	}
	switch c.Compaction.Strategy {
	case "none":
	case "window":
		if c.Compaction.Keep <= 0 {
			return errors.New("compaction.keep must be positive for the window strategy")
		}
	case "summarize":
		if c.Compaction.Threshold <= 0 {
			return errors.New("compaction.threshold must be positive for the summarize strategy")
		}
	default:
		return fmt.Errorf("compaction.strategy %q is invalid (use none, window or summarize)", c.Compaction.Strategy)
	}
	return nil
}

// EffectiveSystemPrompt returns the configured override or the default.
func (c Config) EffectiveSystemPrompt() string {
	if strings.TrimSpace(c.SystemPrompt) != "" {
		return c.SystemPrompt
	}
	return DefaultSystemPrompt
}

var envRef = regexp.MustCompile(`\$\{([A-Za-z_][A-Za-z0-9_]*)(:-([^}]*))?\}`)

// Expand replaces ${VAR} and ${VAR:-default} using lookup. An unset or empty
// variable uses the default when one is given, and expands to "" otherwise.
func Expand(s string, lookup func(string) (string, bool)) string {
	return envRef.ReplaceAllStringFunc(s, func(m string) string {
		sub := envRef.FindStringSubmatch(m)
		name, hasDefault, def := sub[1], sub[2] != "", sub[3]
		v, ok := lookup(name)
		if ok && v != "" {
			return v
		}
		if hasDefault {
			return def
		}
		return ""
	})
}

// expandNode expands every scalar in the YAML tree.
func expandNode(n *yaml.Node, lookup func(string) (string, bool)) {
	if n.Kind == yaml.ScalarNode {
		n.Value = Expand(n.Value, lookup)
	}
	for _, c := range n.Content {
		expandNode(c, lookup)
	}
}

// LoadProjectContext returns the trimmed contents of dir/AGENTS.md, or "" when absent.
func LoadProjectContext(dir string) (string, error) {
	b, err := os.ReadFile(filepath.Join(dir, "AGENTS.md"))
	if errors.Is(err, os.ErrNotExist) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(b)), nil
}

// ComposeSystemPrompt joins the base prompt, the memory preamble and the
// project context. Empty parts are omitted.
func ComposeSystemPrompt(base, memoryPreamble, projectContext string) string {
	var sb strings.Builder
	sb.WriteString(strings.TrimSpace(base))
	if p := strings.TrimSpace(memoryPreamble); p != "" {
		sb.WriteString("\n\n## Long-term memory\n")
		sb.WriteString(p)
	}
	if c := strings.TrimSpace(projectContext); c != "" {
		sb.WriteString("\n\n## Project context\n")
		sb.WriteString(c)
	}
	return sb.String()
}
