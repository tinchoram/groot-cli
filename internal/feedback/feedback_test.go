package feedback

import (
	"path/filepath"
	"strings"
	"testing"
	"time"
)

var at = time.Date(2026, 10, 24, 12, 0, 0, 0, time.UTC)

func openTemp(t *testing.T) *Store {
	t.Helper()
	s, err := Open(filepath.Join(t.TempDir(), "feedback"))
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func TestReportRoundTripAndIndex(t *testing.T) {
	s := openTemp(t)
	bug, err := s.Report(Item{Type: TypeBug, Title: "approval box hides the path", Body: "steps: ...", Area: "ui",
		Space: "payments", Agent: "developer", Version: "0.3.0-dev", ReportedAt: at,
		Context: []string{"❯ build it", "[tool] write_file"}})
	if err != nil {
		t.Fatal(err)
	}
	if bug.ID != "BUG-001" || bug.Status != StatusOpen {
		t.Fatalf("bug = %+v", bug)
	}
	got, err := s.Get("BUG-001")
	if err != nil {
		t.Fatal(err)
	}
	if got.Title != "approval box hides the path" || got.Area != "ui" || got.Body != "steps: ..." ||
		len(got.Context) != 2 || got.Context[1] != "[tool] write_file" || got.Space != "payments" {
		t.Fatalf("round trip = %+v", got)
	}
	index, err := readFile(filepath.Join(s.dir, "BACKLOG.md"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(index, "| BUG-001 | bug | open | ui | approval box hides the path |") {
		t.Fatalf("index row missing:\n%s", index)
	}
}

func TestIDCountersAreSeparatePerType(t *testing.T) {
	s := openTemp(t)
	for i := 0; i < 2; i++ {
		if _, err := s.Report(Item{Type: TypeBug, Title: "b", ReportedAt: at}); err != nil {
			t.Fatal(err)
		}
	}
	feat, err := s.Report(Item{Type: TypeFeat, Title: "f", ReportedAt: at})
	if err != nil {
		t.Fatal(err)
	}
	if feat.ID != "FEAT-001" {
		t.Fatalf("feature id = %s, want FEAT-001", feat.ID)
	}
	bug3, _ := s.Report(Item{Type: TypeBug, Title: "c", ReportedAt: at})
	if bug3.ID != "BUG-003" {
		t.Fatalf("bug id = %s, want BUG-003", bug3.ID)
	}
}

func TestUpdateStatusAndNoteRegeneratesIndex(t *testing.T) {
	s := openTemp(t)
	if _, err := s.Report(Item{Type: TypeFeat, Title: "per-space routes", ReportedAt: at}); err != nil {
		t.Fatal(err)
	}
	if err := s.Update("FEAT-001", StatusInProgress, "started on it"); err != nil {
		t.Fatal(err)
	}
	got, _ := s.Get("FEAT-001")
	if got.Status != StatusInProgress || !strings.Contains(got.Notes, "started on it") {
		t.Fatalf("after update = %+v", got)
	}
	index, _ := readFile(filepath.Join(s.dir, "BACKLOG.md"))
	if !strings.Contains(index, "| FEAT-001 | feat | in_progress |") {
		t.Fatalf("index not regenerated:\n%s", index)
	}
	if err := s.Update("FEAT-001", "vanished", ""); err == nil {
		t.Fatal("an unknown status is refused")
	}
	if err := s.Update("FEAT-999", StatusDone, ""); err == nil {
		t.Fatal("an unknown id is refused")
	}
}

func TestListFiltersByStatus(t *testing.T) {
	s := openTemp(t)
	_, _ = s.Report(Item{Type: TypeBug, Title: "one", ReportedAt: at})
	_, _ = s.Report(Item{Type: TypeBug, Title: "two", ReportedAt: at})
	_ = s.Update("BUG-002", StatusDone, "")
	open, err := s.List(StatusOpen)
	if err != nil || len(open) != 1 || open[0].ID != "BUG-001" {
		t.Fatalf("open = %+v, %v", open, err)
	}
	all, _ := s.List("")
	if len(all) != 2 {
		t.Fatalf("all = %d items", len(all))
	}
}
