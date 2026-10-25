package mcp

import (
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/tinchoram/groot-cli/internal/tool"
)

func TestCapOutputKeepsShortTextAndCutsLongTextOnARuneBoundary(t *testing.T) {
	tests := []struct {
		name      string
		in        string
		truncated bool
	}{
		{name: "short text is kept", in: "echo: hi", truncated: false},
		{name: "exactly at the cap is kept", in: strings.Repeat("a", maxOutputBytes), truncated: false},
		{name: "one byte over is cut", in: strings.Repeat("a", maxOutputBytes+1), truncated: true},
		{name: "multibyte text is cut without splitting a rune", in: strings.Repeat("é", maxOutputBytes), truncated: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := capOutput(tt.in)
			if !utf8.ValidString(got) {
				t.Fatal("cut output is not valid UTF-8")
			}
			if tt.truncated {
				if !strings.Contains(got, "[truncated: ") {
					t.Fatalf("missing truncation note: %.60q", got[len(got)-min(len(got), 60):])
				}
				if len(got) > maxOutputBytes+64 {
					t.Fatalf("output is %d bytes, want about %d", len(got), maxOutputBytes)
				}
				return
			}
			if got != tt.in {
				t.Fatal("text under the cap must come back unchanged")
			}
		})
	}
}

func TestRemoteToolIsExternalAndUntrusted(t *testing.T) {
	var ext tool.External = (*remoteTool)(nil)
	_ = ext
	rt := &remoteTool{r: Remote{Server: "grafana", Name: "echo", ReadOnly: true}}
	if !rt.External() {
		t.Fatal("a remote tool runs outside groot and must report External")
	}
	if !rt.Untrusted() {
		t.Fatal("a remote tool's output is untrusted")
	}
}
