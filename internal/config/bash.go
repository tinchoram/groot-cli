package config

import (
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

// DefaultBashTimeout bounds one bash command when the call names no timeout_seconds.
const DefaultBashTimeout = 60 * time.Second

// DefaultBashMaxTimeout is the ceiling for timeout_seconds when the config sets none.
const DefaultBashMaxTimeout = 10 * time.Minute

// maxBashTimeoutLimit is the highest value bash.max_timeout may take.
const maxBashTimeoutLimit = time.Hour

// BashConfig holds the settings of the bash tool. Timeout is the default limit
// of one command; MaxTimeout is the ceiling a call may request with timeout_seconds.
type BashConfig struct {
	Timeout    time.Duration `yaml:"timeout"`
	MaxTimeout time.Duration `yaml:"max_timeout"`
	Sandbox    SandboxConfig `yaml:"sandbox"`
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
	if b.Timeout < time.Second {
		return fmt.Errorf("bash.timeout %s is invalid (must be at least 1s)", b.Timeout)
	}
	if b.MaxTimeout > maxBashTimeoutLimit {
		return fmt.Errorf("bash.max_timeout %s is invalid (must be at most 1h)", b.MaxTimeout)
	}
	if b.MaxTimeout < b.Timeout {
		return fmt.Errorf("bash.max_timeout %s is invalid (must be at least bash.timeout %s)", b.MaxTimeout, b.Timeout)
	}
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
