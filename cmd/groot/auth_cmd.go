package main

import (
	"bufio"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"

	"golang.org/x/term"

	"github.com/tinchoram/groot-cli/internal/auth"
	"github.com/tinchoram/groot-cli/internal/home"
)

const authUsage = `usage: groot auth <command> [--provider anthropic|gemini]

  login    store the API key (prompts, or reads --stdin)
  status   show where the key comes from, and its last four characters
  logout   remove the stored key from the keychain and the credentials file

  --provider selects the key: anthropic (the default) or gemini.
`

// runAuth handles `groot auth`. The key itself is never printed.
func runAuth(args []string, stdin io.Reader, stdout, stderr io.Writer, getenv func(string) string) int {
	if len(args) == 0 {
		fmt.Fprint(stderr, authUsage)
		return 2
	}
	groot, err := home.Resolve(getenv, os.UserHomeDir)
	if err != nil {
		fmt.Fprintf(stderr, "groot: %v\n", err)
		return 1
	}
	switch args[0] {
	case "login":
		return authLogin(args[1:], stdin, stdout, stderr, groot)
	case "status":
		return authStatus(args[1:], stdout, stderr, getenv, groot)
	case "logout":
		return authLogout(args[1:], stdout, stderr, groot)
	}
	fmt.Fprintf(stderr, "groot: unknown auth command %q\n", args[0])
	fmt.Fprint(stderr, authUsage)
	return 2
}

// providerFlag registers --provider on fs and returns a function that reads it
// back as a service after Parse.
func providerFlag(fs *flag.FlagSet) func() (auth.Service, error) {
	name := fs.String("provider", "anthropic", "provider whose key to use: anthropic or gemini")
	return func() (auth.Service, error) {
		switch *name {
		case "anthropic":
			return auth.Anthropic, nil
		case "gemini":
			return auth.Gemini, nil
		}
		return auth.Service{}, fmt.Errorf("unknown provider %q (use anthropic or gemini)", *name)
	}
}

// keychainFor is the keychain item of svc. Tests replace the two constructors.
func keychainFor(svc auth.Service) auth.Keychain {
	if svc.Keychain == auth.Gemini.Keychain {
		return newGeminiKeychain()
	}
	return newKeychain()
}

func authLogin(args []string, stdin io.Reader, stdout, stderr io.Writer, groot string) int {
	fs := flag.NewFlagSet("groot auth login", flag.ContinueOnError)
	fs.SetOutput(stderr)
	fromStdin := fs.Bool("stdin", false, "read the key from the first line of standard input")
	service := providerFlag(fs)
	if err := fs.Parse(args); err != nil {
		return 2
	}
	svc, err := service()
	if err != nil {
		fmt.Fprintf(stderr, "groot: %v\n", err)
		return 2
	}

	var key string
	switch f, isFile := stdin.(*os.File); {
	case *fromStdin:
		line, err := bufio.NewReader(stdin).ReadString('\n')
		if err != nil && !errors.Is(err, io.EOF) {
			fmt.Fprintf(stderr, "groot: read the key: %v\n", err)
			return 1
		}
		key = line
	case isFile && term.IsTerminal(int(f.Fd())):
		fmt.Fprintf(stderr, "%s API key: ", svc.Name)
		raw, err := term.ReadPassword(int(f.Fd()))
		fmt.Fprintln(stderr)
		if err != nil {
			fmt.Fprintf(stderr, "groot: read the key: %v\n", err)
			return 1
		}
		key = string(raw)
	default:
		fmt.Fprintln(stderr, "groot: auth login reads the key from a terminal, or from standard input with --stdin.")
		fmt.Fprintf(stderr, "  Example: echo \"$KEY\" | groot auth login --stdin --provider %s\n", strings.ToLower(svc.Name))
		return 2
	}

	key = strings.TrimSpace(key)
	if err := auth.ValidKey(key); err != nil {
		fmt.Fprintf(stderr, "groot: %v\n", err)
		return 1
	}
	where, err := auth.LoginFor(svc, groot, keychainFor(svc), key)
	if err != nil {
		fmt.Fprintf(stderr, "groot: %v\n", err)
		return 1
	}
	if where.Store == auth.StoreKeychain {
		fmt.Fprintf(stdout, "%s API key stored in the macOS keychain.\n", svc.Name)
	} else {
		fmt.Fprintf(stdout, "%s API key stored in %s (mode 0600).\n", svc.Name, where.Path)
	}
	return 0
}

func authStatus(args []string, stdout, stderr io.Writer, getenv func(string) string, groot string) int {
	fs := flag.NewFlagSet("groot auth status", flag.ContinueOnError)
	fs.SetOutput(stderr)
	service := providerFlag(fs)
	if err := fs.Parse(args); err != nil {
		return 2
	}
	svc, err := service()
	if err != nil {
		fmt.Fprintf(stderr, "groot: %v\n", err)
		return 2
	}
	cwd, err := os.Getwd()
	if err != nil {
		fmt.Fprintf(stderr, "groot: working directory: %v\n", err)
		return 1
	}
	cred, err := resolveService(svc, getenv(svc.EnvVar), groot, cwd, keychainFor(svc))
	if err != nil {
		fmt.Fprintf(stderr, "groot: %v\n", err)
		return 1
	}
	for _, w := range cred.Warnings {
		fmt.Fprintf(stderr, "warning: %s\n", w)
	}
	if cred.Key == "" {
		fmt.Fprintln(stdout, "source: none")
		fmt.Fprintf(stdout, "Run: groot auth login --provider %s\n", strings.ToLower(svc.Name))
		return 0
	}
	fmt.Fprintf(stdout, "source: %s\n", auth.Label(cred))
	fmt.Fprintf(stdout, "key:    %s\n", auth.Mask(cred.Key))
	return 0
}

func authLogout(args []string, stdout, stderr io.Writer, groot string) int {
	fs := flag.NewFlagSet("groot auth logout", flag.ContinueOnError)
	fs.SetOutput(stderr)
	service := providerFlag(fs)
	if err := fs.Parse(args); err != nil {
		return 2
	}
	svc, err := service()
	if err != nil {
		fmt.Fprintf(stderr, "groot: %v\n", err)
		return 2
	}
	removed, err := auth.LogoutFor(svc, groot, keychainFor(svc))
	if err != nil {
		fmt.Fprintf(stderr, "groot: %v\n", err)
		return 1
	}
	if len(removed) == 0 {
		fmt.Fprintf(stdout, "nothing stored to remove. A %s in the environment or in .env is not touched.\n", svc.EnvVar)
		return 0
	}
	for _, r := range removed {
		fmt.Fprintf(stdout, "removed %s\n", r)
	}
	return 0
}
