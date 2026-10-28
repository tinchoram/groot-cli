package catalog

import (
	"slices"
	"strings"
	"testing"
)

const credentialsAgent = "---\nname: ops\ndescription: runs the cluster\ntools: [bash, read_file]\ncredentials: [aws, kube]\n---\n\nYou operate the cluster.\n"

func TestParseAgentFileReadsCredentialSets(t *testing.T) {
	d, err := ParseAgentFile([]byte(credentialsAgent))
	if err != nil {
		t.Fatalf("ParseAgentFile: %v", err)
	}
	if !slices.Equal(d.Credentials, []string{"aws", "kube"}) {
		t.Fatalf("Credentials = %q", d.Credentials)
	}
	if err := Validate(d); err != nil {
		t.Fatalf("Validate: %v", err)
	}
}

func TestParseAgentFileWithoutCredentialsGrantsNone(t *testing.T) {
	d, err := ParseAgentFile([]byte("---\nname: plain\ndescription: d\ntools: [read_file]\n---\nbody\n"))
	if err != nil {
		t.Fatalf("ParseAgentFile: %v", err)
	}
	if len(d.Credentials) != 0 {
		t.Fatalf("Credentials = %q, want none", d.Credentials)
	}
}

func TestParseAgentFileRejectsAnUnknownCredentialSet(t *testing.T) {
	for _, set := range []string{"vault", ".groot", "groot"} {
		body := "---\nname: ops\ndescription: d\ntools: [bash]\ncredentials: [" + set + "]\n---\nbody\n"
		_, err := ParseAgentFile([]byte(body))
		if err == nil || !strings.Contains(err.Error(), "credentials") || !strings.Contains(err.Error(), set) {
			t.Fatalf("set %q: error = %v, want it to name credentials and %q", set, err, set)
		}
	}
}

func TestValidateRejectsAnUnknownCredentialSet(t *testing.T) {
	err := Validate(AgentDef{Name: "ops", Tools: []string{"bash"}, Credentials: []string{"vault"}})
	if err == nil || !strings.Contains(err.Error(), "vault") {
		t.Fatalf("Validate error = %v, want it to name vault", err)
	}
}

func TestCredentialsRoundTripThroughTheFile(t *testing.T) {
	d, err := ParseAgentFile([]byte(credentialsAgent))
	if err != nil {
		t.Fatalf("ParseAgentFile: %v", err)
	}
	raw, err := RenderAgentFile(d)
	if err != nil {
		t.Fatalf("RenderAgentFile: %v", err)
	}
	back, err := ParseAgentFile(raw)
	if err != nil {
		t.Fatalf("parse rendered file: %v\n%s", err, raw)
	}
	if !slices.Equal(back.Credentials, []string{"aws", "kube"}) {
		t.Fatalf("round trip Credentials = %q\n%s", back.Credentials, raw)
	}
}

func TestRenderOmitsCredentialsWhenThereAreNone(t *testing.T) {
	raw, err := RenderAgentFile(AgentDef{Name: "plain", Description: "d", Tools: []string{"read_file"}, Prompt: "body"})
	if err != nil {
		t.Fatalf("RenderAgentFile: %v", err)
	}
	if strings.Contains(string(raw), "credentials") {
		t.Fatalf("an agent without sets must not write the key:\n%s", raw)
	}
}
