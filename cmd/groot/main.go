// Command groot is a terminal agent for SRE and platform work. This file
// only wires the packages together; the logic lives in internal/.
package main

import (
	"context"
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

// newKeychain builds the OS keychain store. Tests replace it with a fake.
var newKeychain = func() auth.Keychain {
	return auth.SystemKeychain(runtime.GOOS, auth.ExecCommand)
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
	model := fs.String("model", "", "model override (for example claude-opus-5-5)")
	policy := fs.String("policy", "", "permission policy override: interactive, readonly or auto")
	showVersion := fs.Bool("version", false, "print the version and exit")
	if err := fs.Parse(args); err != nil {
		return 2
	}

	if *showVersion {
		fmt.Fprintf(stdout, "groot %s\n", version)
		return 0
	}

	// The key from the shell is read before .env fills in the variables the
	// shell did not export, so that the source can be reported correctly.
	envKey := getenv(auth.EnvVar)
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
	cred, err := resolveKey(envKey, groot)
	if err != nil {
		fmt.Fprintf(stderr, "groot: %v\n", err)
		return 1
	}
	for _, w := range cred.Warnings {
		fmt.Fprintf(stderr, "warning: %s\n", w)
	}
	if cred.Key == "" {
		fmt.Fprintln(stderr, "groot: no Anthropic API key found.")
		fmt.Fprintln(stderr, "  Run: groot auth login")
		fmt.Fprintln(stderr, "  Or export ANTHROPIC_API_KEY=<your key> in this shell, or put it in a .env file in the working directory.")
		fmt.Fprintln(stderr, "  groot never reads the key from groot.yaml.")
		return 1
	}

	cfg, err := loadConfig(explicitFlag(fs, "config"), *cfgPath, filepath.Join(groot, "groot.yaml"))
	if err != nil {
		fmt.Fprintf(stderr, "groot: %v\n", err)
		return 1
	}
	if *model != "" {
		cfg.Provider.Model = *model
	}
	if *policy != "" {
		cfg.Gate.Policy = *policy
	}
	if err := cfg.Validate(); err != nil {
		fmt.Fprintf(stderr, "groot: %v\n", err)
		return 1
	}

	if err := runApp(cfg, *cfgPath, groot, cred.Key); err != nil {
		fmt.Fprintf(stderr, "groot: %v\n", err)
		return 1
	}
	return 0
}

// resolveKey finds the Anthropic key for groot home groot. envKey is the
// variable as the shell had it, before .env was loaded.
func resolveKey(envKey, groot string) (auth.Credential, error) {
	cwd, err := os.Getwd()
	if err != nil {
		return auth.Credential{}, fmt.Errorf("working directory: %w", err)
	}
	return auth.Resolve(auth.Options{EnvKey: envKey, Cwd: cwd, Home: groot, Keychain: newKeychain()})
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

// runApp resolves the groot home, bootstraps it on first run, builds the
// workspace, and runs the terminal UI.
func runApp(cfg config.Config, cfgPath, groot, apiKey string) error {
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
			OnInbox:    bridge.Inbox,
			OnAuto:     bridge.Auto,
		},
		NewProvider: func(model string) provider.Provider {
			return provider.NewAnthropic(provider.AnthropicOptions{Model: model, APIKey: apiKey, Effort: cfg.Provider.Effort})
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
