package workspace

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/tinchoram/groot-cli/internal/catalog"
	"github.com/tinchoram/groot-cli/internal/provider"
)

func agentModel(t *testing.T, home, name string) string {
	t.Helper()
	defs, err := catalog.Load(home)
	if err != nil {
		t.Fatal(err)
	}
	for _, d := range defs {
		if d.Name == name {
			return d.Model
		}
	}
	t.Fatalf("agent %s not found", name)
	return ""
}

func TestSetAgentModelChecksTheListAndSavesTheFile(t *testing.T) {
	f := newFixture(t, text("ok"))
	f.mock.Models = []provider.ModelInfo{{ID: "claude-haiku-5-5", DisplayName: "Haiku"}}
	ctx := context.Background()

	warn, err := f.ws.SetAgentModel(ctx, "reader", "claude-haiku-5-5")
	if err != nil || warn != "" {
		t.Fatalf("listed model: warn=%q err=%v", warn, err)
	}
	if got := agentModel(t, f.home, "reader"); got != "claude-haiku-5-5" {
		t.Fatalf("saved model = %q", got)
	}
	if _, err := f.ws.SetAgentModel(ctx, "reader", "not-listed"); err == nil || !strings.Contains(err.Error(), "unknown model") {
		t.Fatalf("an unlisted model must be refused: %v", err)
	}
	if _, err := f.ws.SetAgentModel(ctx, "nobody", "claude-haiku-5-5"); err == nil {
		t.Fatal("an unknown agent must be refused")
	}
}

func TestSetAgentModelWarnsWhenTheListIsUnreachable(t *testing.T) {
	f := newFixture(t, text("ok"))
	f.mock.ModelsErr = errors.New("offline")

	warn, err := f.ws.SetAgentModel(context.Background(), "reader", "claude-x")
	if err != nil {
		t.Fatalf("an unreachable list must not block the change: %v", err)
	}
	if !strings.Contains(warn, "offline") {
		t.Fatalf("warning = %q", warn)
	}
	if got := agentModel(t, f.home, "reader"); got != "claude-x" {
		t.Fatalf("saved model = %q", got)
	}
}
