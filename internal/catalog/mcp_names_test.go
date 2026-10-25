package catalog

import "testing"

func TestIsKnownToolAcceptsBuiltinsAndSyntacticMCPNames(t *testing.T) {
	tests := []struct {
		name string
		want bool
	}{
		{"read_file", true},
		{"grafana__query_prometheus", true},
		{"grafana__*", true},
		{"k8s-prod__list.pods", true},
		{"Grafana__echo", false},
		{"grafana__", false},
		{"__echo", false},
		{"grafana__ echo", false},
		{"no_such_tool", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := IsKnownTool(tt.name); got != tt.want {
				t.Fatalf("IsKnownTool(%q) = %v, want %v", tt.name, got, tt.want)
			}
		})
	}
}

func TestMCPNamesAreEditableAndValidated(t *testing.T) {
	d := AgentDef{Name: "reader", Tools: []string{"read_file"}}
	up, err := EditTools(d, []string{"grafana__*", "grafana__echo"}, nil, nil)
	if err != nil {
		t.Fatalf("EditTools: %v", err)
	}
	if err := Validate(up); err != nil {
		t.Fatalf("Validate rejected MCP names: %v", err)
	}
	if _, err := EditTools(d, []string{"grafana__"}, nil, nil); err == nil {
		t.Fatal("a malformed MCP name must be refused")
	}
}
