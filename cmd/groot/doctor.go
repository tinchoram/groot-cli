package main

import (
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"

	"github.com/tinchoram/groot-cli/internal/auth"
	"github.com/tinchoram/groot-cli/internal/config"
	"github.com/tinchoram/groot-cli/internal/home"
	"github.com/tinchoram/groot-cli/internal/sandbox"
	"github.com/tinchoram/groot-cli/internal/space"
)

// doctorEnv is everything doctor looks at. runDoctor fills it from the real
// machine; tests fill it with fakes.
type doctorEnv struct {
	Version        string
	EnvKey         string // ANTHROPIC_API_KEY as the shell had it
	Cwd            string
	Home           string
	ConfigPath     string // project file
	ExplicitConfig bool   // -config was set on the command line
	GlobalPath     string // <home>/groot.yaml
	Goos           string
	OSRelease      string
	Keychain       auth.Keychain
	LookPath       func(string) (string, error)
	Detect         func(sandbox.Config) (sandbox.Runner, sandbox.Warning)
}

// doctorInput holds the facts that doctor reports, gathered from doctorEnv.
type doctorInput struct {
	Version      string
	Home         string
	HomeExists   bool
	GlobalPath   string
	GlobalFound  bool
	ProjectPath  string
	ProjectFound bool
	ConfigErr    error
	Key          auth.Credential
	KeyErr       error
	Sandbox      string // backend name, see sandboxBackend
	SandboxHint  string // install command, only when no backend is active
	Kubectl      string // path, empty when not on PATH
	Spaces       int    // -1 when spaces.yaml does not exist
	SpacesErr    error
	InboxPath    string
	InboxFound   bool
}

// doctorLine is one line of the report. Blocks means groot cannot start.
type doctorLine struct {
	Name   string
	Value  string
	Blocks bool
}

// runDoctor handles `groot doctor`.
func runDoctor(args []string, stdout, stderr io.Writer, getenv func(string) string) int {
	fs := flag.NewFlagSet("groot doctor", flag.ContinueOnError)
	fs.SetOutput(stderr)
	cfgPath := fs.String("config", "groot.yaml", "path to the project configuration file")
	if err := fs.Parse(args); err != nil {
		return 2
	}

	envKey := getenv(auth.EnvVar)
	if err := config.LoadDotEnv(".env"); err != nil {
		fmt.Fprintf(stderr, "groot: %v\n", err)
		return 1
	}
	groot, err := home.Resolve(getenv, os.UserHomeDir)
	if err != nil {
		fmt.Fprintf(stderr, "groot: %v\n", err)
		return 1
	}
	cwd, err := os.Getwd()
	if err != nil {
		fmt.Fprintf(stderr, "groot: working directory: %v\n", err)
		return 1
	}
	return doctor(stdout, doctorEnv{
		Version:        version,
		EnvKey:         envKey,
		Cwd:            cwd,
		Home:           groot,
		ConfigPath:     *cfgPath,
		ExplicitConfig: explicitFlag(fs, "config"),
		GlobalPath:     filepath.Join(groot, "groot.yaml"),
		Goos:           runtime.GOOS,
		OSRelease:      sandbox.ReadOSRelease(runtime.GOOS),
		Keychain:       newKeychain(),
		LookPath:       exec.LookPath,
		Detect:         sandbox.Detect,
	})
}

// doctor prints the report and returns the exit status: 1 when something
// blocks startup, 0 otherwise.
func doctor(stdout io.Writer, env doctorEnv) int {
	lines, blocks := doctorLines(gatherDoctor(env))
	for _, l := range lines {
		fmt.Fprintf(stdout, "%s: %s\n", l.Name, l.Value)
	}
	if blocks {
		return 1
	}
	return 0
}

