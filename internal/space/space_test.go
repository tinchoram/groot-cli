package space

import (
	"path/filepath"
	"strings"
	"testing"
	"time"
)

var now = time.Date(2026, 10, 24, 9, 0, 0, 0, time.UTC)

func openTemp(t *testing.T) (*Store, string) {
	t.Helper()
	p := filepath.Join(t.TempDir(), "home", "spaces.yaml")
	s, err := Open(p)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	return s, p
}

func TestAddUseAndCurrent(t *testing.T) {
	s, _ := openTemp(t)
	dir := t.TempDir()
	if _, ok := s.Current(); ok {
		t.Fatal("empty store has no current space")
	}
	if err := s.Add("payments", dir, now); err != nil {
		t.Fatalf("Add: %v", err)
	}
	if err := s.Use("payments"); err != nil {
		t.Fatalf("Use: %v", err)
	}
	cur, ok := s.Current()
	if !ok || cur.Name != "payments" || cur.Path != dir {
		t.Fatalf("current = %+v ok=%v", cur, ok)
	}
	if !cur.CreatedAt.Equal(now) {
		t.Fatalf("created_at not recorded: %v", cur.CreatedAt)
	}
}

func TestStateSurvivesReopen(t *testing.T) {
	s, path := openTemp(t)
	dir := t.TempDir()
	_ = s.Add("default", dir, now)
	_ = s.Assign("default", "devjunior")
	_ = s.Use("default")

	again, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	cur, ok := again.Current()
	if !ok || cur.Name != "default" || len(cur.Agents) != 1 || cur.Agents[0] != "devjunior" {
		t.Fatalf("reopened state wrong: %+v", cur)
	}
}

func TestAddRejectsDuplicatesBadNamesAndMissingPaths(t *testing.T) {
	s, _ := openTemp(t)
	dir := t.TempDir()
	if err := s.Add("ok", dir, now); err != nil {
		t.Fatal(err)
	}
	if err := s.Add("ok", dir, now); err == nil {
		t.Error("duplicate name must fail")
	}
	if err := s.Add("Bad Name", dir, now); err == nil {
		t.Error("invalid slug must fail")
	}
	if err := s.Add("gone", filepath.Join(dir, "missing"), now); err == nil {
		t.Error("nonexistent path must fail")
	}
	if err := s.Add("relative", "some/relative/path", now); err == nil {
		t.Error("relative path must fail")
	}
}

func TestUseUnknownSpaceFails(t *testing.T) {
	s, _ := openTemp(t)
	if err := s.Use("nope"); err == nil {
		t.Fatal("using an unknown space must fail")
	}
}

func TestAssignAndUnassign(t *testing.T) {
	s, _ := openTemp(t)
	_ = s.Add("default", t.TempDir(), now)
	if err := s.Assign("default", "devjunior"); err != nil {
		t.Fatal(err)
	}
	if err := s.Assign("default", "devjunior"); err == nil {
		t.Fatal("double assign must fail")
	}
	if err := s.Assign("missing", "devjunior"); err == nil {
		t.Fatal("assigning in an unknown space must fail")
	}
	if err := s.Unassign("default", "devjunior"); err != nil {
		t.Fatal(err)
	}
	if err := s.Unassign("default", "devjunior"); err == nil {
		t.Fatal("unassigning something not assigned must fail")
	}
	sp, _ := s.Get("default")
	if len(sp.Agents) != 0 {
		t.Fatalf("agents left behind: %v", sp.Agents)
	}
}

func TestListReturnsCopies(t *testing.T) {
	s, _ := openTemp(t)
	_ = s.Add("a", t.TempDir(), now)
	_ = s.Add("b", t.TempDir(), now)
	list := s.List()
	names := []string{list[0].Name, list[1].Name}
	if strings.Join(names, ",") != "a,b" {
		t.Fatalf("order %v", names)
	}
	list[0].Name = "mutated"
	if got, _ := s.Get("a"); got.Name != "a" {
		t.Fatal("List must not expose internal state")
	}
}

func TestEnsureFirstCreatesOnceAtFront(t *testing.T) {
	s, _ := openTemp(t)
	_ = s.Add("default", t.TempDir(), now)
	root := t.TempDir()
	created, err := s.EnsureFirst("root", root, now)
	if err != nil || !created {
		t.Fatalf("first EnsureFirst = %v, %v", created, err)
	}
	created, err = s.EnsureFirst("root", root, now)
	if err != nil || created {
		t.Fatalf("second EnsureFirst must be a no-op: %v, %v", created, err)
	}
	list := s.List()
	if len(list) != 2 || list[0].Name != "root" || list[1].Name != "default" {
		t.Fatalf("root must be first: %+v", list)
	}
}
