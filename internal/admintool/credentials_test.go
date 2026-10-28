package admintool

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/tinchoram/groot-cli/internal/tool"
)

// The base fake accepts the credential and network changes without recording them.
func (f *fakeManager) EditAgentCredentials(string, []string, []string) error { return nil }

func (f *fakeManager) SetSpaceNetwork(string, bool) error { return nil }

// credentialManager records the credential edits made through the admin tools.
type credentialManager struct {
	fakeManager
	edits []string
	err   error
}

func (c *credentialManager) EditAgentCredentials(name string, add, remove []string) error {
	for _, s := range add {
		c.edits = append(c.edits, "+"+s+"@"+name)
	}
	for _, s := range remove {
		c.edits = append(c.edits, "-"+s+"@"+name)
	}
	return c.err
}

func TestGrantCredentialsAddsASet(t *testing.T) {
	m := &credentialManager{}
	out, isErr := NewGrantCredentials(m).Execute(context.Background(), `{"agent":"reader","set":"aws"}`)
	if isErr || out != "granted aws to reader" {
		t.Fatalf("grant_credentials = %q (error %v)", out, isErr)
	}
	if !slices.Equal(m.edits, []string{"+aws@reader"}) {
		t.Fatalf("edits = %q", m.edits)
	}
}

func TestRevokeCredentialsRemovesASet(t *testing.T) {
	m := &credentialManager{}
	out, isErr := NewRevokeCredentials(m).Execute(context.Background(), `{"agent":"reader","set":"kube"}`)
	if isErr || out != "revoked kube from reader" {
		t.Fatalf("revoke_credentials = %q (error %v)", out, isErr)
	}
	if !slices.Equal(m.edits, []string{"-kube@reader"}) {
		t.Fatalf("edits = %q", m.edits)
	}
}

func TestCredentialToolsNeedAnAgentAndASet(t *testing.T) {
	for _, tl := range []tool.Tool{NewGrantCredentials(&credentialManager{}), NewRevokeCredentials(&credentialManager{})} {
		for _, in := range []string{`{}`, `{"agent":"reader"}`, `{"set":"aws"}`, `{"agent":"reader","set":"aws","extra":1}`} {
			if out, isErr := tl.Execute(context.Background(), in); !isErr {
				t.Errorf("%s(%s) must be refused, got %q", tl.Definition().Name, in, out)
			}
		}
	}
}

func TestCredentialToolsReportTheManagerError(t *testing.T) {
	m := &credentialManager{err: fmt.Errorf(`unknown credential set "vault"`)}
	out, isErr := NewGrantCredentials(m).Execute(context.Background(), `{"agent":"reader","set":"vault"}`)
	if !isErr || !strings.Contains(out, "vault") {
		t.Fatalf("out = %q (error %v), want the manager error", out, isErr)
	}
}

func TestCredentialToolsAreAdminWrites(t *testing.T) {
	m := &credentialManager{}
	grant, revoke := NewGrantCredentials(m), NewRevokeCredentials(m)
	if grant.Risk() != tool.RiskWrite || revoke.Risk() != tool.RiskWrite {
		t.Fatal("credential changes are writes")
	}
	if got := grant.Preview(`{"agent":"reader","set":"aws"}`); got != "grant aws credentials to reader" {
		t.Fatalf("grant preview = %q", got)
	}
	if got := revoke.Preview(`{"agent":"reader","set":"aws"}`); got != "revoke aws credentials from reader" {
		t.Fatalf("revoke preview = %q", got)
	}
	names := map[string]bool{}
	for _, tl := range All(m, "/home/sre") {
		names[tl.Definition().Name] = true
	}
	if !names["grant_credentials"] || !names["revoke_credentials"] {
		t.Fatal("the credential tools must be registered with the admin tools")
	}
}

// networkManager records the network changes made through edit_space.
type networkManager struct {
	fakeManager
	networks []string
}

func (n *networkManager) SetSpaceNetwork(space string, on bool) error {
	n.networks = append(n.networks, fmt.Sprintf("%s:%t", space, on))
	return nil
}

func TestEditSpaceSetsTheNetworkOfASpace(t *testing.T) {
	m := &networkManager{fakeManager: *newEditFake()}
	tl := NewEditSpace(m, "/home/sre")
	out, isErr := tl.Execute(context.Background(), `{"name":"payments","network":false}`)
	if isErr || !strings.Contains(out, "network") {
		t.Fatalf("edit_space network = %q (error %v)", out, isErr)
	}
	if !slices.Equal(m.networks, []string{"payments:false"}) {
		t.Fatalf("networks = %q", m.networks)
	}
	for _, e := range m.edits {
		if strings.HasPrefix(e, "edit_space:") {
			t.Fatalf("a network-only edit must not rename or move the space: %q", m.edits)
		}
	}
}

func TestEditSpaceNetworkFollowsARename(t *testing.T) {
	m := &networkManager{fakeManager: *newEditFake()}
	if _, isErr := NewEditSpace(m, "/home/sre").Execute(context.Background(), `{"name":"payments","new_name":"billing","network":true}`); isErr {
		t.Fatal("valid edit refused")
	}
	if !slices.Equal(m.networks, []string{"billing:true"}) {
		t.Fatalf("the network must apply to the renamed space: %q", m.networks)
	}
}

func TestEditSpaceWithoutNetworkKeepsTheOldRules(t *testing.T) {
	m := &networkManager{fakeManager: *newEditFake()}
	if out, isErr := NewEditSpace(m, "/home/sre").Execute(context.Background(), `{"name":"payments"}`); !isErr || !strings.Contains(out, "new_name or path") {
		t.Fatalf("no change = %q (%v)", out, isErr)
	}
	if len(m.networks) != 0 {
		t.Fatalf("no network was given, but %q was set", m.networks)
	}
}
