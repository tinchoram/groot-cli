// Package mcptest serves an in-process MCP server for tests of the packages
// that use internal/mcp. Its sessions run over in-memory transports, so no
// process or network is started.
package mcptest

import (
	"context"
	"encoding/json"
	"errors"
	"sync"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

// Tool is one tool the fake server offers. Handler gets the raw arguments and
// returns the text of the result.
type Tool struct {
	Name        string
	Description string
	Handler     func(args json.RawMessage) string
}

// Server is a fake MCP server. Calls counts the calls each tool receives, so a
// test can prove that a refused call never arrived.
type Server struct {
	srv *sdk.Server

	mu    sync.Mutex
	calls map[string]int
	down  error
}

// New returns a fake server offering tools.
func New(tools ...Tool) *Server {
	s := &Server{
		srv:   sdk.NewServer(&sdk.Implementation{Name: "mcptest", Version: "1.0.0"}, nil),
		calls: map[string]int{},
	}
	object := json.RawMessage(`{"type":"object","properties":{"text":{"type":"string"}}}`)
	for _, t := range tools {
		s.add(t, object)
	}
	return s
}

func (s *Server) add(t Tool, schema json.RawMessage) {
	s.srv.AddTool(&sdk.Tool{Name: t.Name, Description: t.Description, InputSchema: schema},
		func(_ context.Context, req *sdk.CallToolRequest) (*sdk.CallToolResult, error) {
			s.mu.Lock()
			s.calls[t.Name]++
			s.mu.Unlock()
			text := ""
			if t.Handler != nil {
				text = t.Handler(req.Params.Arguments)
			}
			return &sdk.CallToolResult{Content: []sdk.Content{&sdk.TextContent{Text: text}}}, nil
		})
}

// Down makes every later transport fail with err, as a server that is not
// running does. Nil brings the server back.
func (s *Server) Down(err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.down = err
}

// Calls returns how many times the named tool was called.
func (s *Server) Calls(name string) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.calls[name]
}

// Transport is an mcp.TransportFunc: it connects a new client session to the
// fake server.
func (s *Server) Transport(context.Context, string) (sdk.Transport, error) {
	s.mu.Lock()
	down := s.down
	s.mu.Unlock()
	if down != nil {
		return nil, down
	}
	clientSide, serverSide := sdk.NewInMemoryTransports()
	if _, err := s.srv.Connect(context.Background(), serverSide, nil); err != nil {
		return nil, errors.Join(errors.New("in-memory server"), err)
	}
	return clientSide, nil
}
