// Command groot is a terminal agent for SRE and platform work. This file
// only wires the packages together; the logic lives in internal/.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"time"

	"github.com/tinchoram/groot-cli/internal/auth"
	"github.com/tinchoram/groot-cli/internal/config"
	"github.com/tinchoram/groot-cli/internal/gate"
	"github.com/tinchoram/groot-cli/internal/home"
	"github.com/tinchoram/groot-cli/internal/provider"
	"github.com/tinchoram/groot-cli/internal/sandbox"
	"github.com/tinchoram/groot-cli/internal/space"
	"github.com/tinchoram/groot-cli/internal/ui"
	"github.com/tinchoram/groot-cli/internal/update"
	"github.com/tinchoram/groot-cli/internal/workspace"
)

// version is overridden at build time with -ldflags "-X main.version=...".
var version = "0.6.0"

func main() {
	os.Exit(run(os.Args[1:], os.Stdin, os.Stdout, os.Stderr, os.Getenv))
}

// newKeychain builds the Anthropic key's keychain item. Tests replace it with a fake.
var newKeychain = func() auth.Keychain {
	return auth.SystemKeychain(runtime.GOOS, auth.ExecCommand)
}

// newGeminiKeychain builds the Gemini key's keychain item. Tests replace it too.
var newGeminiKeychain = func() auth.Keychain {
	return auth.SystemKeychainFor(auth.Gemini, runtime.GOOS, auth.ExecCommand)
}

// providerKeys holds the key of each provider groot can call. A provider
// without a stored key has an empty one.
type providerKeys struct {
	anthropic string
	gemini    string
}

// of returns the key of the named provider.
func (k providerKeys) of(name string) string {
	if name == provider.NameGemini {
		return k.gemini
	}
	return k.anthropic
}

// keyed lists the providers that have a key, in catalog order.
func (k providerKeys) keyed() []string {
	var out []string
	if k.anthropic != "" {
		out = append(out, provider.NameAnthropic)
	}
	if k.gemini != "" {
		out = append(out, provider.NameGemini)
	}
	return out
}

// keyEnv holds the key variables as the shell had them, before .env was loaded.
type keyEnv struct {
	anthropic string
	gemini    string
}

// missingKeyError says which provider has no key and how to add one. Its text is
// meant to be printed as it is.
type missingKeyError struct {
	provider string
}

func (e *missingKeyError) Error() string {
	if e.provider == provider.NameGemini {
		return "groot: no Gemini API key found.\n" +
			"  Run: groot auth login --provider gemini\n" +
			"  Or export GEMINI_API_KEY=<your key> in this shell, or put it in a .env file in the working directory.\n" +
			"  groot never reads the key from groot.yaml."
	}
	return "groot: no Anthropic API key found.\n" +
		"  Run: groot auth login\n" +
		"  Or export ANTHROPIC_API_KEY=<your key> in this shell, or put it in a .env file in the working directory.\n" +
		"  groot never reads the key from groot.yaml."
}

