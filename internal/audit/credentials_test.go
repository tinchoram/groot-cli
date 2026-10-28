package audit

import (
	"encoding/json"
	"strings"
	"testing"
)

// The record names the credential sets of a call, and leaves the key out when
// the call had none, so older readers and plain calls see no change.
func TestRecordCarriesCredentialSetsOnlyWhenPresent(t *testing.T) {
	with, err := json.Marshal(Record{Tool: "bash", Credentials: []string{"aws", "kube"}})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(with), `"credentials":["aws","kube"]`) {
		t.Fatalf("record = %s, want the credential sets", with)
	}
	plain, err := json.Marshal(Record{Tool: "bash"})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(plain), "credentials") {
		t.Fatalf("record = %s, want no credentials key", plain)
	}
}
