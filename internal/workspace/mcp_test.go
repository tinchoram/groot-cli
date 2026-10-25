package workspace

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/tinchoram/groot-cli/internal/admintool"
	"github.com/tinchoram/groot-cli/internal/api"
	"github.com/tinchoram/groot-cli/internal/catalog"
	"github.com/tinchoram/groot-cli/internal/config"
	"github.com/tinchoram/groot-cli/internal/gate"
	"github.com/tinchoram/groot-cli/internal/mcp"
	"github.com/tinchoram/groot-cli/internal/mcp/mcptest"
	"github.com/tinchoram/groot-cli/internal/provider"
	"github.com/tinchoram/groot-cli/internal/space"
	"github.com/tinchoram/groot-cli/internal/tool"
)

// grafanaServers is the global config of the fake server: echo and chart are
// allowlisted, echo is read-only, and drop_table is offered but not allowlisted.
func grafanaServers() map[string]config.MCPServer {
	return map[string]config.MCPServer{"grafana": {
		Transport: "http",
		URL:       "https://grafana.internal/mcp",
		Tools:     []string{"echo", "chart"},
		ReadOnly:  []string{"echo"},
		Timeout:   5 * time.Second,
	}}
}

// fakeGrafana is the server the workspace reaches through its MCP transport.
func fakeGrafana() *mcptest.Server {
	return mcptest.New(
		mcptest.Tool{Name: "echo", Description: "Echo the text back.", Handler: func(args json.RawMessage) string {
			return "echo: " + string(args)
		}},
		mcptest.Tool{Name: "chart", Description: "Render a chart.", Handler: func(json.RawMessage) string { return "rendered" }},
		mcptest.Tool{Name: "drop_table", Description: "Destructive.", Handler: func(json.RawMessage) string { return "dropped" }},
	)
}

type mcpFixture struct {
	*fixture
	srv      *mcptest.Server
	mu       sync.Mutex
	warnings []string
	notes    []string
	asked    int
}

type mcpOpts struct {
	servers map[string]config.MCPServer
	tools   []string
	gate    gate.Gate
	srv     *mcptest.Server
	steps   []provider.Step
	// transport replaces the server's transport, to make it slow. Nil uses srv.
	transport mcp.TransportFunc
}

