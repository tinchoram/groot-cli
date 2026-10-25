package mcp

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

// fixture is an in-process MCP server with the tools the tests need. It counts
// the calls each tool receives, so a test can prove a call never reached it.
type fixture struct {
	srv   *sdk.Server
	mu    sync.Mutex
	calls map[string]int
}

type toolHandler func(context.Context, *sdk.CallToolRequest) (*sdk.CallToolResult, error)

func newFixture(t *testing.T) *fixture {
	t.Helper()
	f := &fixture{
		srv:   sdk.NewServer(&sdk.Implementation{Name: "fixture", Version: "1.0.0"}, nil),
		calls: map[string]int{},
	}
	object := json.RawMessage(`{"type":"object","properties":{"text":{"type":"string"}}}`)

	f.add("echo", "Echo the text back.", object, func(_ context.Context, req *sdk.CallToolRequest) (*sdk.CallToolResult, error) {
		var in struct {
			Text string `json:"text"`
		}
		_ = json.Unmarshal(req.Params.Arguments, &in)
		return textResult("echo: " + in.Text), nil
	})

	f.add("chart", "Render a chart.", object, func(context.Context, *sdk.CallToolRequest) (*sdk.CallToolResult, error) {
		return &sdk.CallToolResult{Content: []sdk.Content{
			&sdk.TextContent{Text: "rendered"},
			&sdk.ImageContent{Data: []byte("png-bytes"), MIMEType: "image/png"},
			&sdk.ResourceLink{URI: "grafana://panel/1", Name: "panel"},
		}}, nil
	})

	f.add("fail", "Always fails inside the tool.", object, func(context.Context, *sdk.CallToolRequest) (*sdk.CallToolResult, error) {
		return &sdk.CallToolResult{IsError: true, Content: []sdk.Content{&sdk.TextContent{Text: "query failed: bad metric"}}}, nil
	})

	f.add("slow", "Sleeps until the caller gives up.", object, func(ctx context.Context, _ *sdk.CallToolRequest) (*sdk.CallToolResult, error) {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(5 * time.Second):
			return textResult("finished"), nil
		}
	})

	f.add("drop_table", "Destructive; never allowlisted in tests.", object, func(context.Context, *sdk.CallToolRequest) (*sdk.CallToolResult, error) {
		return textResult("dropped"), nil
	})
	return f
}

func textResult(s string) *sdk.CallToolResult {
	return &sdk.CallToolResult{Content: []sdk.Content{&sdk.TextContent{Text: s}}}
}

func (f *fixture) add(name, desc string, schema json.RawMessage, h toolHandler) {
	f.srv.AddTool(&sdk.Tool{Name: name, Description: desc, InputSchema: schema}, func(ctx context.Context, req *sdk.CallToolRequest) (*sdk.CallToolResult, error) {
		f.mu.Lock()
		f.calls[name]++
		f.mu.Unlock()
		return h(ctx, req)
	})
}

func (f *fixture) callCount(name string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.calls[name]
}

// dialer connects each new session to the fixture over in-memory transports.
func (f *fixture) dialer(t *testing.T) func(context.Context, string, resolved) (sdk.Transport, error) {
	return func(context.Context, string, resolved) (sdk.Transport, error) {
		clientSide, serverSide := sdk.NewInMemoryTransports()
		ss, err := f.srv.Connect(context.Background(), serverSide, nil)
		if err != nil {
			return nil, err
		}
		t.Cleanup(func() { _ = ss.Close() })
		return clientSide, nil
	}
}

func newClientForTest(t *testing.T) *sdk.Client {
	t.Helper()
	return sdk.NewClient(&sdk.Implementation{Name: "groot-test", Version: "0"}, nil)
}

// newHeaderEchoServer records the Authorization header of each request and
// answers 500, so a handshake fails at once and the test only checks the header.
func newHeaderEchoServer(t *testing.T, seen func(auth string)) *httptest.Server {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen(r.Header.Get("Authorization"))
		http.Error(w, "not an MCP server", http.StatusInternalServerError)
	}))
	t.Cleanup(server.Close)
	return server
}
