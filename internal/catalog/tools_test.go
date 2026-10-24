package catalog

import (
	"reflect"
	"slices"
	"strings"
	"testing"
)

func TestEditToolsAddsKnownToolsOnce(t *testing.T) {
	d := AgentDef{Name: "dev", Tools: []string{"read_file"}}
	got, err := EditTools(d, []string{"bash", "read_file", "kubectl"}, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"read_file", "bash", "kubectl"}; !reflect.DeepEqual(got.Tools, want) {
		t.Fatalf("tools = %v, want %v", got.Tools, want)
	}
	if !reflect.DeepEqual(d.Tools, []string{"read_file"}) {
		t.Fatal("the input definition must not change")
	}
}

func TestEditToolsRefusesUnknownTools(t *testing.T) {
	d := AgentDef{Name: "dev", Tools: []string{"read_file"}}
	if _, err := EditTools(d, []string{"read_file", "teleport"}, nil, nil); err == nil || !strings.Contains(err.Error(), "teleport") {
		t.Fatalf("unknown tool must be refused: %v", err)
	}
}

func TestEditToolsRemovesTools(t *testing.T) {
	d := AgentDef{Name: "dev", Tools: []string{"read_file", "bash", "kubectl"}}
	got, err := EditTools(d, nil, []string{"bash", "not-there"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"read_file", "kubectl"}; !reflect.DeepEqual(got.Tools, want) {
		t.Fatalf("tools = %v, want %v", got.Tools, want)
	}
}

func TestEditToolsKeepsRequiredTools(t *testing.T) {
	d := AgentDef{Name: "groot", Tools: []string{"create_space", "recall"}}
	_, err := EditTools(d, nil, []string{"create_space"}, []string{"create_space"})
	if err == nil || !strings.Contains(err.Error(), "required") {
		t.Fatalf("a required tool must not be removed: %v", err)
	}
}

func TestSplitToolsAcceptsCommasAndSpaces(t *testing.T) {
	got := SplitTools([]string{"bash,kubectl", "  recall ", ""})
	if want := []string{"bash", "kubectl", "recall"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("split = %v, want %v", got, want)
	}
}

func TestBaseSetIsTheManagedToolsWithoutFetch(t *testing.T) {
	want := []string{"send_message", "read_inbox", "task_classify", "task_open", "task_advance", "task_status", "recall", "remember", "list_dir", "glob"}
	if got := ManagedTools(); !reflect.DeepEqual(got, want) {
		t.Fatalf("base set = %v", got)
	}
	for _, name := range []string{"list_dir", "glob"} {
		if !slices.Contains(KnownTools, name) {
			t.Fatalf("%s must be a known tool", name)
		}
	}
	for _, name := range DefaultTools() {
		if name == "fetch_url" {
			t.Fatal("fetch_url is granted explicitly, not by default")
		}
	}
}
