package ui

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func pushAll(h *History, lines ...string) {
	for _, l := range lines {
		h.Push(l)
	}
}

func TestHistoryRecallOrder(t *testing.T) {
	h := NewHistory(nil, 100)
	pushAll(h, "a", "b", "c")
	steps := []string{"c", "b", "a", "a"} // the last Prev stays on the oldest entry
	for i, want := range steps {
		got, ok := h.Prev("")
		if !ok || got != want {
			t.Fatalf("Prev #%d = %q,%v want %q", i, got, ok, want)
		}
	}
}

func TestHistoryDraftRestoredPastNewest(t *testing.T) {
	h := NewHistory(nil, 100)
	pushAll(h, "a", "b")
	if got, _ := h.Prev("half typed"); got != "b" {
		t.Fatalf("Prev = %q", got)
	}
	if got, _ := h.Next(); got != "half typed" {
		t.Fatalf("Next past newest must restore the draft, got %q", got)
	}
	if h.Browsing() {
		t.Fatal("restoring the draft ends browsing")
	}
	if _, ok := h.Next(); ok {
		t.Fatal("Next outside browsing must report no change")
	}
}

func TestHistoryResetReturnsDraft(t *testing.T) {
	h := NewHistory(nil, 100)
	pushAll(h, "a")
	h.Prev("draft text")
	if got := h.Reset(); got != "draft text" {
		t.Fatalf("Reset = %q", got)
	}
	if h.Browsing() {
		t.Fatal("Reset must stop browsing")
	}
}

func TestHistoryCollapsesConsecutiveDuplicatesAndSkipsBlank(t *testing.T) {
	cases := []struct {
		name string
		in   []string
		want []string
	}{
		{"consecutive duplicate", []string{"a", "a", "b", "b", "a"}, []string{"a", "b", "a"}},
		{"blank lines", []string{"", "   ", "a"}, []string{"a"}},
		{"non-consecutive repeat kept", []string{"a", "b", "a"}, []string{"a", "b", "a"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			h := NewHistory(nil, 100)
			pushAll(h, c.in...)
			if got := h.Entries(); !reflect.DeepEqual(got, c.want) {
				t.Fatalf("entries = %q want %q", got, c.want)
			}
		})
	}
}

func TestHistoryCapDropsOldest(t *testing.T) {
	h := NewHistory(nil, 3)
	pushAll(h, "1", "2", "3", "4", "5")
	if got := h.Entries(); !reflect.DeepEqual(got, []string{"3", "4", "5"}) {
		t.Fatalf("entries = %q", got)
	}
}

func TestHistoryEditAfterRecallDoesNotMutateEntries(t *testing.T) {
	h := NewHistory(nil, 100)
	pushAll(h, "kubectl get pods")
	recalled, _ := h.Prev("")
	edited := recalled + " -n prod" // the UI edits its own copy
	if edited == recalled {
		t.Fatal("test setup: edit must change the copy")
	}
	h.Prev(edited)
	h.Reset()
	if got := h.Entries(); !reflect.DeepEqual(got, []string{"kubectl get pods"}) {
		t.Fatalf("stored entry changed: %q", got)
	}
}

func TestEscapeRoundTrip(t *testing.T) {
	cases := []string{
		"plain",
		"two\nlines",
		`back\slash`,
		`literal \n stays`,
		"trailing backslash \\",
	}
	for _, in := range cases {
		if got := unescapeEntry(escapeEntry(in)); got != in {
			t.Errorf("round trip of %q gave %q", in, got)
		}
		if strings.Contains(escapeEntry(in), "\n") {
			t.Errorf("escaped form of %q must be one line", in)
		}
	}
}

func TestHistoryFileRoundTripAndPermissions(t *testing.T) {
	path := filepath.Join(t.TempDir(), "history")
	entries := []string{"list pods", "multi\nline prompt", `path C:\tmp\n`}
	if err := SaveHistory(path, entries); err != nil {
		t.Fatalf("SaveHistory: %v", err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Fatalf("permissions = %o, want 600", perm)
	}
	got, err := LoadHistory(path)
	if err != nil {
		t.Fatalf("LoadHistory: %v", err)
	}
	if !reflect.DeepEqual(got, entries) {
		t.Fatalf("round trip = %q want %q", got, entries)
	}
}

func TestLoadHistoryMissingFileAndCap(t *testing.T) {
	dir := t.TempDir()
	got, err := LoadHistory(filepath.Join(dir, "absent"))
	if err != nil || got != nil {
		t.Fatalf("missing file should be empty, got %q (%v)", got, err)
	}

	path := filepath.Join(dir, "history")
	var all []string
	for i := 0; i < HistoryCap+5; i++ {
		all = append(all, string(rune('a'+i%26))+strings.Repeat("x", i%3))
	}
	if err := os.WriteFile(path, []byte(strings.Join(all, "\n")+"\n\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	got, err = LoadHistory(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != HistoryCap || got[len(got)-1] != all[len(all)-1] {
		t.Fatalf("load should keep the newest %d entries, got %d", HistoryCap, len(got))
	}
}

func TestSaveHistoryTrimsToCap(t *testing.T) {
	path := filepath.Join(t.TempDir(), "history")
	var entries []string
	for i := 0; i < HistoryCap+2; i++ {
		entries = append(entries, "entry")
	}
	entries[len(entries)-1] = "newest"
	if err := SaveHistory(path, entries); err != nil {
		t.Fatal(err)
	}
	got, _ := LoadHistory(path)
	if len(got) != HistoryCap || got[len(got)-1] != "newest" {
		t.Fatalf("saved %d entries, newest %q", len(got), got[len(got)-1])
	}
}