func run(args []string, stdin io.Reader, stdout, stderr io.Writer, getenv func(string) string) int {
	if len(args) > 0 {
		switch args[0] {
		case "auth":
			return runAuth(args[1:], stdin, stdout, stderr, getenv)
		case "doctor":
			return runDoctor(args[1:], stdout, stderr, getenv)
		case "upgrade":
			return runUpgrade(args[1:], stdout, stderr, upgradeEnvironment(getenv))
		}
	}

	fs := flag.NewFlagSet("groot", flag.ContinueOnError)
	fs.SetOutput(stderr)
	cfgPath := fs.String("config", "groot.yaml", "path to the configuration file")
	model := fs.String("model", "", "model override (for example claude-opus-5-5 or gemini-3.8-flash)")
	policy := fs.String("policy", "", "permission policy override: interactive, readonly or auto")
	showVersion := fs.Bool("version", false, "print the version and exit")
	if err := fs.Parse(args); err != nil {
		return 2
	}

	if *showVersion {
		fmt.Fprintf(stdout, "groot %s\n", version)
		return 0
	}

	// The keys from the shell are read before .env fills in the variables the
	// shell did not export, so that the source can be reported correctly.
	env := keyEnv{anthropic: getenv(auth.Anthropic.EnvVar), gemini: getenv(auth.Gemini.EnvVar)}
	// A .env in the working directory fills in variables the shell did not export.
	if err := config.LoadDotEnv(".env"); err != nil {
		fmt.Fprintf(stderr, "groot: %v\n", err)
		return 1
	}

	groot, err := home.Resolve(getenv, os.UserHomeDir)
	if err != nil {
		fmt.Fprintf(stderr, "groot: %v\n", err)
		return 1
	}
	// A missing key is reported before a configuration error: the key is what
	// the user needs first. The key required is that of the configured model.
	cfg, cfgErr := loadConfig(explicitFlag(fs, "config"), *cfgPath, filepath.Join(groot, "groot.yaml"))
	if cfgErr == nil {
		if *model != "" {
			cfg.Provider.Model = *model
		}
		if *policy != "" {
			cfg.Gate.Policy = *policy
		}
		cfgErr = cfg.Validate()
	}
	keys, warnings, keyErr := resolveKeys(env, groot, provider.NameOf(cfg.Provider.Model, cfg.Provider.Name))
	for _, w := range warnings {
		fmt.Fprintf(stderr, "warning: %s\n", w)
	}
	var missing *missingKeyError
	switch {
	case errors.As(keyErr, &missing):
		fmt.Fprintln(stderr, keyErr)
		return 1
	case keyErr != nil:
		fmt.Fprintf(stderr, "groot: %v\n", keyErr)
		return 1
	case cfgErr != nil:
		fmt.Fprintf(stderr, "groot: %v\n", cfgErr)
		return 1
	}

	if err := runApp(cfg, *cfgPath, groot, keys); err != nil {
		fmt.Fprintf(stderr, "groot: %v\n", err)
		return 1
	}
	return 0
}

// resolveKeys finds the key of each provider for groot home groot. The key of
// defaultName, the provider of the configured model, is required; the other is
// optional and lets a conversation switch to its models. Warnings explain stores
// that were skipped.
func resolveKeys(env keyEnv, groot, defaultName string) (providerKeys, []string, error) {
	cwd, err := os.Getwd()
	if err != nil {
		return providerKeys{}, nil, fmt.Errorf("working directory: %w", err)
	}
	ant, err := resolveService(auth.Anthropic, env.anthropic, groot, cwd, newKeychain())
	if err != nil {
		return providerKeys{}, nil, err
	}
	gem, err := resolveService(auth.Gemini, env.gemini, groot, cwd, newGeminiKeychain())
	if err != nil {
		return providerKeys{}, nil, err
	}
	keys := providerKeys{anthropic: ant.Key, gemini: gem.Key}
	warnings := append(ant.Warnings, gem.Warnings...)
	if keys.of(defaultName) == "" {
		name := defaultName
		if name == "" {
			name = provider.NameAnthropic
		}
		return keys, warnings, &missingKeyError{provider: name}
	}
	return keys, warnings, nil
}

// resolveService finds the key of one provider. shellKey is its variable as
// the shell had it.
func resolveService(svc auth.Service, shellKey, groot, cwd string, kc auth.Keychain) (auth.Credential, error) {
	return auth.Resolve(auth.Options{Service: svc, EnvKey: shellKey, Cwd: cwd, Home: groot, Keychain: kc})
}

// explicitFlag reports whether the flag was set on the command line.
func explicitFlag(fs *flag.FlagSet, name string) bool {
	set := false
	fs.Visit(func(f *flag.Flag) {
		if f.Name == name {
			set = true
		}
	})
	return set
}

// loadConfig applies the global file in the groot home and then the project
// file, each over the one before (see config.LoadLayers). A project path set
// explicitly must exist; the default groot.yaml and the global file are optional.
func loadConfig(explicit bool, projectPath, globalPath string) (config.Config, error) {
	if explicit {
		if _, err := os.Stat(projectPath); err != nil {
			return config.Config{}, fmt.Errorf("config file %s: %w", projectPath, err)
		}
	}
	return config.LoadWithUser(globalPath, config.UserFile(globalPath), projectPath)
}

// buildGate maps the policy name to a gate. confirm is used only by the
// interactive policy.
func buildGate(policy string, confirm func(req gate.Request) bool) (gate.Gate, error) {
	switch policy {
	case "interactive":
		return gate.Interactive{Confirm: confirm}, nil
	case "readonly":
		return gate.ReadOnly{}, nil
	case "auto":
		return gate.AutoApprove{}, nil
	}
	return nil, fmt.Errorf("unknown gate policy %q", policy)
}

