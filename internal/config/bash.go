package config

import (
	"fmt"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

// BashConfig holds the settings of the bash tool.
type BashConfig struct {
	Sandbox SandboxConfig `yaml:"sandbox"`
}

// SandboxConfig confines the commands the bash tool runs. Mode is auto (the
// best backend, with a warning when there is none), strict (refuse to run
// without a backend) or off. Network allows outbound connections. ExtraWritable
// lists more absolute directories the commands may write.
//
// It is written either as a scalar, `sandbox: strict`, which sets the mode, or
// as a mapping with mode, network and extra_writable.
type SandboxConfig struct {
	Mode          string   `yaml:"mode"`
	Network       bool     `yaml:"network"`
	ExtraWritable []string `yaml:"extra_writable"`
}

// UnmarshalYAML accepts the scalar form, which sets the mode, and the mapping
// form, which keeps the defaults of the keys it does not set.
func (c *SandboxConfig) UnmarshalYAML(value *yaml.Node) error {
	if value.Kind == yaml.ScalarNode {
		c.Mode = strings.TrimSpace(value.Value)
		return nil
	}
	type plain SandboxConfig
	p := plain(*c)
	if err := value.Decode(&p); err != nil {
		return err
	}
	*c = SandboxConfig(p)
	return nil
}

// validateBash checks the enumerated and path settings of the bash tool.
func validateBash(b BashConfig) error {
	switch b.Sandbox.Mode {
	case "", "auto", "strict", "off":
	default:
		return fmt.Errorf("bash.sandbox.mode %q is invalid (use auto, strict or off)", b.Sandbox.Mode)
	}
	for _, p := range b.Sandbox.ExtraWritable {
		if strings.TrimSpace(p) == "" || !filepath.IsAbs(p) {
			return fmt.Errorf("bash.sandbox.extra_writable: %q must be an absolute path", p)
		}
	}
	return nil
}
