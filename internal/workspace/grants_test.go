package workspace

import (
	"slices"
	"testing"
)

func TestSessionGrantsArePerConversation(t *testing.T) {
	f := newFixture(t, text("ok"))
	// Turning work mode on opens the conversation, so its grants exist.
	if err := f.ws.SetWorkMode("default", "reader", true); err != nil {
		t.Fatal(err)
	}
	key := "default/reader"

	if err := f.ws.GrantSession(key, "write_file"); err != nil {
		t.Fatal(err)
	}
	if err := f.ws.GrantSession(key, "bash"); err != nil {
		t.Fatal(err)
	}
	if got := f.ws.SessionGrants(key); !slices.Equal(got, []string{"bash", "write_file"}) {
		t.Fatalf("grants = %v", got)
	}
	if got := f.ws.SessionGrants("default/other"); len(got) != 0 {
		t.Fatalf("another conversation must not see the grants: %v", got)
	}
	if err := f.ws.GrantSession(key, "create_space"); err == nil {
		t.Fatal("admin tools must not be granted for the session")
	}
	if err := f.ws.GrantSession("default/nobody", "bash"); err == nil {
		t.Fatal("a conversation that is not open has no grants")
	}

	f.ws.ClearSessionGrants(key)
	if got := f.ws.SessionGrants(key); len(got) != 0 {
		t.Fatalf("after clear: %v", got)
	}
}
