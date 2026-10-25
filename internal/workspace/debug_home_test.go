package workspace

import (
	"context"
	"path/filepath"
	"slices"
	"testing"

	"github.com/tinchoram/groot-cli/internal/space"
)

// The user may move debug to another space. Bootstrap must not put it back
// in root on the next start; it only places debug when it is nowhere.
func TestBootstrapKeepsDebugWhereTheUserMovedIt(t *testing.T) {
	home, userHome := t.TempDir(), t.TempDir()
	st, err := space.Open(filepath.Join(home, "spaces.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if err := st.Add("root", userHome, t0); err != nil {
		t.Fatal(err)
	}
	if err := st.Assign("root", "groot"); err != nil {
		t.Fatal(err)
	}
	if err := st.Add("tools", t.TempDir(), t0); err != nil {
		t.Fatal(err)
	}
	if err := st.Assign("tools", "debug"); err != nil {
		t.Fatal(err)
	}

	if _, err := Bootstrap(home, userHome, t0); err != nil {
		t.Fatal(err)
	}
	after, _ := space.Open(filepath.Join(home, "spaces.yaml"))
	root, _ := after.Get("root")
	if slices.Contains(root.Agents, "debug") {
		t.Fatalf("bootstrap put debug back in root: %v", root.Agents)
	}
	tools, _ := after.Get("tools")
	if !slices.Contains(tools.Agents, "debug") {
		t.Fatalf("debug must stay in tools: %v", tools.Agents)
	}
}

func TestBootstrapPlacesDebugInRootOnlyWhenItIsNowhere(t *testing.T) {
	home, userHome := t.TempDir(), t.TempDir()
	st, _ := space.Open(filepath.Join(home, "spaces.yaml"))
	_ = st.Add("root", userHome, t0)
	_ = st.Assign("root", "groot")
	_ = st.Add("tools", t.TempDir(), t0)

	if _, err := Bootstrap(home, userHome, t0); err != nil {
		t.Fatal(err)
	}
	after, _ := space.Open(filepath.Join(home, "spaces.yaml"))
	root, _ := after.Get("root")
	if !slices.Contains(root.Agents, "debug") {
		t.Fatalf("debug assigned nowhere must land in root: %v", root.Agents)
	}
}

// Reports follow debug: they go to the space it is assigned to.
func TestFeedbackFollowsDebugToItsSpace(t *testing.T) {
	f := newFixture(t, text("ok"))
	if err := f.spaces.Add("root", t.TempDir(), t0); err != nil {
		t.Fatal(err)
	}
	if err := f.spaces.Add("tools", t.TempDir(), t0); err != nil {
		t.Fatal(err)
	}
	if err := f.spaces.Assign("tools", "debug"); err != nil {
		t.Fatal(err)
	}
	_, recipient, err := f.ws.SendFeedback("default", "reader", "bug", "something", nil)
	if err != nil {
		t.Fatal(err)
	}
	if recipient != "tools/debug" {
		t.Fatalf("recipient = %q, want tools/debug", recipient)
	}
}

// A bare "debug" address reaches debug wherever it is, like "groot" does.
func TestBareDebugAddressResolvesToItsSpace(t *testing.T) {
	f := newFixture(t, text("ok"))
	if err := f.spaces.Add("tools", t.TempDir(), t0); err != nil {
		t.Fatal(err)
	}
	if err := f.spaces.Assign("tools", "debug"); err != nil {
		t.Fatal(err)
	}
	recipient, _, err := f.ws.messengerFor("default", "reader").Send(context.Background(), "debug", "bug: x", "body")
	if err != nil {
		t.Fatal(err)
	}
	if recipient != "tools/debug" {
		t.Fatalf("recipient = %q, want tools/debug", recipient)
	}
}
