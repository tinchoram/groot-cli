// Package auth finds and stores the provider API keys. A key is taken from the
// first of these that holds one: the provider's environment variable (for
// Anthropic, ANTHROPIC_API_KEY; for Gemini, GEMINI_API_KEY), a .env file in the
// working directory, the macOS keychain, and the credentials file in the groot
// home. The keychain is reached through the security command on macOS only;
// elsewhere the credentials file is the only store. Every provider uses the
// same stores, each under its own item or field.
package auth

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/tinchoram/groot-cli/internal/config"
)

// EnvVar is the environment variable that carries the Anthropic key.
const EnvVar = "ANTHROPIC_API_KEY"

// Service is a provider whose key groot stores.
type Service struct {
	Name     string // shown in messages: "Anthropic", "Gemini"
	EnvVar   string // environment variable and .env name
	Keychain string // keychain service name of the item
	FileKey  string // field of the credentials file
}

// The providers that have a stored key.
var (
	Anthropic = Service{Name: "Anthropic", EnvVar: EnvVar, Keychain: "groot.anthropic", FileKey: "anthropic_api_key"}
	Gemini    = Service{Name: "Gemini", EnvVar: "GEMINI_API_KEY", Keychain: "groot.gemini", FileKey: "gemini_api_key"}
)

// Source labels, as shown by `groot auth status` and doctor.
const (
	SourceEnvironment = "environment"
	SourceDotEnv      = ".env"
	SourceKeychain    = "keychain"
	SourceCredentials = "credentials file"
	SourceNone        = "none"
)

// keychainAccount is the account of every keychain item groot stores.
const (
	keychainAccount = "groot"
	// securityNotFound is the exit status of the security command for an item
	// that does not exist (errSecItemNotFound).
	securityNotFound = 44
)

// ErrNotFound means the store holds no key. ErrUnavailable means the store does
// not exist on this platform.
var (
	ErrNotFound    = errors.New("no API key is stored")
	ErrUnavailable = errors.New("the keychain is available on macOS only")
)

// Keychain is the secret store that the OS provides.
type Keychain interface {
	Get() (string, error)
	Set(key string) error
	Delete() error
}

// Command runs an external program and returns its stdout and exit status. A
// non-nil error means the program could not be started. Tests inject a fake.
type Command func(name string, args ...string) (stdout []byte, exitCode int, err error)

// ExecCommand is the Command that runs real programs.
func ExecCommand(name string, args ...string) ([]byte, int, error) {
	out, err := exec.Command(name, args...).Output()
	if err == nil {
		return out, 0, nil
	}
	var ee *exec.ExitError
	if errors.As(err, &ee) {
		return out, ee.ExitCode(), nil
	}
	return nil, -1, err
}

// SystemKeychain returns the Anthropic keychain item for goos. See SystemKeychainFor.
func SystemKeychain(goos string, run Command) Keychain {
	return SystemKeychainFor(Anthropic, goos, run)
}

// SystemKeychainFor returns the keychain item of service for goos. On darwin it
// uses the security command through run; elsewhere every operation reports
// ErrUnavailable.
func SystemKeychainFor(svc Service, goos string, run Command) Keychain {
	if goos == "darwin" {
		return securityKeychain{run: run, service: svc.Keychain}
	}
	return unavailableKeychain{}
}

// securityKeychain stores the key as a generic password item through the
// security command. The key is passed as an argument, so it is visible to ps for
// the moment the command runs; that is the trade-off of avoiding cgo.
type securityKeychain struct {
	run     Command
	service string
}

func (k securityKeychain) Get() (string, error) {
	out, code, err := k.run("security", "find-generic-password", "-a", keychainAccount, "-s", k.service, "-w")
	if err != nil {
		return "", fmt.Errorf("keychain: cannot run security: %w", err)
	}
	if code == securityNotFound {
		return "", ErrNotFound
	}
	if code != 0 {
		return "", fmt.Errorf("keychain: security exited with status %d", code)
	}
	key := strings.TrimSpace(string(out))
	if key == "" {
		return "", ErrNotFound
	}
	return key, nil
}

func (k securityKeychain) Set(key string) error {
	_, code, err := k.run("security", "add-generic-password", "-U", "-a", keychainAccount, "-s", k.service, "-w", key)
	if err != nil {
		return fmt.Errorf("keychain: cannot run security: %w", err)
	}
	if code != 0 {
		return fmt.Errorf("keychain: security exited with status %d", code)
	}
	return nil
}

