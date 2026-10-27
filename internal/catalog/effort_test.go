package catalog

import (
	"strings"
	"testing"
)

const effortAgent = "---\nname: reviewer\ndescription: reviews\ntools: [read_file]\n"

func TestAgentFrontmatterParsesEffort(t *testing.T) {
	d, err := ParseAgentFile([]byte(effortAgent + "effort: high\n---\nYou review.\n"))
	if err != nil {
		t.Fatal(err)
	}
	if d.Effort != "high" {
		t.Fatalf("effort = %q, want high", d.Effort)
	}
}

func TestAgentWithoutEffortLeavesItUnset(t *testing.T) {
	d, err := ParseAgentFile([]byte(effortAgent + "---\nYou review.\n"))
	if err != nil {
		t.Fatal(err)
	}
	if d.Effort != "" {
		t.Fatalf("effort = %q, want unset so the provider value applies", d.Effort)
	}
}

func TestAgentFrontmatterRejectsUnknownEffort(t *testing.T) {
	_, err := ParseAgentFile([]byte(effortAgent + "effort: turbo\n---\nYou review.\n"))
	if err == nil || !strings.Contains(err.Error(), "effort") {
		t.Fatalf("err = %v, want an effort error", err)
	}
}

func TestValidateRejectsUnknownEffort(t *testing.T) {
	err := Validate(AgentDef{Name: "reviewer", Tools: []string{"read_file"}, Effort: "turbo"})
	if err == nil || !strings.Contains(err.Error(), "effort") {
		t.Fatalf("err = %v, want an effort error", err)
	}
}

func TestEffortRoundTripsThroughTheAgentFile(t *testing.T) {
	raw, err := RenderAgentFile(AgentDef{Name: "reviewer", Tools: []string{"read_file"}, Effort: "xhigh", Prompt: "You review."})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), "effort: xhigh") {
		t.Fatalf("rendered file lacks effort:\n%s", raw)
	}
	back, err := ParseAgentFile(raw)
	if err != nil {
		t.Fatal(err)
	}
	if back.Effort != "xhigh" {
		t.Fatalf("effort after round trip = %q", back.Effort)
	}
}
