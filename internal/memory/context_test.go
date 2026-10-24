package memory

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"
	"time"

	"github.com/tinchoram/groot-cli/internal/api"
)

func TestContextTokensArePersistedAndReadBack(t *testing.T) {
	ctx := context.Background()
	s := openTempSQLite(t)
	if err := s.StartSession(ctx, Session{ID: "c-1", Agent: "reader", StartedAt: time.Now().UTC()}); err != nil {
		t.Fatal(err)
	}
	if err := s.SetContextTokens(ctx, "c-1", 84_000); err != nil {
		t.Fatal(err)
	}
	got, ok, err := s.LatestSession(ctx, "reader")
	if err != nil || !ok {
		t.Fatalf("latest session: ok=%v err=%v", ok, err)
	}
	if got.ContextTokens != 84_000 {
		t.Fatalf("ContextTokens = %d, want 84000", got.ContextTokens)
	}
}

func TestNewSessionHasNoContextYet(t *testing.T) {
	ctx := context.Background()
	s := openTempSQLite(t)
	if err := s.StartSession(ctx, Session{ID: "c-2", Agent: "reader", StartedAt: time.Now().UTC()}); err != nil {
		t.Fatal(err)
	}
	got, _, _ := s.LatestSession(ctx, "reader")
	if got.ContextTokens != 0 {
		t.Fatalf("a new session reports %d context tokens, want 0", got.ContextTokens)
	}
}

func TestNoMemoryKeepsNoContext(t *testing.T) {
	if err := (NoMemory{}).SetContextTokens(context.Background(), "x", 5); err != nil {
		t.Fatalf("NoMemory.SetContextTokens: %v", err)
	}
}

func TestOpenAddsContextColumnToOlderDatabases(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "groot.db")
	s, err := OpenSQLite(path)
	if err != nil {
		t.Fatal(err)
	}
	seedSession(t, s, "s-1", "reader", base, api.Message{Role: api.RoleUser,
		Content: []api.Block{{Type: api.BlockText, Text: "kept"}}})
	if err := s.SetContextTokens(ctx, "s-1", 1234); err != nil {
		t.Fatal(err)
	}
	_ = s.Close()

	// Simulate a database written before context_tokens existed.
	raw, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := raw.Exec(`ALTER TABLE sessions DROP COLUMN context_tokens`); err != nil {
		t.Fatal(err)
	}
	_ = raw.Close()

	again, err := OpenSQLite(path)
	if err != nil {
		t.Fatalf("reopen after dropping the column: %v", err)
	}
	defer again.Close()
	got, ok, err := again.LatestSession(ctx, "reader")
	if err != nil || !ok || got.ID != "s-1" || got.ContextTokens != 0 {
		t.Fatalf("after migration: %+v ok=%v err=%v", got, ok, err)
	}
	if err := again.SetContextTokens(ctx, "s-1", 99); err != nil {
		t.Fatalf("write after migration: %v", err)
	}
}
