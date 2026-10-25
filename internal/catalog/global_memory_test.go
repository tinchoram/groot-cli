package catalog

import (
	"slices"
	"strings"
	"testing"
)

func TestGlobalMemoryFlagParsesFromTheFrontmatter(t *testing.T) {
	tests := []struct {
		name string
		body string
		want bool
	}{
		{name: "set", body: "---\nname: payments\ndescription: d\ntools: [recall, remember]\nglobal_memory: true\n---\nprompt\n", want: true},
		{name: "missing means off", body: "---\nname: payments\ndescription: d\ntools: [recall, remember]\n---\nprompt\n", want: false},
		{name: "explicit false", body: "---\nname: payments\ndescription: d\ntools: [recall]\nglobal_memory: false\n---\nprompt\n", want: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			d, err := ParseAgentFile([]byte(tt.body))
			if err != nil {
				t.Fatal(err)
			}
			if d.GlobalMemory != tt.want {
				t.Fatalf("GlobalMemory = %v, want %v", d.GlobalMemory, tt.want)
			}
		})
	}
}

func TestGlobalMemoryFlagIsWrittenOnlyWhenSetAndRoundTrips(t *testing.T) {
	off, err := RenderAgentFile(AgentDef{Name: "payments", Description: "d", Tools: []string{"recall"}, Prompt: "p"})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(off), "global_memory") {
		t.Fatalf("an agent without global memory must not carry the key:\n%s", off)
	}

	on, err := RenderAgentFile(AgentDef{Name: "payments", Description: "d", Tools: []string{"recall"}, Prompt: "p", GlobalMemory: true})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(on), "global_memory: true") {
		t.Fatalf("the flag must be written:\n%s", on)
	}
	back, err := ParseAgentFile(on)
	if err != nil {
		t.Fatal(err)
	}
	if !back.GlobalMemory {
		t.Fatal("the flag must survive a render and parse")
	}
}

func TestMemoryToolsAreKnownAndInTheDefaults(t *testing.T) {
	for _, name := range []string{"memory_get", "forget", "update_memory", "grant_global_memory", "revoke_global_memory"} {
		if !slices.Contains(KnownTools, name) {
			t.Fatalf("%s must be a known tool", name)
		}
	}
	for _, name := range []string{"memory_get", "forget", "update_memory"} {
		if !slices.Contains(DefaultTools(), name) {
			t.Fatalf("%s must be in the default tool set", name)
		}
	}
}
