package config

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"strings"
)

// LoadDotEnv reads KEY=VALUE pairs from path and sets the ones that are not
// already present in the environment, so a value exported in the shell always
// wins over the file. A missing file is not an error. Supported syntax: blank
// lines, `#` comments, an optional `export ` prefix, and single or double
// quoted values. Malformed lines are skipped.
func LoadDotEnv(path string) error {
	f, err := os.Open(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("config: open %s: %w", path, err)
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		key, value, ok := parseDotEnvLine(scanner.Text())
		if !ok {
			continue
		}
		if _, exists := os.LookupEnv(key); exists {
			continue
		}
		if err := os.Setenv(key, value); err != nil {
			return fmt.Errorf("config: set %s: %w", key, err)
		}
	}
	if err := scanner.Err(); err != nil {
		return fmt.Errorf("config: read %s: %w", path, err)
	}
	return nil
}

// DotEnvValue returns the value that the file at path sets for key, without
// touching the environment. found is false when the file is missing or does not
// set the key. Credential resolution uses it to tell a key that came from .env
// apart from one exported in the shell.
func DotEnvValue(path, key string) (value string, found bool, err error) {
	f, err := os.Open(path)
	if errors.Is(err, os.ErrNotExist) {
		return "", false, nil
	}
	if err != nil {
		return "", false, fmt.Errorf("config: open %s: %w", path, err)
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		k, v, ok := parseDotEnvLine(scanner.Text())
		if ok && k == key {
			return v, true, nil
		}
	}
	if err := scanner.Err(); err != nil {
		return "", false, fmt.Errorf("config: read %s: %w", path, err)
	}
	return "", false, nil
}

func parseDotEnvLine(line string) (key, value string, ok bool) {
	line = strings.TrimSpace(line)
	if line == "" || strings.HasPrefix(line, "#") {
		return "", "", false
	}
	line = strings.TrimPrefix(line, "export ")
	key, value, found := strings.Cut(line, "=")
	key = strings.TrimSpace(key)
	if !found || key == "" || strings.ContainsAny(key, " \t") {
		return "", "", false
	}
	value = strings.TrimSpace(value)
	switch {
	case len(value) >= 2 && (value[0] == '"' || value[0] == '\''):
		quote := value[0]
		if end := strings.IndexByte(value[1:], quote); end >= 0 {
			value = value[1 : end+1]
		}
	default:
		// Unquoted values may carry a trailing comment.
		if i := strings.Index(value, " #"); i >= 0 {
			value = strings.TrimSpace(value[:i])
		}
	}
	return key, value, true
}
