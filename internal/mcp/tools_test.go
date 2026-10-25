package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"regexp"
	"strings"
	"testing"
	"time"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/tinchoram/groot-cli/internal/config"
	"github.com/tinchoram/groot-cli/internal/tool"
)

var toolNamePattern = regexp.MustCompile(`^[a-zA-Z0-9_-]{1,64}$`)

func TestToolNameIsSanitisedAndRoundTrips(t *testing.T) {
	tests := []struct {
		name, server, tool, want string
	}{
		{name: "plain", server: "grafana", tool: "query_prometheus", want: "grafana__query_prometheus"},
		{name: "dot replaced", server: "k8s", tool: "list.pods", want: "k8s__list_pods"},
		{name: "unicode replaced", server: "k8s", tool: "lístä", want: "k8s__l_st_"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ToolName(tt.server, tt.tool)
			if got != tt.want {
				t.Fatalf("got %q, want %q", got, tt.want)
			}
			if !toolNamePattern.MatchString(got) {
				t.Fatalf("name %q breaks the model's tool-name rule", got)
			}
		})
	}

	server, tool, ok := SplitToolName(ToolName("grafana", "query_prometheus"))
	if !ok || server != "grafana" || tool != "query_prometheus" {
		t.Fatalf("round trip lost the parts: %q %q %v", server, tool, ok)
	}
	if _, _, ok := SplitToolName("builtin_tool"); ok {
		t.Fatal("a name without the server separator is not an MCP tool")
	}
}

func TestToolNameIsCutToSixtyFourCharacters(t *testing.T) {
	long := strings.Repeat("a", 200)
	got := ToolName("grafana", long)
	if len(got) != 64 || !toolNamePattern.MatchString(got) {
		t.Fatalf("got length %d: %q", len(got), got)
	}
}

func adaptedFor(t *testing.T, readOnly bool) tool.Tool {
	t.Helper()
	clock := &fakeClock{now: time.Unix(1000, 0)}
	m, _, _ := newTestManager(t, grafanaServer(), clock)
	return AdaptTool(m, Remote{Server: "grafana", Name: "echo", Description: "Echo.", ReadOnly: readOnly,
		InputSchema: json.RawMessage(`{"type":"object"}`)})
}

func TestAdaptedToolRiskFollowsTheReadOnlyList(t *testing.T) {
	ro := adaptedFor(t, true)
	rw := adaptedFor(t, false)

	if ro.Risk() != tool.RiskRead || ro.RiskFor(`{}`) != tool.RiskRead {
		t.Fatalf("a read-only remote is RiskRead, got %v", ro.Risk())
	}
	if rw.Risk() != tool.RiskWrite || rw.RiskFor(`{}`) != tool.RiskWrite {
		t.Fatalf("a remote not in read_only is RiskWrite, got %v", rw.Risk())
	}
}

func TestAdaptedToolIsUntrusted(t *testing.T) {
	u, ok := adaptedFor(t, true).(tool.Untrusted)
	if !ok || !u.Untrusted() {
		t.Fatal("remote output is external text and must be marked untrusted")
	}
}

func TestAdaptedToolDefinitionNamesTheServerAndCapsTheDescription(t *testing.T) {
	clock := &fakeClock{now: time.Unix(1000, 0)}
	m, _, _ := newTestManager(t, grafanaServer(), clock)
	long := strings.Repeat("é", 500)

	def := AdaptTool(m, Remote{Server: "grafana", Name: "echo", Description: long}).Definition()
	if def.Name != "grafana__echo" {
		t.Fatalf("name = %q", def.Name)
	}
	if !strings.HasPrefix(def.Description, "[mcp:grafana] ") {
		t.Fatalf("description must name the server, got %q", def.Description)
	}
	if n := len([]rune(def.Description)); n != len([]rune("[mcp:grafana] "))+400 {
		t.Fatalf("remote description must be cut to 400 runes, total runes %d", n)
	}
	if string(def.InputSchema) != `{"type":"object"}` {
		t.Fatalf("an empty remote schema becomes an object schema, got %s", def.InputSchema)
	}
}

func TestAdaptedToolPreviewIsCompactAndCut(t *testing.T) {
	clock := &fakeClock{now: time.Unix(1000, 0)}
	m, _, _ := newTestManager(t, grafanaServer(), clock)
	a := AdaptTool(m, Remote{Server: "grafana", Name: "echo"})

	if got := a.Preview(`{ "text" : "hi" }`); got != `grafana echo {"text":"hi"}` {
		t.Fatalf("preview = %q", got)
	}
	big := `{"q":"` + strings.Repeat("x", 300) + `"}`
	got := a.Preview(big)
	if !strings.HasPrefix(got, "grafana echo {") || len([]rune(got)) > len([]rune("grafana echo "))+120 {
		t.Fatalf("arguments must be cut to 120 runes, got %d runes", len([]rune(got)))
	}
}

func TestAdaptedToolExecuteReturnsTextAndErrors(t *testing.T) {
	clock := &fakeClock{now: time.Unix(1000, 0)}
	m, _, _ := newTestManager(t, grafanaServer(), clock)
	echo := AdaptTool(m, Remote{Server: "grafana", Name: "echo"})
	fail := AdaptTool(m, Remote{Server: "grafana", Name: "fail"})

	out, isErr := echo.Execute(t.Context(), `{"text":"hi"}`)
	if out != "echo: hi" || isErr {
		t.Fatalf("success: got %q, %v", out, isErr)
	}
	out, isErr = fail.Execute(t.Context(), `{}`)
	if out != "query failed: bad metric" || !isErr {
		t.Fatalf("tool error must carry the flag: got %q, %v", out, isErr)
	}
	out, isErr = echo.Execute(t.Context(), `not json`)
	if !isErr || !strings.HasPrefix(out, "mcp grafana/echo: ") {
		t.Fatalf("a call error must name the server and tool: got %q, %v", out, isErr)
	}
}

func TestAdaptAllSkipsAFailedServerAndReportsItOnce(t *testing.T) {
	f := newFixture(t)
	good := f.dialer(t)
	opts := Options{
		dial: func(ctx context.Context, name string, r resolved) (sdk.Transport, error) {
			if name == "bad" {
				return nil, errors.New("connection refused")
			}
			return good(ctx, name, r)
		},
	}
	m := New(map[string]config.MCPServer{
		"grafana": grafanaServer(),
		"bad":     {Transport: "stdio", Command: "x", Tools: []string{"a"}},
	}, opts)

	tools, errs := AdaptAll(t.Context(), m)
	if len(tools) != 4 {
		t.Fatalf("the healthy server contributes its four allowlisted tools, got %d", len(tools))
	}
	if len(errs) != 1 || !strings.Contains(errs[0].Error(), "mcp server bad") {
		t.Fatalf("want one error naming the failed server, got %v", errs)
	}
	for _, tl := range tools {
		if strings.HasPrefix(tl.Definition().Name, "bad__") {
			t.Fatal("a failed server must yield no tools")
		}
	}
}

func TestAdaptServerReturnsOnlyThatServersTools(t *testing.T) {
	clock := &fakeClock{now: time.Unix(1000, 0)}
	m, _, _ := newTestManager(t, grafanaServer(), clock)

	tools, err := AdaptServer(t.Context(), m, "grafana")
	if err != nil {
		t.Fatal(err)
	}
	if len(tools) != 4 {
		t.Fatalf("got %d tools", len(tools))
	}
	if _, err := AdaptServer(t.Context(), m, "nope"); err == nil {
		t.Fatal("an unknown server must fail")
	}
}
