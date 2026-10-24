package config

import (
	"errors"
	"fmt"
	"regexp"
)

// WorkConfig limits work mode: the tool calls one task may use, the spend of a
// session, and the bash commands that always ask the user.
type WorkConfig struct {
	MaxTurns     int      `yaml:"max_turns"`
	MaxAutoTurns int      `yaml:"max_auto_turns"`
	MaxUSD       float64  `yaml:"max_usd"`
	AlwaysAsk    []string `yaml:"always_ask"`
}

// DefaultAlwaysAsk lists the commands that work mode never approves on its own.
func DefaultAlwaysAsk() []string {
	return []string{
		`\brm\s+-[a-zA-Z]*r[a-zA-Z]*\s+(/|~|\.\.)`,
		`(^|[\s;&|])sudo\b`,
		`\bgit\s+push\b.*(--force\b|\s-f\b)`,
		`\bgit\s+reset\s+--hard\b`,
		`\bgit\s+clean\b`,
		`\bkubectl\s+(apply|delete|patch|edit|scale|rollout)\b`,
		`\bdocker\s+(rm|rmi)\b|\bdocker\s+system\s+prune\b`,
		`\bterraform\s+(apply|destroy)\b`,
		`\b(curl|wget)\b.*(\s-o\b|\s-O\b|--output\b)`,
	}
}

// validateWork checks the limits and the patterns.
func validateWork(w WorkConfig) error {
	if w.MaxTurns < 0 || w.MaxUSD < 0 || w.MaxAutoTurns < 0 {
		return errors.New("work.max_turns and work.max_usd cannot be negative")
	}
	for _, p := range w.AlwaysAsk {
		if _, err := regexp.Compile(p); err != nil {
			return fmt.Errorf("work.always_ask %q is not a valid pattern: %w", p, err)
		}
	}
	return nil
}
