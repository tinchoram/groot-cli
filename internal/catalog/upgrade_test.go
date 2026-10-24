package catalog

import (
	"slices"
	"testing"
)

func TestUpgradeAddsMissingManagedToolsAndKeepsTheRest(t *testing.T) {
	d := AgentDef{Name: "developer", Tools: []string{"bash", "read_file", "recall"}}
	up, added := Upgrade(d)

	if !slices.Equal(up.Tools[:3], []string{"bash", "read_file", "recall"}) {
		t.Fatalf("existing tools keep their order: %v", up.Tools)
	}
	for _, want := range ManagedTools() {
		if !slices.Contains(up.Tools, want) {
			t.Fatalf("%s missing after upgrade: %v", want, up.Tools)
		}
	}
	if slices.Contains(added, "recall") {
		t.Fatal("recall was already there, so it is not reported as added")
	}
	if len(d.Tools) != 3 {
		t.Fatal("the input definition must not change")
	}
}

func TestUpgradeIsIdempotent(t *testing.T) {
	up, _ := Upgrade(AgentDef{Name: "x", Tools: []string{"bash"}})
	again, added := Upgrade(up)
	if len(added) != 0 || len(again.Tools) != len(up.Tools) {
		t.Fatalf("second upgrade added %v", added)
	}
}

func TestUpgradeNeverRemovesTools(t *testing.T) {
	up, _ := Upgrade(AgentDef{Name: "x", Tools: []string{"write_file", "bash", "custom_tool"}})
	for _, keep := range []string{"write_file", "bash", "custom_tool"} {
		if !slices.Contains(up.Tools, keep) {
			t.Fatalf("%s was removed", keep)
		}
	}
}