// newMCPFixture builds a workspace whose agent "reader" has the given tools and
// whose MCP servers are served by an in-memory fake.
func newMCPFixture(t *testing.T, o mcpOpts) *mcpFixture {
	t.Helper()
	home := t.TempDir()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "notes.txt"), []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}
	tools := o.tools
	if tools == nil {
		tools = []string{"read_file"}
	}
	if err := catalog.Save(home, catalog.AgentDef{Name: "reader", Description: "reads", Tools: tools}, "You read."); err != nil {
		t.Fatal(err)
	}
	sp, err := space.Open(filepath.Join(home, "spaces.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if err := sp.Add("default", dir, t0); err != nil {
		t.Fatal(err)
	}
	if err := sp.Assign("default", "reader"); err != nil {
		t.Fatal(err)
	}
	if err := sp.Use("default"); err != nil {
		t.Fatal(err)
	}
	cfg := config.Default()
	cfg.MCP.Servers = o.servers
	mock := provider.NewMock(o.steps...)
	f := &mcpFixture{srv: o.srv}
	fx := &fixture{home: home, dir: dir, mock: mock, spaces: sp}
	g := o.gate
	if g == nil {
		g = gate.AutoApprove{}
	}
	transport := o.transport
	if transport == nil {
		transport = o.srv.Transport
	}
	ws, err := New(Options{
		Home:          home,
		Config:        cfg,
		Spaces:        sp,
		Gate:          g,
		NewProvider:   func(string) provider.Provider { return mock },
		MCPTransport:  transport,
		DisablePublic: true,
		Hooks: Hooks{
			OnWarning: func(_, msg string) {
				f.mu.Lock()
				defer f.mu.Unlock()
				f.warnings = append(f.warnings, msg)
			},
			OnNotice: func(_, text string) {
				f.mu.Lock()
				defer f.mu.Unlock()
				f.notes = append(f.notes, text)
			},
		},
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(func() { _ = ws.Close(context.Background()) })
	fx.ws = ws
	f.fixture = fx
	return f
}

func (f *mcpFixture) warned() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Clone(f.warnings)
}

// waitWarning waits for a warning containing want. Warnings are delivered
// asynchronously, so the workspace never blocks on the UI.
func waitWarning(t *testing.T, f *mcpFixture, want string) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		for _, w := range f.warned() {
			if strings.Contains(w, want) {
				return
			}
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("no warning containing %q, got %q", want, f.warned())
}

// waitNotice waits for a note containing want. MCP tools join an open
// conversation in the background, and the note says when they are in.
func waitNotice(t *testing.T, f *mcpFixture, want string) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		f.mu.Lock()
		notes := slices.Clone(f.notes)
		f.mu.Unlock()
		for _, n := range notes {
			if strings.Contains(n, want) {
				return
			}
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("no note containing %q", want)
}

// openReady opens the reader's conversation and waits until its MCP tools are in.
func openReady(t *testing.T, f *mcpFixture) Chat {
	t.Helper()
	if err := f.ws.SelectAgent("reader"); err != nil {
		t.Fatal(err)
	}
	chat := f.ws.Chat()
	waitNotice(t, f, "mcp grafana: ")
	return chat
}

func mcpCall(id, name, input string) provider.Step {
	return provider.Step{Response: api.Response{
		Content:    []api.Block{{Type: api.BlockToolUse, ID: id, Name: name, Input: json.RawMessage(input)}},
		StopReason: api.StopToolUse,
	}}
}

func toolNames(defs []api.ToolDef) []string {
	var out []string
	for _, d := range defs {
		out = append(out, d.Name)
	}
	return out
}

func TestConversationBuildsMCPToolsFromWildcardAndExplicitNames(t *testing.T) {
	t.Run("wildcard admits every allowlisted tool and no other", func(t *testing.T) {
		f := newMCPFixture(t, mcpOpts{servers: grafanaServers(), tools: []string{"read_file", "grafana__*"}, srv: fakeGrafana(), steps: []provider.Step{text("ok")}})
		if _, err := openReady(t, f).Send(context.Background(), "hi"); err != nil {
			t.Fatal(err)
		}
		got := toolNames(f.mock.Calls[0].Tools)
		if !slices.Contains(got, "grafana__echo") || !slices.Contains(got, "grafana__chart") {
			t.Fatalf("allowlisted tools missing from the model's tools: %v", got)
		}
		if slices.Contains(got, "grafana__drop_table") {
			t.Fatalf("a tool outside the allowlist reached the model: %v", got)
		}
	})

	t.Run("explicit name admits only that tool", func(t *testing.T) {
		f := newMCPFixture(t, mcpOpts{servers: grafanaServers(), tools: []string{"read_file", "grafana__echo"}, srv: fakeGrafana(), steps: []provider.Step{text("ok")}})
		if _, err := openReady(t, f).Send(context.Background(), "hi"); err != nil {
			t.Fatal(err)
		}
		got := toolNames(f.mock.Calls[0].Tools)
		if !slices.Contains(got, "grafana__echo") || slices.Contains(got, "grafana__chart") {
			t.Fatalf("explicit name must admit only echo: %v", got)
		}
	})
}

func TestConversationCallsAnMCPTool(t *testing.T) {
	f := newMCPFixture(t, mcpOpts{
		servers: grafanaServers(),
		tools:   []string{"read_file", "grafana__echo"},
		srv:     fakeGrafana(),
		steps:   []provider.Step{mcpCall("tu_1", "grafana__echo", `{"text":"hi"}`), text("done")},
	})
	if _, err := openReady(t, f).Send(context.Background(), "echo hi"); err != nil {
		t.Fatal(err)
	}
	last := f.mock.Calls[1].Messages[len(f.mock.Calls[1].Messages)-1].Content[0]
	if last.Type != api.BlockToolResult || !strings.Contains(last.Content, `"text":"hi"`) {
		t.Fatalf("the MCP result did not reach the model: %+v", last)
	}
	if n := f.srv.Calls("echo"); n != 1 {
		t.Fatalf("echo reached the server %d times, want 1", n)
	}
}

func TestFailedMCPServerWarnsAndTheConversationStillOpens(t *testing.T) {
	srv := fakeGrafana()
	srv.Down(errors.New("connection refused"))
	f := newMCPFixture(t, mcpOpts{servers: grafanaServers(), tools: []string{"read_file", "grafana__*"}, srv: srv, steps: []provider.Step{text("ok")}})
	if err := f.ws.SelectAgent("reader"); err != nil {
		t.Fatal(err)
	}
	reply, err := f.ws.Chat().Send(context.Background(), "hi")
	if err != nil || reply != "ok" {
		t.Fatalf("the conversation must open and answer: %q, %v", reply, err)
	}
	waitWarning(t, f, "mcp grafana: ")
	waitWarning(t, f, "connection refused")
	for _, d := range f.mock.Calls[0].Tools {
		if strings.HasPrefix(d.Name, "grafana__") {
			t.Fatalf("a failed server must contribute no tools, got %s", d.Name)
		}
	}
}

func TestWorkModeAsksForMCPWritesAndApprovesReads(t *testing.T) {
	srv := fakeGrafana()
	user := &countingGate{}
	f := newMCPFixture(t, mcpOpts{
		servers: grafanaServers(),
		tools:   []string{"read_file", "task_classify", "grafana__echo", "grafana__chart"},
		srv:     srv,
		gate:    user,
		steps: []provider.Step{
			// The flow asks for a classification before any risky call.
			mcpCall("tu_0", "task_classify", `{"size":"small","resumable_from_diff":true,"reason":"test"}`),
			mcpCall("tu_1", "grafana__chart", `{}`),
			mcpCall("tu_2", "grafana__echo", `{"text":"x"}`),
			text("done"),
		},
	})
	chat := openReady(t, f)
	chat.SetWorkMode(true)
	if _, err := chat.Send(context.Background(), "go"); err != nil {
		t.Fatal(err)
	}
	if n := srv.Calls("chart"); n != 0 {
		t.Fatalf("work mode ran an MCP write without asking (%d calls)", n)
	}
	if user.count() != 1 {
		t.Fatalf("the user must be asked once, for the write; asked %d times", user.count())
	}
	if n := srv.Calls("echo"); n != 1 {
		t.Fatalf("work mode must approve the read_only tool, echo ran %d times", n)
	}
}

// gatedTransport makes a server slow: it answers only after release is closed,
// or fails when its context ends first.
func gatedTransport(release <-chan struct{}, next mcp.TransportFunc) mcp.TransportFunc {
	return func(ctx context.Context, server string) (sdk.Transport, error) {
		select {
		case <-release:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
		return next(ctx, server)
	}
}

func TestOpenDoesNotWaitForASlowMCPServer(t *testing.T) {
	release := make(chan struct{})
	srv := fakeGrafana()
	f := newMCPFixture(t, mcpOpts{
		servers:   grafanaServers(),
		tools:     []string{"read_file", "grafana__echo"},
		srv:       srv,
		transport: gatedTransport(release, srv.Transport),
		steps:     []provider.Step{text("first"), text("second")},
	})
	if err := f.ws.SelectAgent("reader"); err != nil {
		t.Fatal(err)
	}
	opened := make(chan Chat, 1)
	go func() { opened <- f.ws.Chat() }()
	var chat Chat
	select {
	case chat = <-opened:
	case <-time.After(3 * time.Second):
		t.Fatal("opening the conversation waited for the slow MCP server")
	}

	if _, err := chat.Send(context.Background(), "before the server answers"); err != nil {
		t.Fatal(err)
	}
	if got := toolNames(f.mock.Calls[0].Tools); slices.Contains(got, "grafana__echo") {
		t.Fatalf("MCP tools were offered before the server answered: %v", got)
	}

	close(release)
	waitNotice(t, f, "mcp grafana: 1 tool ready")
	if _, err := chat.Send(context.Background(), "after the swap"); err != nil {
		t.Fatal(err)
	}
	if got := toolNames(f.mock.Calls[1].Tools); !slices.Contains(got, "grafana__echo") {
		t.Fatalf("MCP tools did not reach the next turn after the swap: %v", got)
	}
}

func TestClosedConversationIgnoresTheLateMCPSwap(t *testing.T) {
	release := make(chan struct{})
	srv := fakeGrafana()
	f := newMCPFixture(t, mcpOpts{
		servers:   grafanaServers(),
		tools:     []string{"read_file", "grafana__echo"},
		srv:       srv,
		transport: gatedTransport(release, srv.Transport),
		steps:     []provider.Step{text("ok")},
	})
	if err := f.ws.SelectAgent("reader"); err != nil {
		t.Fatal(err)
	}
	conv := f.ws.Chat().(*Conversation)
	if err := f.ws.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
	close(release)
	time.Sleep(300 * time.Millisecond)

	for _, n := range func() []string { f.mu.Lock(); defer f.mu.Unlock(); return slices.Clone(f.notes) }() {
		if strings.Contains(n, "tools ready") {
			t.Fatalf("a closed conversation was swapped: note %q", n)
		}
	}
	if _, ok := conv.Agent.Tools.Get("grafana__echo"); ok {
		t.Fatal("the closed conversation's registry gained MCP tools")
	}
}

// countingGate asks the user for every call that needs a question and denies it.
type countingGate struct {
	mu sync.Mutex
	n  int
}

func (g *countingGate) Decide(_ context.Context, req gate.Request) gate.Decision {
	if req.Risk == tool.RiskRead {
		return gate.Allow
	}
	g.mu.Lock()
	g.n++
	g.mu.Unlock()
	return gate.Deny
}

func (g *countingGate) count() int {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.n
}

func TestEditAgentToolsAdmitsOnlyConfiguredMCPTools(t *testing.T) {
	f := newMCPFixture(t, mcpOpts{servers: grafanaServers(), srv: fakeGrafana()})
	tests := []struct {
		name    string
		add     string
		wantErr bool
	}{
		{"allowlisted tool", "grafana__echo", false},
		{"wildcard of a configured server", "grafana__*", false},
		{"unknown server", "other__tool", true},
		{"tool outside the allowlist", "grafana__drop_table", true},
		{"malformed name", "grafana__", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := f.ws.EditAgentTools("reader", []string{tt.add}, nil)
			if (err != nil) != tt.wantErr {
				t.Fatalf("EditAgentTools(%q) error = %v, want error %v", tt.add, err, tt.wantErr)
			}
		})
	}
}

func TestGrantAndRevokeMCPToolRoundTrip(t *testing.T) {
	f := newMCPFixture(t, mcpOpts{servers: grafanaServers(), srv: fakeGrafana()})
	ctx := context.Background()
	out, isErr := admintool.NewGrantTool(f.ws.Manager()).Execute(ctx, `{"agent":"reader","tool":"grafana__echo"}`)
	if isErr || out != "granted grafana__echo to reader" {
		t.Fatalf("grant_tool = %q (error %v)", out, isErr)
	}
	tools, err := f.ws.AgentTools("reader")
	if err != nil || !slices.Contains(tools, "grafana__echo") {
		t.Fatalf("the grant is not saved: %v, %v", tools, err)
	}
	out, isErr = admintool.NewRevokeTool(f.ws.Manager()).Execute(ctx, `{"agent":"reader","tool":"grafana__echo"}`)
	if isErr || out != "revoked grafana__echo from reader" {
		t.Fatalf("revoke_tool = %q (error %v)", out, isErr)
	}
	tools, _ = f.ws.AgentTools("reader")
	if slices.Contains(tools, "grafana__echo") {
		t.Fatalf("the revoke is not saved: %v", tools)
	}
}

func TestMCPServersReportsConnectionStateAndHolders(t *testing.T) {
	t.Run("no servers configured", func(t *testing.T) {
		f := newMCPFixture(t, mcpOpts{srv: fakeGrafana()})
		got, err := f.ws.MCPServers(context.Background())
		if err != nil || len(got) != 0 {
			t.Fatalf("MCPServers = %+v, %v; want none", got, err)
		}
	})

	t.Run("connected server lists its allowlist and holders", func(t *testing.T) {
		f := newMCPFixture(t, mcpOpts{servers: grafanaServers(), tools: []string{"read_file", "grafana__echo"}, srv: fakeGrafana()})
		ctx := context.Background()
		got, err := f.ws.MCPServers(ctx)
		if err != nil || len(got) != 1 || got[0].Connected {
			t.Fatalf("before any use the server must be idle: %+v, %v", got, err)
		}
		n, err := f.ws.MCPConnect(ctx, "grafana")
		if err != nil || n != 2 {
			t.Fatalf("MCPConnect = %d, %v; want 2 tools", n, err)
		}
		got, err = f.ws.MCPServers(ctx)
		if err != nil {
			t.Fatal(err)
		}
		s := got[0]
		if !s.Connected || s.Transport != "http" || len(s.Tools) != 2 {
			t.Fatalf("connected server reported as %+v", s)
		}
		for _, tl := range s.Tools {
			switch tl.Name {
			case "grafana__echo":
				if !tl.ReadOnly || !slices.Equal(tl.Agents, []string{"reader"}) {
					t.Fatalf("echo: %+v", tl)
				}
			case "grafana__chart":
				if tl.ReadOnly || len(tl.Agents) != 0 {
					t.Fatalf("chart: %+v", tl)
				}
			default:
				t.Fatalf("unexpected tool %+v", tl)
			}
		}
	})

	t.Run("failed server carries its error", func(t *testing.T) {
		srv := fakeGrafana()
		srv.Down(errors.New("connection refused"))
		f := newMCPFixture(t, mcpOpts{servers: grafanaServers(), srv: srv})
		ctx := context.Background()
		if _, err := f.ws.MCPConnect(ctx, "grafana"); err == nil || !strings.Contains(err.Error(), "connection refused") {
			t.Fatalf("MCPConnect error = %v", err)
		}
		got, _ := f.ws.MCPServers(ctx)
		if len(got) != 1 || got[0].Connected || !strings.Contains(got[0].Err, "connection refused") {
			t.Fatalf("failed server status = %+v", got)
		}
	})
}
