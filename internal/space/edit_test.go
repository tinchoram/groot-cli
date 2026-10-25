package space

import (
	"os"
	"path/filepath"
	"slices"
	"testing"
)

// reopen reads the store back from disk, the way a restart would.
func reopen(t *testing.T, path string) *Store {
	t.Helper()
	s, err := Open(path)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	return s
}

func TestRemoveDropsSpaceFromDisk(t *testing.T) {
	s, p := openTemp(t)
	if err := s.Add("a", t.TempDir(), now); err != nil {
		t.Fatal(err)
	}
	if err := s.Add("b", t.TempDir(), now); err != nil {
		t.Fatal(err)
	}
	if err := s.Use("a"); err != nil {
		t.Fatal(err)
	}
	if err := s.Remove("a"); err != nil {
		t.Fatalf("Remove: %v", err)
	}
	back := reopen(t, p)
	if _, ok := back.Get("a"); ok {
		t.Fatal("removed space is still on disk")
	}
	if _, ok := back.Get("b"); !ok {
		t.Fatal("other space was lost")
	}
	if _, ok := back.Current(); ok {
		t.Fatal("removing the active space must clear the selection")
	}
}

func TestRemoveUnknownSpaceFails(t *testing.T) {
	s, _ := openTemp(t)
	if err := s.Remove("ghost"); err == nil {
		t.Fatal("removing an unknown space must fail")
	}
}

func TestRenameMovesSpaceAndSelection(t *testing.T) {
	s, p := openTemp(t)
	dir := t.TempDir()
	if err := s.Add("a", dir, now); err != nil {
		t.Fatal(err)
	}
	if err := s.Assign("a", "reviewer"); err != nil {
		t.Fatal(err)
	}
	if err := s.Use("a"); err != nil {
		t.Fatal(err)
	}
	if err := s.Rename("a", "b"); err != nil {
		t.Fatalf("Rename: %v", err)
	}
	back := reopen(t, p)
	sp, ok := back.Get("b")
	if !ok || sp.Path != dir || !slices.Equal(sp.Agents, []string{"reviewer"}) {
		t.Fatalf("renamed space = %+v ok=%v", sp, ok)
	}
	if _, ok := back.Get("a"); ok {
		t.Fatal("old name is still on disk")
	}
	if cur, _ := back.Current(); cur.Name != "b" {
		t.Fatalf("selection = %q, want b", cur.Name)
	}
}

func TestRenameRefusesExistingAndInvalidNames(t *testing.T) {
	s, _ := openTemp(t)
	if err := s.Add("a", t.TempDir(), now); err != nil {
		t.Fatal(err)
	}
	if err := s.Add("b", t.TempDir(), now); err != nil {
		t.Fatal(err)
	}
	if err := s.Rename("a", "b"); err == nil {
		t.Fatal("renaming onto an existing space must fail")
	}
	if err := s.Rename("a", "Not A Slug"); err == nil {
		t.Fatal("an invalid name must fail")
	}
	if err := s.Rename("ghost", "c"); err == nil {
		t.Fatal("renaming an unknown space must fail")
	}
}

func TestSetPathChangesDirectoryOnDisk(t *testing.T) {
	s, p := openTemp(t)
	if err := s.Add("a", t.TempDir(), now); err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(t.TempDir(), "file.txt")
	if err := os.WriteFile(file, []byte("not a directory"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := s.SetPath("a", file); err == nil {
		t.Fatal("a file is not a valid space path")
	}
	dir := t.TempDir()
	if err := s.SetPath("a", dir); err != nil {
		t.Fatalf("SetPath: %v", err)
	}
	if sp, _ := reopen(t, p).Get("a"); sp.Path != dir {
		t.Fatalf("path on disk = %q, want %q", sp.Path, dir)
	}
	if err := s.SetPath("a", "relative"); err == nil {
		t.Fatal("a relative path must fail")
	}
}

func TestMoveAgentIsOneSave(t *testing.T) {
	s, p := openTemp(t)
	for _, n := range []string{"from", "to"} {
		if err := s.Add(n, t.TempDir(), now); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.Assign("from", "dev"); err != nil {
		t.Fatal(err)
	}
	if err := s.Assign("from", "other"); err != nil {
		t.Fatal(err)
	}
	if err := s.Move("dev", "from", "to"); err != nil {
		t.Fatalf("Move: %v", err)
	}
	back := reopen(t, p)
	from, _ := back.Get("from")
	to, _ := back.Get("to")
	if !slices.Equal(from.Agents, []string{"other"}) || !slices.Equal(to.Agents, []string{"dev"}) {
		t.Fatalf("after move from=%v to=%v", from.Agents, to.Agents)
	}
}

func TestMoveRefusesUnassignedAndAlreadyPresent(t *testing.T) {
	s, _ := openTemp(t)
	for _, n := range []string{"from", "to"} {
		if err := s.Add(n, t.TempDir(), now); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.Assign("from", "dev"); err != nil {
		t.Fatal(err)
	}
	if err := s.Assign("to", "dev"); err != nil {
		t.Fatal(err)
	}
	if err := s.Move("dev", "from", "to"); err == nil {
		t.Fatal("moving onto a space that already has the agent must fail")
	}
	if err := s.Move("ghost", "from", "to"); err == nil {
		t.Fatal("moving an agent that is not assigned to from must fail")
	}
	if err := s.Move("dev", "from", "nowhere"); err == nil {
		t.Fatal("moving to an unknown space must fail")
	}
}

func TestRenameAgentUpdatesAssignmentsRoutesAndManager(t *testing.T) {
	s, p := openTemp(t)
	for _, n := range []string{"a", "b"} {
		if err := s.Add(n, t.TempDir(), now); err != nil {
			t.Fatal(err)
		}
		if err := s.Assign(n, "dev"); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.SetRoute("a", "small", "", "dev"); err != nil {
		t.Fatal(err)
	}
	if err := s.SetDelegation("a", "auto", "dev"); err != nil {
		t.Fatal(err)
	}
	if err := s.RenameAgent("dev", "coder"); err != nil {
		t.Fatalf("RenameAgent: %v", err)
	}
	back := reopen(t, p)
	for _, n := range []string{"a", "b"} {
		if sp, _ := back.Get(n); !slices.Equal(sp.Agents, []string{"coder"}) {
			t.Fatalf("space %s agents = %v", n, sp.Agents)
		}
	}
	a, _ := back.Get("a")
	if a.Flow.Routes.Small.Agent != "coder" || a.Flow.Manager != "coder" {
		t.Fatalf("routes and manager were not renamed: %+v", a.Flow)
	}
}

func TestUnassignEverywhereClearsRoutesOnDisk(t *testing.T) {
	s, p := openTemp(t)
	for _, n := range []string{"a", "b"} {
		if err := s.Add(n, t.TempDir(), now); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.Assign("a", "dev"); err != nil {
		t.Fatal(err)
	}
	if err := s.Assign("b", "dev"); err != nil {
		t.Fatal(err)
	}
	if err := s.SetRoute("b", "large", "", "dev"); err != nil {
		t.Fatal(err)
	}
	removed, err := s.UnassignEverywhere("dev")
	if err != nil {
		t.Fatalf("UnassignEverywhere: %v", err)
	}
	if !slices.Equal(removed, []string{"a", "b"}) {
		t.Fatalf("removed from %v", removed)
	}
	back := reopen(t, p)
	b, _ := back.Get("b")
	if len(b.Agents) != 0 || b.Flow.Routes.Large.Agent != "" {
		t.Fatalf("b after unassign = %+v", b)
	}
}
