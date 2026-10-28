package main

import (
	"errors"
	"strings"
	"testing"

	"github.com/tinchoram/groot-cli/internal/auth"
	"github.com/tinchoram/groot-cli/internal/provider"
)

// isolateKeys points the key stores at fakes and the working directory at an
// empty temporary directory, so resolveKeys reads nothing from the machine.
func isolateKeys(t *testing.T) {
	t.Helper()
	t.Chdir(t.TempDir())
	prevA, prevG := newKeychain, newGeminiKeychain
	newKeychain = func() auth.Keychain { return &fakeKeychain{} }
	newGeminiKeychain = func() auth.Keychain { return &fakeKeychain{} }
	t.Cleanup(func() { newKeychain, newGeminiKeychain = prevA, prevG })
}

func TestGeminiDefaultNeedsOnlyTheGeminiKey(t *testing.T) {
	isolateKeys(t)
	keys, _, err := resolveKeys(keyEnv{gemini: "gem-key"}, t.TempDir(), provider.NameGemini)
	if err != nil {
		t.Fatalf("a Gemini default with only a Gemini key must start: %v", err)
	}
	if keys.gemini != "gem-key" || keys.anthropic != "" {
		t.Fatalf("keys = %+v", keys)
	}
	if got := keys.keyed(); len(got) != 1 || got[0] != provider.NameGemini {
		t.Fatalf("keyed = %v, want gemini only", got)
	}
}

func TestAnthropicDefaultWithoutKeyNamesBothLoginCommands(t *testing.T) {
	isolateKeys(t)
	_, _, err := resolveKeys(keyEnv{gemini: "gem-key"}, t.TempDir(), "")
	var mk *missingKeyError
	if !errors.As(err, &mk) || mk.provider != provider.NameAnthropic {
		t.Fatalf("err = %v, want a missing Anthropic key", err)
	}
	if !strings.Contains(err.Error(), "groot auth login") || !strings.Contains(err.Error(), "ANTHROPIC_API_KEY") {
		t.Fatalf("hint = %q, want the Anthropic login and variable", err.Error())
	}
}

func TestGeminiDefaultWithoutKeyNamesTheGeminiLogin(t *testing.T) {
	isolateKeys(t)
	_, _, err := resolveKeys(keyEnv{anthropic: "ant-key"}, t.TempDir(), provider.NameGemini)
	if err == nil || !strings.Contains(err.Error(), "groot auth login --provider gemini") || !strings.Contains(err.Error(), "GEMINI_API_KEY") {
		t.Fatalf("err = %v, want the Gemini login and variable", err)
	}
}

func TestBothKeysAreKeptWhenBothAreSet(t *testing.T) {
	isolateKeys(t)
	keys, _, err := resolveKeys(keyEnv{anthropic: "ant-key", gemini: "gem-key"}, t.TempDir(), provider.NameAnthropic)
	if err != nil {
		t.Fatal(err)
	}
	if got := keys.keyed(); len(got) != 2 || got[0] != provider.NameAnthropic || got[1] != provider.NameGemini {
		t.Fatalf("keyed = %v, want both in catalog order", got)
	}
}
