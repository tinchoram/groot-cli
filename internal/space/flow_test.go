package space

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const spacesWithOverrides = `active: payments
spaces:
  - name: payments
    path: ` + "/work/payments" + `
    agents: [reviewer]
    created_at: 2026-10-24T09:00:00Z
    model: claude-haiku-5-5
    flow:
      mode: advisory
      routes:
        small:
          model: claude-haiku-5-5
        medium:
          model: claude-sonnet-5-5
          agent: reviewer
        large:
          model: claude-opus-5-5
`

func writeFile(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestFlowAndModelBlockRoundTrips(t *testing.T) {
	path := filepath.Join(t.TempDir(), "home", "spaces.yaml")
	writeFile(t, path, spacesWithOverrides)

	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	sp, ok := s.Get("payments")
	if !ok {
		t.Fatal("payments not loaded")
	}
	if sp.Model != "claude-haiku-5-5" || sp.Flow.Mode != "advisory" {
		t.Fatalf("model=%q mode=%q", sp.Model, sp.Flow.Mode)
	}
	if got := sp.Flow.Routes.Medium; got.Model != "claude-sonnet-5-5" || got.Agent != "reviewer" {
		t.Fatalf("medium route = %+v", got)
	}
	if got := sp.Flow.Routes.Small; got.Model != "claude-haiku-5-5" || got.Agent != "" {
		t.Fatalf("small route = %+v", got)
	}

	// An unrelated save must keep the block.
	if err := s.Assign("payments", "builder"); err != nil {
		t.Fatal(err)
	}
	again, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	sp, _ = again.Get("payments")
	if sp.Model != "claude-haiku-5-5" || sp.Flow.Mode != "advisory" ||
		sp.Flow.Routes.Medium.Model != "claude-sonnet-5-5" || sp.Flow.Routes.Medium.Agent != "reviewer" ||
		sp.Flow.Routes.Large.Model != "claude-opus-5-5" {
		t.Fatalf("block lost on save: %+v", sp)
	}
}

func TestFilesWithoutTheBlockKeepWorking(t *testing.T) {
	path := filepath.Join(t.TempDir(), "spaces.yaml")
	dir := t.TempDir()
	writeFile(t, path, "active: default\nspaces:\n  - name: default\n    path: "+dir+"\n    agents: [reviewer]\n")

	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	sp, _ := s.Get("default")
	if sp.Model != "" || sp.Flow != (SpaceFlow{}) {
		t.Fatalf("an old file must read as no overrides: %+v", sp)
	}
	if err := s.Assign("default", "builder"); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "flow:") || strings.Contains(string(raw), "model:") {
		t.Fatalf("empty overrides must not be written:\n%s", raw)
	}
}

func TestSetRouteAndSetModelPersistAndClear(t *testing.T) {
	path := filepath.Join(t.TempDir(), "spaces.yaml")
	writeFile(t, path, spacesWithOverrides)
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}

	if err := s.SetRoute("payments", "small", "claude-sonnet-5-5", "builder"); err != nil {
		t.Fatalf("SetRoute: %v", err)
	}
	if err := s.SetModel("payments", "claude-opus-5-5"); err != nil {
		t.Fatalf("SetModel: %v", err)
	}
	reopened, _ := Open(path)
	sp, _ := reopened.Get("payments")
	if sp.Flow.Routes.Small != (SpaceRoute{Model: "claude-sonnet-5-5", Agent: "builder"}) || sp.Model != "claude-opus-5-5" {
		t.Fatalf("changes not persisted: %+v", sp)
	}

	// Empty values clear the field, so the global value applies again.
	if err := s.SetRoute("payments", "medium", "", ""); err != nil {
		t.Fatal(err)
	}
	if err := s.SetModel("payments", ""); err != nil {
		t.Fatal(err)
	}
	cleared, _ := Open(path)
	sp, _ = cleared.Get("payments")
	if sp.Flow.Routes.Medium != (SpaceRoute{}) || sp.Model != "" {
		t.Fatalf("clearing failed: %+v", sp)
	}
}

func TestSetFlowModePersistsAndClears(t *testing.T) {
	path := filepath.Join(t.TempDir(), "spaces.yaml")
	writeFile(t, path, spacesWithOverrides)
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.SetFlowMode("payments", "strict"); err != nil {
		t.Fatal(err)
	}
	reopened, _ := Open(path)
	if sp, _ := reopened.Get("payments"); sp.Flow.Mode != "strict" {
		t.Fatalf("mode not persisted: %q", sp.Flow.Mode)
	}
	if err := s.SetFlowMode("payments", ""); err != nil {
		t.Fatal(err)
	}
	if sp, _ := s.Get("payments"); sp.Flow.Mode != "" {
		t.Fatalf("clear failed: %q", sp.Flow.Mode)
	}
	if err := s.SetFlowMode("missing", "strict"); err == nil {
		t.Error("an unknown space must fail")
	}
}

func TestSetRouteAndSetModelRefuseBadInput(t *testing.T) {
	s, _ := openTemp(t)
	_ = s.Add("payments", t.TempDir(), now)
	if err := s.SetRoute("payments", "huge", "m", ""); err == nil {
		t.Error("an unknown size must fail")
	}
	if err := s.SetRoute("missing", "small", "m", ""); err == nil {
		t.Error("an unknown space must fail")
	}
	if err := s.SetModel("missing", "m"); err == nil {
		t.Error("an unknown space must fail")
	}
}

func TestValidSize(t *testing.T) {
	for _, ok := range []string{"small", "medium", "large"} {
		if !ValidSize(ok) {
			t.Errorf("%s must be a size", ok)
		}
	}
	for _, bad := range []string{"", "Small", "huge"} {
		if ValidSize(bad) {
			t.Errorf("%q must not be a size", bad)
		}
	}
}