// gatherDoctor reads the machine through env. It never creates files.
func gatherDoctor(env doctorEnv) doctorInput {
	in := doctorInput{
		Version:      env.Version,
		Home:         env.Home,
		HomeExists:   isDir(env.Home),
		GlobalPath:   env.GlobalPath,
		GlobalFound:  isFile(env.GlobalPath),
		ProjectPath:  env.ConfigPath,
		ProjectFound: isFile(env.ConfigPath),
		InboxPath:    filepath.Join(env.Home, "inbox.db"),
		InboxFound:   isFile(filepath.Join(env.Home, "inbox.db")),
	}

	cfg, err := loadConfig(env.ExplicitConfig, env.ConfigPath, env.GlobalPath)
	in.ConfigErr = err
	mode := "auto"
	if err == nil {
		mode = cfg.Bash.Sandbox.Mode
	}

	in.Key, in.KeyErr = auth.Resolve(auth.Options{EnvKey: env.EnvKey, Cwd: env.Cwd, Home: env.Home, Keychain: env.Keychain})

	runner, _ := env.Detect(sandbox.Config{Mode: mode})
	in.Sandbox = sandboxBackend(runner, mode)
	if in.Sandbox == "none" || in.Sandbox == "refused" {
		in.SandboxHint = sandbox.InstallHint(env.Goos, env.OSRelease)
	}

	if path, err := env.LookPath("kubectl"); err == nil {
		in.Kubectl = path
	}

	in.Spaces = -1
	spacesPath := filepath.Join(env.Home, "spaces.yaml")
	if isFile(spacesPath) {
		store, err := space.Open(spacesPath)
		in.SpacesErr = err
		if err == nil {
			in.Spaces = len(store.List())
		}
	}
	return in
}

// sandboxBackend names the backend the way doctor shows it. A runner that
// reports none under strict mode is a refusal, not a plain fallback.
func sandboxBackend(r sandbox.Runner, mode string) string {
	switch name := r.Name(); {
	case name == sandbox.BackendSandboxExec:
		return "seatbelt"
	case name == sandbox.BackendBwrap:
		return "bwrap"
	case name == sandbox.BackendOff:
		return "off"
	case mode == "strict":
		return "refused"
	default:
		return "none"
	}
}

// doctorLines turns the facts into the report lines. It reports whether a line
// blocks startup: a missing key, or a config file that does not load.
func doctorLines(in doctorInput) ([]doctorLine, bool) {
	var lines []doctorLine
	blocks := false
	add := func(name, value string, blocking bool) {
		lines = append(lines, doctorLine{Name: name, Value: value, Blocks: blocking})
		blocks = blocks || blocking
	}

	add("version", in.Version, false)
	add("home", in.Home+" ("+existence(in.HomeExists, "exists", "missing, created on first run")+")", false)
	add("config global", in.GlobalPath+" ("+existence(in.GlobalFound, "found", "missing")+")", false)
	add("config project", in.ProjectPath+" ("+existence(in.ProjectFound, "found", "missing")+")", false)
	if in.ConfigErr != nil {
		add("config", "error: "+in.ConfigErr.Error(), true)
	}

	switch {
	case in.KeyErr != nil:
		add("api key", "error: "+in.KeyErr.Error(), true)
	case in.Key.Key == "":
		add("api key", "missing: run groot auth login", true)
	default:
		add("api key", auth.Label(in.Key)+" ("+auth.Mask(in.Key.Key)+")", false)
	}
	for _, w := range in.Key.Warnings {
		add("api key warning", w, false)
	}

	sandboxValue := in.Sandbox
	if in.SandboxHint != "" {
		sandboxValue += ". " + in.SandboxHint
	}
	add("sandbox", sandboxValue, false)

	if in.Kubectl != "" {
		add("kubectl", in.Kubectl, false)
	} else {
		add("kubectl", "not found on PATH (optional)", false)
	}

	switch {
	case in.SpacesErr != nil:
		add("spaces", "error: "+in.SpacesErr.Error(), false)
	case in.Spaces < 0:
		add("spaces", "none yet (no spaces.yaml)", false)
	default:
		add("spaces", fmt.Sprintf("%d in %s", in.Spaces, filepath.Join(in.Home, "spaces.yaml")), false)
	}
	add("inbox", in.InboxPath+" ("+existence(in.InboxFound, "found", "missing")+")", false)
	return lines, blocks
}

func existence(ok bool, yes, no string) string {
	if ok {
		return yes
	}
	return no
}

func isFile(path string) bool {
	fi, err := os.Stat(path)
	return err == nil && !fi.IsDir()
}

func isDir(path string) bool {
	fi, err := os.Stat(path)
	return err == nil && fi.IsDir()
}
