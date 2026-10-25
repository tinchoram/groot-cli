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
	"time"

	"github.com/tinchoram/groot-cli/internal/config"
	"github.com/tinchoram/groot-cli/internal/gate"
	"github.com/tinchoram/groot-cli/internal/home"
	"github.com/tinchoram/groot-cli/internal/provider"
	"github.com/tinchoram/groot-cli/internal/sandbox"
	"github.com/tinchoram/groot-cli/internal/space"
	"github.com/tinchoram/groot-cli/internal/ui"
	"github.com/tinchoram/groot-cli/internal/workspace"
)

// version is overridden at build time with -ldflags "-X main.version=...".
var version = "0.5.4-dev"

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr, os.Getenv))
}

func run(args []string, stdout, stderr io.Writer, getenv func(string) string) int {
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

	// A .env in the working directory fills in variables the shell did not export.
	if err := config.LoadDotEnv(".env"); err != nil {
		fmt.Fprintf(stderr, "groot: %v\n", err)
		return 1
	}

	if getenv("ANTHROPIC_API_KEY") == "" {
		fmt.Fprintln(stderr, "groot: ANTHROPIC_API_KEY is not set.")
		fmt.Fprintln(stderr, "  Set it in this shell or in a .env file in the working directory:")
		fmt.Fprintln(stderr, "    export ANTHROPIC_API_KEY=<your key>")
		fmt.Fprintln(stderr, "  groot reads the key only from the environment or .env. Do not put it in groot.yaml.")
		return 1
	}

	cfg, err := loadConfig(fs, *cfgPath)
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

	if err := runApp(cfg, *cfgPath, getenv); err != nil {
		fmt.Fprintf(stderr, "groot: %v\n", err)
		return 1
	}
	return 0
}

// loadConfig loads the file. A path set explicitly on the command line must
// exist; the default groot.yaml is optional.
func loadConfig(fs *flag.FlagSet, path string) (config.Config, error) {
	explicit := false
	fs.Visit(func(f *flag.Flag) {
		if f.Name == "config" {
			explicit = true
		}
	})
	if explicit {
		if _, err := os.Stat(path); err != nil {
			return config.Config{}, fmt.Errorf("config file %s: %w", path, err)
		}
	}
	return config.Load(path)
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
func runApp(cfg config.Config, cfgPath string, getenv func(string) string) error {
	groot, err := home.Resolve(getenv, os.UserHomeDir)
	if err != nil {
		return err
	}
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
		Home:       groot,
		UserHome:   userHome,
		Version:    version,
		Sandbox:    runner,
		Config:     cfg,
		ConfigPath: cfgPath,
		Spaces:     spaces,
		Gate:       g,
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
			return provider.NewAnthropic(provider.AnthropicOptions{Model: model})
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
	})
}
