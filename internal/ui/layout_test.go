package ui

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLayoutRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "home", "layout.yaml")
	want := Layout{SidebarWidth: 34, TerminalWidth: 50, SidebarVisible: true, TerminalVisible: false}
	if err := SaveLayout(path, want); err != nil {
		t.Fatalf("SaveLayout: %v", err)
	}
	got, err := LoadLayout(path)
	if err != nil {
		t.Fatalf("LoadLayout: %v", err)
	}
	if got != want {
		t.Fatalf("round trip = %+v, want %+v", got, want)
	}
}

func TestLayoutMissingFileGivesDefaults(t *testing.T) {
	got, err := LoadLayout(filepath.Join(t.TempDir(), "absent.yaml"))
	if err != nil {
		t.Fatalf("a missing layout is not an error: %v", err)
	}
	if got != DefaultLayout() {
		t.Fatalf("got %+v, want defaults %+v", got, DefaultLayout())
	}
}

func TestLayoutCorruptFileIsReported(t *testing.T) {
	path := filepath.Join(t.TempDir(), "layout.yaml")
	if err := os.WriteFile(path, []byte("sidebar_width: [unclosed"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadLayout(path); err == nil {
		t.Fatal("corrupt layout must be an error")
	}
}
