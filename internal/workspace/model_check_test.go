package workspace

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/tinchoram/groot-cli/internal/provider"
)

var knownModels = []provider.ModelInfo{
	{ID: "claude-sonnet-5-5", DisplayName: "Sonnet"},
	{ID: "claude-haiku-5-5", DisplayName: "Haiku"},
	{ID: "claude-opus-5-5", DisplayName: "Opus"},
}

func TestCheckModelRefusesUnknownIdWithSuggestions(t *testing.T) {
	f := newFixture(t, text("ok"))
	f.mock.Models = knownModels

	_, err := f.ws.CheckModel(context.Background(), "claude-sonet-5-5", false)
	if err == nil {
		t.Fatal("an unknown id must be refused")
	}
	if !strings.Contains(err.Error(), "claude-sonnet-5-5") || !strings.Contains(err.Error(), "--force") {
		t.Fatalf("the refusal names the closest id and the force flag: %v", err)
	}
	if _, err := f.ws.CheckModel(context.Background(), "claude-sonnet-5-5", false); err != nil {
		t.Fatalf("a listed id is accepted: %v", err)
	}
}

func TestCheckModelForceSkipsValidation(t *testing.T) {
	f := newFixture(t, text("ok"))
	f.mock.Models = knownModels
	if _, err := f.ws.CheckModel(context.Background(), "not-listed", true); err != nil {
		t.Fatalf("force accepts any id: %v", err)
	}
}

func TestCheckModelAcceptsWithWarningWhenListFails(t *testing.T) {
	f := newFixture(t, text("ok"))
	f.mock.ModelsErr = errors.New("offline")

	warn, err := f.ws.CheckModel(context.Background(), "claude-x", false)
	if err != nil {
		t.Fatalf("an unreachable list must not refuse the id: %v", err)
	}
	if !strings.Contains(warn, "offline") {
		t.Fatalf("warning = %q", warn)
	}
}

func TestModelListIsCachedForTenMinutes(t *testing.T) {
	f := newFixture(t, text("ok"))
	f.mock.Models = []provider.ModelInfo{{ID: "claude-b"}}
	if _, err := f.ws.CheckModel(context.Background(), "claude-b", false); err != nil {
		t.Fatal(err)
	}
	f.mock.Models = []provider.ModelInfo{{ID: "claude-a"}}
	if _, err := f.ws.CheckModel(context.Background(), "claude-b", false); err != nil {
		t.Fatalf("the second check must use the cached list: %v", err)
	}
}

func TestConfiguredModelIsCheckedAtStartup(t *testing.T) {
	f := newFixture(t, text("ok"))
	f.mock.Models = knownModels
	f.ws.opts.Config.Provider.Model = "claude-unlisted"
	// The configured model is not in the list: a warning, not a failure.
	if warn := f.ws.CheckConfiguredModel(context.Background()); warn == "" {
		t.Fatal("an unlisted configured model must produce a warning")
	}
}
