package workspace

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tinchoram/groot-cli/internal/config"
)

func TestSetDefaultModelWritesTheConfigAndUpdatesTheDefault(t *testing.T) {
	f := newFixture(t, text("ok"))
	path := filepath.Join(t.TempDir(), "groot.yaml")
	f.ws.opts.ConfigPath = path
	f.mock.Models = knownModels
	ctx := context.Background()

	if _, err := f.ws.SetDefaultModel(ctx, "claude-haiku-5-5", false); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Provider.Model != "claude-haiku-5-5" || f.ws.DefaultModel() != "claude-haiku-5-5" {
		t.Fatalf("file model = %q, default = %q", cfg.Provider.Model, f.ws.DefaultModel())
	}
	if _, err := f.ws.SetDefaultModel(ctx, "not-listed", false); err == nil {
		t.Fatal("an unknown id is refused")
	}
	if cfg, _ := config.Load(path); cfg.Provider.Model != "claude-haiku-5-5" {
		t.Fatalf("a refused id must not reach the file: %q", cfg.Provider.Model)
	}
}

func TestDefaultModelFallsBackToTheProviderDefault(t *testing.T) {
	f := newFixture(t, text("ok"))
	if got := f.ws.DefaultModel(); got == "" {
		t.Fatal("with no configured model the provider default is reported")
	}
}

func TestGrootPromptPointsAtListModels(t *testing.T) {
	if !strings.Contains(grootPrompt, "When asked about models, use list_models") {
		t.Fatal("groot's prompt must say where model questions are answered")
	}
}
