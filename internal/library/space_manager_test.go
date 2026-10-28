package library

import (
	"strings"
	"testing"
)

// The space-manager answers status as a phase table, shows one phase on
// request, falls back to the backlog when there are no phases yet, and
// verifies what other agents report before relaying it.
func TestSpaceManagerPromptFixesTheStatusFormat(t *testing.T) {
	entries, err := readBuiltin()
	if err != nil {
		t.Fatal(err)
	}
	var prompt string
	for _, e := range entries {
		if e.Name == "space-manager" {
			prompt = e.Prompt
		}
	}
	if prompt == "" {
		t.Fatal("space-manager is not in the builtin library")
	}
	for _, want := range []string{
		"FASE 1 - <name>",
		"TASK 01 - <title>",
		"One phase asked by name or number",
		"No phases yet: the backlog",
		"confirmed, refuted or unverified",
	} {
		if !strings.Contains(prompt, want) {
			t.Errorf("space-manager prompt lacks %q", want)
		}
	}
}
