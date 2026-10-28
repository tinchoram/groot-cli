package gate

import "testing"

// Granting or revoking credentials changes an agent file, so it is an admin
// change: work mode never approves it and no session grant can cover it.
func TestCredentialToolsAreAdminChanges(t *testing.T) {
	for _, name := range []string{"grant_credentials", "revoke_credentials"} {
		if Grantable(name) {
			t.Errorf("%s must not be grantable for the session", name)
		}
		s := &WorkState{}
		if s.approves(Request{Tool: name, Input: `{"agent":"sre","set":"aws"}`}) {
			t.Errorf("work mode must not approve %s", name)
		}
	}
}

// A call that carries credential sets is still approved by work mode on the
// same terms as before: the sets are not a reason to ask by themselves.
func TestCredentialSetsDoNotChangeWorkApproval(t *testing.T) {
	s := &WorkState{}
	req := Request{Tool: "bash", Input: `{"command":"aws s3 ls"}`, Credentials: []string{"aws"}}
	if !s.approves(req) {
		t.Fatal("work mode approves a plain bash call; a credential set must not stop it")
	}
}