// buildProvider creates the adapter of a provider kind with its key. The
// router calls it for every provider a conversation uses.
func buildProvider(keys providerKeys, effort string) provider.Builder {
	return func(name, model string) provider.Provider {
		if name == provider.NameGemini {
			return provider.NewGemini(provider.GeminiOptions{Model: model, APIKey: keys.gemini, Effort: effort})
		}
		return provider.NewAnthropic(provider.AnthropicOptions{Model: model, APIKey: keys.anthropic, Effort: effort})
	}
}

// runApp resolves the groot home, bootstraps it on first run, builds the
// workspace, and runs the terminal UI.
func runApp(cfg config.Config, cfgPath, groot string, keys providerKeys) error {
	userHome, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("home directory: %w", err)
	}
	fresh, err := workspace.Bootstrap(groot, userHome, time.Now())
	if err != nil {
		return fmt.Errorf("setup in %s: %w", groot, err)
	}
	spaces, err := space.Open(filepath.Join(groot, "spaces.yaml"))
	if err != nil {
		return err
	}

	bridge := ui.NewBridge()
	g, err := buildGate(cfg.Gate.Policy, nil) // the workspace binds approvals per conversation
	if err != nil {
		return err
	}
	runner, sandboxWarning := sandbox.Detect(sandbox.Config{Mode: cfg.Bash.Sandbox.Mode})
	build := buildProvider(keys, cfg.Provider.Effort)
	keyed := keys.keyed()
	ws, err := workspace.New(workspace.Options{
		Home:             groot,
		UserHome:         userHome,
		Version:          version,
		Sandbox:          runner,
		Config:           cfg,
		ConfigPath:       cfgPath,
		GlobalConfigPath: filepath.Join(groot, "groot.yaml"),
		UserConfigPath:   config.UserFile(filepath.Join(groot, "groot.yaml")),
		Spaces:           spaces,
		Gate:             g,
		Hooks: workspace.Hooks{
			OnText:     bridge.Text,
			OnToolCall: bridge.ToolCall,
			OnWarning:  bridge.Warn,
			OnConfirm:  bridge.Confirm,
			OnNotice:   bridge.Notice,
			OnAnswer:   bridge.Done,
			OnAsk:      bridge.Ask,
			OnInbox:    bridge.Inbox,
			OnJobs:     bridge.Jobs,
			OnAuto:     bridge.Auto,
		},
		// The router sends each conversation to the provider of its model, and
		// the catalog merges the providers that have a key.
		NewProvider: func(model string) provider.Provider {
			return provider.NewRouter(provider.RouterOptions{
				Default: cfg.Provider.Name,
				Model:   model,
				Keyed:   keyed,
				Build:   build,
			})
		},
	})
	if err != nil {
		return err
	}
	// A configured model the provider does not list is a warning, never a stop.
	checkCtx, cancelCheck := context.WithTimeout(context.Background(), 10*time.Second)
	if warn := ws.CheckConfiguredModel(checkCtx); warn != "" {
		fmt.Fprintln(os.Stderr, "warning: "+warn)
	}
	cancelCheck()
	historyPath := filepath.Join(groot, "history")
	entries, err := ui.LoadHistory(historyPath)
	if err != nil {
		return fmt.Errorf("load prompt history: %w", err)
	}
	layoutPath := filepath.Join(groot, "layout.yaml")
	layout, err := ui.LoadLayout(layoutPath)
	if err != nil {
		return fmt.Errorf("load layout: %w", err)
	}
	return ui.Run(ui.Options{
		Workspace:   ws,
		Bridge:      bridge,
		Version:     version,
		LayoutPath:  layoutPath,
		Layout:      &layout,
		History:     ui.NewHistory(entries, ui.HistoryCap),
		HistoryPath: historyPath,
		ShowWelcome: fresh,
		ShowCost:    cfg.UI.ShowCost,
		// The sandbox state is shown in the header; the notice explains it once.
		SandboxName:   runner.Name(),
		SandboxNotice: string(sandboxWarning),
		// The release check runs in the background; a newer version shows as a note.
		UpdateNotices: startUpdateNotice(cfg.Updates.Check, groot, version, update.Token(os.Getenv)),
	})
}
