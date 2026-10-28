package space

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func openWithSpace(t *testing.T) (*Store, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "spaces.yaml")
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Add("work", t.TempDir(), time.Now()); err != nil {
		t.Fatal(err)
	}
	return s, path
}

func boolPtr(v bool) *bool { return &v }

// A new space follows the global network setting: no override is stored.
func TestNewSpaceHasNoNetworkOverride(t *testing.T) {
	s, _ := openWithSpace(t)
	sp, _ := s.Get("work")
	if sp.Network != nil {
		t.Fatalf("Network = %v, want nil (the global value)", *sp.Network)
	}
}

// An override is saved in spaces.yaml, read back, and cleared with nil.
func TestSetNetworkPersistsAndClears(t *testing.T) {
	s, path := openWithSpace(t)
	if err := s.SetNetwork("work", boolPtr(false)); err != nil {
		t.Fatalf("SetNetwork: %v", err)
	}
	raw, _ := os.ReadFile(path)
	if !strings.Contains(string(raw), "network: false") {
		t.Fatalf("spaces.yaml lacks the override:\n%s", raw)
	}

	reopened, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	sp, _ := reopened.Get("work")
	if sp.Network == nil || *sp.Network {
		t.Fatalf("after reopen Network = %v, want false", sp.Network)
	}

	if err := reopened.SetNetwork("work", nil); err != nil {
		t.Fatalf("clear: %v", err)
	}
	sp, _ = reopened.Get("work")
	if sp.Network != nil {
		t.Fatalf("cleared Network = %v, want nil", *sp.Network)
	}
}

// Get returns a copy: changing it does not change the store.
func TestNetworkOverrideIsCopiedOnRead(t *testing.T) {
	s, _ := openWithSpace(t)
	if err := s.SetNetwork("work", boolPtr(true)); err != nil {
		t.Fatal(err)
	}
	sp, _ := s.Get("work")
	*sp.Network = false
	again, _ := s.Get("work")
	if again.Network == nil || !*again.Network {
		t.Fatal("a copy of the space must not share the network override")
	}
}

func TestSetNetworkNamesAnUnknownSpace(t *testing.T) {
	s, _ := openWithSpace(t)
	if err := s.SetNetwork("ghost", boolPtr(true)); err == nil {
		t.Fatal("SetNetwork on an unknown space must fail")
	}
}
