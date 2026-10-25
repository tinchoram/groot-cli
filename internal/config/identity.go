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

// UserConfig is the operator's identity: the name agents mention with @ and
// the banner prints as "operated by". It is set in the global config or in
// user.yaml next to it, never in a project file.
type UserConfig struct {
	Name string `yaml:"name"`
}

// userFileName is the identity file in the groot home. It holds only the name,
// written by the onboarding prompt and /me, so groot.yaml keeps its comments.
const userFileName = "user.yaml"

// userNameRule is the shape of a name: 2 to 32 letters, digits, _ or -.
var userNameRule = regexp.MustCompile(`^[A-Za-z0-9_-]{2,32}$`)

// errUserNameRule is the message every rejected name gets.
const errUserNameRule = "user.name must be 2-32 letters, digits, _ or -"

// NormalizeUserName trims the name, drops one leading @, and checks the rule.
// The result is what is stored and printed without the @.
func NormalizeUserName(raw string) (string, error) {
	name := strings.TrimPrefix(strings.TrimSpace(raw), "@")
	if !userNameRule.MatchString(name) {
		return "", errors.New(errUserNameRule)
	}
	return name, nil
}

// UserFile is the identity file that sits next to the global config: the
// groot home's user.yaml for ~/.groot/groot.yaml.
func UserFile(globalPath string) string {
	return filepath.Join(filepath.Dir(globalPath), userFileName)
}

// SaveUserName writes the name into the identity file at path. The write is
// atomic: a temporary file in the same directory is renamed over the old one,
// and it is created with mode 0600 like the credentials.
func SaveUserName(path, raw string) error {
	name, err := NormalizeUserName(raw)
	if err != nil {
		return err
	}
	out, err := yaml.Marshal(UserConfig{Name: name})
	if err != nil {
		return fmt.Errorf("encode %s: %w", path, err)
	}
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, ".user-*.yaml")
	if err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName) // no-op once the rename has moved it
	if err := tmp.Chmod(0o600); err != nil {
		tmp.Close()
		return fmt.Errorf("write %s: %w", path, err)
	}
	if _, err := tmp.Write(out); err != nil {
		tmp.Close()
		return fmt.Errorf("write %s: %w", path, err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}
	if err := os.Rename(tmpName, path); err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}
	return nil
}

// loadUserLayer applies the identity file: a top-level name, expanded like the
// other files and normalized by the caller. A missing file changes nothing.
func loadUserLayer(cfg *Config, path string) error {
	raw, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("read %s: %w", path, err)
	}
	var doc yaml.Node
	if err := yaml.Unmarshal(raw, &doc); err != nil {
		return fmt.Errorf("parse %s: %w", path, err)
	}
	expandConfigNode(&doc, os.LookupEnv)
	var id UserConfig
	if err := doc.Decode(&id); err != nil {
		return fmt.Errorf("decode %s: %w", path, err)
	}
	if strings.TrimSpace(id.Name) != "" {
		cfg.User.Name = id.Name
	}
	return nil
}

// rejectProjectUserName refuses a project file that sets the operator's name.
// The name belongs to the operator, not to the repository being worked on.
func rejectProjectUserName(path string, raw []byte) error {
	var probe struct {
		User struct {
			Name yaml.Node `yaml:"name"`
		} `yaml:"user"`
	}
	if err := yaml.Unmarshal(raw, &probe); err != nil {
		return nil
	}
	if probe.User.Name.Kind != 0 {
		return fmt.Errorf("%s: user.name can only be set in the global config (~/.groot/groot.yaml): a project file names the operator for every agent it reaches", path)
	}
	return nil
}

// LoadWithUser loads the global config, then the identity file, then the
// project files, each over the one before. The identity file sits between the
// two so that user.yaml overrides a user.name in groot.yaml, and no project file
// may set user.name at all.
func LoadWithUser(globalPath, userPath string, projectPaths ...string) (Config, error) {
	layers := []layer{{path: globalPath, kind: layerGlobal}, {path: userPath, kind: layerUser}}
	for _, p := range projectPaths {
		// The global file read again as the project is the same file: it was
		// already applied with the global rules.
		if filepath.Clean(p) == filepath.Clean(globalPath) {
			continue
		}
		layers = append(layers, layer{path: p, kind: layerProject})
	}
	return loadLayers(layers)
}

// PeopleBlock is the People section of every system prompt. The operator
// sentence is there only when a name is set; the groot sentence always is.
func PeopleBlock(operator string) string {
	var sb strings.Builder
	sb.WriteString("## People\n")
	if operator != "" {
		fmt.Fprintf(&sb, "The operator is @%s. Mention @%s only when you need a decision, approval or information from them; the UI highlights it.\n", operator, operator)
	}
	sb.WriteString("@Groot is the coordinator agent: to ask groot for something, use send_message to groot; a mention alone does not reach it.")
	return sb.String()
}
