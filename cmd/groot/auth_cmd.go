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

const authUsage = `usage: groot auth <command>

  login    store the Anthropic API key (prompts, or reads --stdin)
  status   show where the key comes from, and its last four characters
  logout   remove the stored key from the keychain and the credentials file
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
		return authStatus(stdout, stderr, getenv(auth.EnvVar), groot)
	case "logout":
		return authLogout(stdout, stderr, groot)
	}
	fmt.Fprintf(stderr, "groot: unknown auth command %q\n", args[0])
	fmt.Fprint(stderr, authUsage)
	return 2
}

func authLogin(args []string, stdin io.Reader, stdout, stderr io.Writer, groot string) int {
	fs := flag.NewFlagSet("groot auth login", flag.ContinueOnError)
	fs.SetOutput(stderr)
	fromStdin := fs.Bool("stdin", false, "read the key from the first line of standard input")
	if err := fs.Parse(args); err != nil {
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
		fmt.Fprint(stderr, "Anthropic API key: ")
		raw, err := term.ReadPassword(int(f.Fd()))
		fmt.Fprintln(stderr)
		if err != nil {
			fmt.Fprintf(stderr, "groot: read the key: %v\n", err)
			return 1
		}
		key = string(raw)
	default:
		fmt.Fprintln(stderr, "groot: auth login reads the key from a terminal, or from standard input with --stdin.")
		fmt.Fprintln(stderr, "  Example: echo \"$KEY\" | groot auth login --stdin")
		return 2
	}

	key = strings.TrimSpace(key)
	if err := auth.ValidKey(key); err != nil {
		fmt.Fprintf(stderr, "groot: %v\n", err)
		return 1
	}
	where, err := auth.Login(groot, newKeychain(), key)
	if err != nil {
		fmt.Fprintf(stderr, "groot: %v\n", err)
		return 1
	}
	if where.Store == auth.StoreKeychain {
		fmt.Fprintln(stdout, "API key stored in the macOS keychain.")
	} else {
		fmt.Fprintf(stdout, "API key stored in %s (mode 0600).\n", where.Path)
	}
	return 0
}

func authStatus(stdout, stderr io.Writer, envKey, groot string) int {
	cred, err := resolveKey(envKey, groot)
	if err != nil {
		fmt.Fprintf(stderr, "groot: %v\n", err)
		return 1
	}
	for _, w := range cred.Warnings {
		fmt.Fprintf(stderr, "warning: %s\n", w)
	}
	if cred.Key == "" {
		fmt.Fprintln(stdout, "source: none")
		fmt.Fprintln(stdout, "Run: groot auth login")
		return 0
	}
	fmt.Fprintf(stdout, "source: %s\n", auth.Label(cred))
	fmt.Fprintf(stdout, "key:    %s\n", auth.Mask(cred.Key))
	return 0
}

func authLogout(stdout, stderr io.Writer, groot string) int {
	removed, err := auth.Logout(groot, newKeychain())
	if err != nil {
		fmt.Fprintf(stderr, "groot: %v\n", err)
		return 1
	}
	if len(removed) == 0 {
		fmt.Fprintln(stdout, "nothing stored to remove. An ANTHROPIC_API_KEY in the environment or in .env is not touched.")
		return 0
	}
	for _, r := range removed {
		fmt.Fprintf(stdout, "removed %s\n", r)
	}
	return 0
}
