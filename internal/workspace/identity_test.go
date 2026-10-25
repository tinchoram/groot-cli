package workspace

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tinchoram/groot-cli/internal/config"
)

func TestOperatorNameComesFromConfig(t *testing.T) {
	f := newFixture(t)
	if got := f.ws.OperatorName(); got != "" {
		t.Fatalf("no name configured, OperatorName = %q", got)
	}

	cfg := config.Default()
	cfg.User.Name = "Tinchoram"
	f.ws.opts.Config = cfg
	f.ws.operator = cfg.User.Name
	if got := f.ws.OperatorName(); got != "Tinchoram" {
		t.Fatalf("OperatorName = %q, want Tinchoram", got)
	}
}

func TestSetOperatorNameSavesUserFileAndRefreshesOpenPrompts(t *testing.T) {
	f := newFixture(t, text("first"), text("second"))
	f.ws.opts.UserConfigPath = filepath.Join(f.home, "user.yaml")
	if err := f.ws.SelectAgent("reader"); err != nil {
		t.Fatal(err)
	}
	if _, err := f.ws.Chat().Send(context.Background(), "hi"); err != nil {
		t.Fatal(err)
	}

	name, err := f.ws.SetOperatorName("@Tinchoram")
	if err != nil {
		t.Fatalf("SetOperatorName: %v", err)
	}
	if name != "Tinchoram" || f.ws.OperatorName() != "Tinchoram" {
		t.Fatalf("name = %q, OperatorName = %q", name, f.ws.OperatorName())
	}
	raw, err := os.ReadFile(filepath.Join(f.home, "user.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if string(raw) != "name: Tinchoram\n" {
		t.Fatalf("user.yaml = %q", raw)
	}

	if _, err := f.ws.Chat().Send(context.Background(), "again"); err != nil {
		t.Fatal(err)
	}
	if sys := f.mock.Calls[1].System; !strings.Contains(sys, "The operator is @Tinchoram.") {
		t.Fatalf("open conversation kept the old prompt; People block missing:\n%s", sys)
	}
}

func TestSetOperatorNameRefusesInvalidNameAndKeepsState(t *testing.T) {
	f := newFixture(t)
	f.ws.opts.UserConfigPath = filepath.Join(f.home, "user.yaml")

	if _, err := f.ws.SetOperatorName("no way"); err == nil {
		t.Fatal("invalid name accepted")
	}
	if f.ws.OperatorName() != "" {
		t.Fatalf("OperatorName changed after a refused name: %q", f.ws.OperatorName())
	}
	if _, err := os.Stat(filepath.Join(f.home, "user.yaml")); !os.IsNotExist(err) {
		t.Fatalf("user.yaml written for a refused name: %v", err)
	}
}

func TestSetOperatorNameNeedsAFile(t *testing.T) {
	f := newFixture(t)
	if _, err := f.ws.SetOperatorName("Tinchoram"); err == nil {
		t.Fatal("without a user.yaml path the name cannot be saved")
	}
}

func TestSystemPromptPeopleBlock(t *testing.T) {
	f := newFixture(t, text("named"), text("anonymous"))
	f.ws.operator = "Tinchoram"
	_ = f.ws.SelectAgent("reader")
	if _, err := f.ws.Chat().Send(context.Background(), "hi"); err != nil {
		t.Fatal(err)
	}
	sys := f.mock.Calls[0].System
	for _, want := range []string{"## People", "The operator is @Tinchoram.", "@Groot is the coordinator agent"} {
		if !strings.Contains(sys, want) {
			t.Fatalf("system prompt lacks %q:\n%s", want, sys)
		}
	}

	f.ws.operator = ""
	f.ws.opts.UserConfigPath = filepath.Join(f.home, "user.yaml")
	if err := f.ws.refreshPrompts(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, err := f.ws.Chat().Send(context.Background(), "again"); err != nil {
		t.Fatal(err)
	}
	sys = f.mock.Calls[1].System
	if strings.Contains(sys, "The operator is") {
		t.Fatalf("no name set, the operator sentence must be omitted:\n%s", sys)
	}
	if !strings.Contains(sys, "@Groot is the coordinator agent") {
		t.Fatalf("the People block must stay without a name:\n%s", sys)
	}
}
