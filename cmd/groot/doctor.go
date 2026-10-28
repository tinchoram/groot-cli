package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"time"

	"github.com/tinchoram/groot-cli/internal/auth"
	"github.com/tinchoram/groot-cli/internal/config"
	"github.com/tinchoram/groot-cli/internal/home"
	"github.com/tinchoram/groot-cli/internal/provider"
	"github.com/tinchoram/groot-cli/internal/sandbox"
	"github.com/tinchoram/groot-cli/internal/space"
	"github.com/tinchoram/groot-cli/internal/update"
)

// geminiProbeTimeout bounds the Gemini model listing doctor makes.
const geminiProbeTimeout = 5 * time.Second

// doctorEnv is everything doctor looks at. runDoctor fills it from the real
// machine; tests fill it with fakes.
type doctorEnv struct {
	Version        string
	EnvKey         string // ANTHROPIC_API_KEY as the shell had it
	GeminiEnvKey   string // GEMINI_API_KEY as the shell had it
	GeminiKeychain auth.Keychain
	// ProbeGemini lists the Gemini models with the key and returns how many
	// there are. Nil skips the probe; the report then says it was not probed.
	ProbeGemini    func(ctx context.Context, key string) (int, error)
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
	// ProbeMCP connects each enabled MCP server and reports it. Nil skips the
	// probe; the report then names the servers as not probed.
	ProbeMCP func(servers map[string]config.MCPServer) []mcpCheck
}

// mcpCheck is what doctor learns about one MCP server: its error, or the number
// of allowlisted tools it offers and the ones it does not.
type mcpCheck struct {
	Name    string
	Tools   int
	Missing []string
	Err     string
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
	GeminiKey    auth.Credential
	GeminiKeyErr error
	GeminiProbed bool
	GeminiModels int   // models the probe found
	GeminiErr    error // the probe failed

	Sandbox     string // backend name, see sandboxBackend
	SandboxHint string // install command, only when no backend is active
	Kubectl     string // path, empty when not on PATH
	Spaces      int    // -1 when spaces.yaml does not exist
	SpacesErr   error
	InboxPath   string
	InboxFound  bool
	Update      update.Status // from the cache only; doctor makes no network call
	// MCP is the probe of each enabled server, in name order. MCPNone is set
	// when no server is enabled.
	MCP     []mcpCheck
	MCPNone bool
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

	envKey := getenv(auth.Anthropic.EnvVar)
	geminiEnvKey := getenv(auth.Gemini.EnvVar)
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
		GeminiEnvKey:   geminiEnvKey,
		GeminiKeychain: newGeminiKeychain(),
		ProbeGemini:    probeGemini,
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
		ProbeMCP: func(servers map[string]config.MCPServer) []mcpCheck {
			return probeMCP(servers, cwd, version)
		},
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
		Update:       update.Known(env.Home, env.Version),
	}

	cfg, err := loadConfig(env.ExplicitConfig, env.ConfigPath, env.GlobalPath)
	in.ConfigErr = err
	if err == nil {
		servers := cfg.MCP.EnabledServers()
		in.MCPNone = len(servers) == 0
		if !in.MCPNone && env.ProbeMCP != nil {
			in.MCP = env.ProbeMCP(servers)
		}
	}
	mode := "auto"
	if err == nil {
		mode = cfg.Bash.Sandbox.Mode
	}

	in.Key, in.KeyErr = auth.Resolve(auth.Options{EnvKey: env.EnvKey, Cwd: env.Cwd, Home: env.Home, Keychain: env.Keychain})

	in.GeminiKey, in.GeminiKeyErr = auth.Resolve(auth.Options{Service: auth.Gemini, EnvKey: env.GeminiEnvKey, Cwd: env.Cwd, Home: env.Home, Keychain: env.GeminiKeychain})
	if in.GeminiKeyErr == nil && in.GeminiKey.Key != "" && env.ProbeGemini != nil {
		in.GeminiProbed = true
		ctx, cancel := context.WithTimeout(context.Background(), geminiProbeTimeout)
		in.GeminiModels, in.GeminiErr = env.ProbeGemini(ctx, in.GeminiKey.Key)
		cancel()
	}

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
	geminiLines(add, in)

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
	mcpLines(add, in)
	add("update", updateValue(in.Update), false)
	return lines, blocks
}

// geminiLines reports the Gemini key and, when there is one, the model listing.
// Gemini is optional, so nothing here blocks startup.
func geminiLines(add func(name, value string, blocking bool), in doctorInput) {
	switch {
	case in.GeminiKeyErr != nil:
		add("gemini key", "error: "+in.GeminiKeyErr.Error(), false)
	case in.GeminiKey.Key == "":
		add("gemini key", "missing (optional: needed for gemini- models)", false)
	default:
		add("gemini key", auth.Label(in.GeminiKey)+" ("+auth.Mask(in.GeminiKey.Key)+")", false)
	}
	for _, w := range in.GeminiKey.Warnings {
		add("gemini key warning", w, false)
	}
	switch {
	case in.GeminiKeyErr != nil || in.GeminiKey.Key == "":
		add("gemini models", "not checked (no key)", false)
	case !in.GeminiProbed:
		add("gemini models", "not probed", false)
	case in.GeminiErr != nil:
		add("gemini models", "unreachable: "+in.GeminiErr.Error(), false)
	default:
		add("gemini models", fmt.Sprintf("reachable, %d models", in.GeminiModels), false)
	}
}

// probeGemini lists the Gemini models with key. The error names only the HTTP
// status or that the connection failed, never the response body.
func probeGemini(ctx context.Context, key string) (int, error) {
	models, err := provider.NewGemini(provider.GeminiOptions{APIKey: key}).ListModels(ctx)
	if err != nil {
		var pe *provider.Error
		if errors.As(err, &pe) && pe.Status != 0 {
			return 0, fmt.Errorf("HTTP %d", pe.Status)
		}
		return 0, errors.New("connection failed")
	}
	return len(models), nil
}

// updateValue words the update status: available, up to date, or unknown with
// the reason the cache gives.
func updateValue(st update.Status) string {
	switch {
	case st.Newer:
		return "v" + st.Latest + " available"
	case st.Latest != "":
		return "up to date"
	}
	return "unknown (" + st.Reason + ")"
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