func (k securityKeychain) Delete() error {
	_, code, err := k.run("security", "delete-generic-password", "-a", keychainAccount, "-s", k.service)
	if err != nil {
		return fmt.Errorf("keychain: cannot run security: %w", err)
	}
	if code == securityNotFound {
		return ErrNotFound
	}
	if code != 0 {
		return fmt.Errorf("keychain: security exited with status %d", code)
	}
	return nil
}

type unavailableKeychain struct{}

func (unavailableKeychain) Get() (string, error) { return "", ErrUnavailable }
func (unavailableKeychain) Set(string) error     { return ErrUnavailable }
func (unavailableKeychain) Delete() error        { return ErrUnavailable }

// CredentialsPath is the credentials file in the groot home.
func CredentialsPath(home string) string {
	return filepath.Join(home, "credentials")
}

// Options are the inputs of Resolve. EnvKey is the service's variable as the
// process had it before .env was loaded, so a key from .env is told apart from
// one exported in the shell. Keychain may be nil. Service selects the provider;
// the zero value means Anthropic.
type Options struct {
	Service  Service
	EnvKey   string
	Cwd      string
	Home     string
	Keychain Keychain
}

func (o Options) service() Service {
	if o.Service.EnvVar == "" {
		return Anthropic
	}
	return o.Service
}

// Credential is the key that Resolve found and where it came from. Path is the
// .env file or the credentials file that supplied it. Warnings explain stores
// that were skipped.
type Credential struct {
	Key      string
	Source   string
	Path     string
	Warnings []string
}

// Resolve returns the first key found, in the order environment, .env in the
// working directory, keychain, credentials file. No key is not an error: the
// Source is SourceNone.
func Resolve(o Options) (Credential, error) {
	svc := o.service()
	if o.EnvKey != "" {
		return Credential{Key: o.EnvKey, Source: SourceEnvironment}, nil
	}
	dotEnv := filepath.Join(o.Cwd, ".env")
	if v, found, err := config.DotEnvValue(dotEnv, svc.EnvVar); err != nil {
		return Credential{}, err
	} else if found && v != "" {
		return Credential{Key: v, Source: SourceDotEnv, Path: dotEnv}, nil
	}

	var warnings []string
	if o.Keychain != nil {
		key, err := o.Keychain.Get()
		switch {
		case err == nil && key != "":
			return Credential{Key: key, Source: SourceKeychain, Warnings: warnings}, nil
		case err == nil, errors.Is(err, ErrNotFound), errors.Is(err, ErrUnavailable):
		default:
			warnings = append(warnings, err.Error())
		}
	}

	path := CredentialsPath(o.Home)
	key, warning, err := readCredentials(path, svc.FileKey)
	if err != nil {
		return Credential{}, err
	}
	if warning != "" {
		warnings = append(warnings, warning)
	}
	if key != "" {
		return Credential{Key: key, Source: SourceCredentials, Path: path, Warnings: warnings}, nil
	}
	return Credential{Source: SourceNone, Warnings: warnings}, nil
}

// Label is the human name of the source, for status and doctor output.
func Label(c Credential) string {
	switch c.Source {
	case SourceDotEnv:
		return ".env in " + filepath.Dir(c.Path)
	case SourceCredentials:
		return "credentials file " + c.Path
	case SourceNone, "":
		return SourceNone
	}
	return c.Source
}

// Mask shows the last four characters of a key, or nothing for a short key.
func Mask(key string) string {
	if len(key) <= 8 {
		return ""
	}
	return "…" + key[len(key)-4:]
}

// ValidKey checks the shape only: non-empty and without whitespace. The API
// decides whether the key works.
func ValidKey(key string) error {
	if key == "" {
		return errors.New("the key is empty")
	}
	if strings.ContainsAny(key, " \t\r\n") {
		return errors.New("the key must not contain spaces or line breaks")
	}
	return nil
}

// Where says where Login stored the key.
type Where struct {
	Store string // StoreKeychain or StoreFile
	Path  string // the credentials file, for StoreFile
}

// Store names for Where.
const (
	StoreKeychain = "keychain"
	StoreFile     = "credentials file"
)

// Login stores the Anthropic key in the keychain and, when the keychain fails,
// in the credentials file of home.
func Login(home string, kc Keychain, key string) (Where, error) {
	return LoginFor(Anthropic, home, kc, key)
}

// LoginFor stores the key of svc in the keychain and, when the keychain fails,
// in the credentials file of home.
func LoginFor(svc Service, home string, kc Keychain, key string) (Where, error) {
	if err := ValidKey(key); err != nil {
		return Where{}, err
	}
	if kc != nil {
		if err := kc.Set(key); err == nil {
			return Where{Store: StoreKeychain}, nil
		}
	}
	path, err := storeInFile(home, svc, key)
	if err != nil {
		return Where{}, err
	}
	return Where{Store: StoreFile, Path: path}, nil
}

// Logout removes the Anthropic key from the keychain and from the credentials
// file. It returns the names of what it removed, empty when nothing was stored.
func Logout(home string, kc Keychain) ([]string, error) {
	return LogoutFor(Anthropic, home, kc)
}

// LogoutFor removes the key of svc from the keychain and from the credentials
// file. The file is deleted when no key is left in it. It returns the names of
// what it removed.
func LogoutFor(svc Service, home string, kc Keychain) ([]string, error) {
	var removed []string
	if kc != nil {
		err := kc.Delete()
		switch {
		case err == nil:
			removed = append(removed, "keychain")
		case errors.Is(err, ErrNotFound), errors.Is(err, ErrUnavailable):
		default:
			return removed, err
		}
	}
	path := CredentialsPath(home)
	doc, err := readCredentialsDoc(path)
	if err != nil {
		return removed, err
	}
	if doc == nil {
		return removed, nil
	}
	if _, ok := doc[svc.FileKey]; !ok {
		return removed, nil
	}
	delete(doc, svc.FileKey)
	if len(doc) == 0 {
		if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
			return removed, fmt.Errorf("remove %s: %w", path, err)
		}
		return append(removed, path), nil
	}
	if err := writeCredentialsDoc(path, doc); err != nil {
		return removed, err
	}
	return append(removed, path), nil
}

// storeInFile writes the key of svc to the credentials file of home with mode
// 0600, keeping the other fields of the file.
func storeInFile(home string, svc Service, key string) (string, error) {
	if err := os.MkdirAll(home, 0o700); err != nil {
		return "", fmt.Errorf("create %s: %w", home, err)
	}
	path := CredentialsPath(home)
	doc, err := readCredentialsDoc(path)
	if err != nil {
		return "", err
	}
	if doc == nil {
		doc = map[string]string{}
	}
	doc[svc.FileKey] = key
	if err := writeCredentialsDoc(path, doc); err != nil {
		return "", err
	}
	return path, nil
}

// writeCredentialsDoc replaces the credentials file with doc, mode 0600.
func writeCredentialsDoc(path string, doc map[string]string) error {
	raw, err := yaml.Marshal(doc)
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, raw, 0o600); err != nil {
		return fmt.Errorf("write %s: %w", tmp, err)
	}
	if err := os.Chmod(tmp, 0o600); err != nil {
		return fmt.Errorf("chmod %s: %w", tmp, err)
	}
	if err := os.Rename(tmp, path); err != nil {
		return fmt.Errorf("replace %s: %w", path, err)
	}
	return nil
}

// readCredentialsDoc parses the credentials file. A missing file is nil, not
// an error. The parser error is not wrapped: it can echo a line of the file,
// which holds the key.
func readCredentialsDoc(path string) (map[string]string, error) {
	raw, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
	doc := map[string]string{}
	if err := yaml.Unmarshal(raw, &doc); err != nil {
		return nil, fmt.Errorf("%s is not a valid credentials file", path)
	}
	return doc, nil
}

// readCredentials returns the key of field in the credentials file. A file that
// is readable by group or others is ignored, with a warning that names its mode.
func readCredentials(path, field string) (key, warning string, err error) {
	fi, err := os.Stat(path)
	if errors.Is(err, os.ErrNotExist) {
		return "", "", nil
	}
	if err != nil {
		return "", "", fmt.Errorf("stat %s: %w", path, err)
	}
	if perm := fi.Mode().Perm(); perm&0o077 != 0 {
		return "", fmt.Sprintf("ignoring %s: its mode is %04o, expected 0600 (run chmod 600)", path, perm), nil
	}
	doc, err := readCredentialsDoc(path)
	if err != nil {
		return "", "", err
	}
	return strings.TrimSpace(doc[field]), "", nil
}
