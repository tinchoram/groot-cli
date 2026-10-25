// Package mcp connects groot to MCP servers and exposes their allowlisted tools
// to the agent. Servers are defined in the global config only. Each server gets
// one lazily connected session.
//
// Trust boundary: stdio servers run as ordinary processes outside groot's bash
// sandbox. They are trusted because the user listed them in the global config,
// so they start with a clean environment (only PATH, HOME and the configured
// values) and the space path as their working directory, nothing else.
package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"sort"
	"strings"
	"sync"
	"time"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/tinchoram/groot-cli/internal/config"
)

// reconnectBackoff is how long a failed connect waits before the next attempt.
const reconnectBackoff = 10 * time.Second

// clientName is the implementation name groot reports to MCP servers.
const clientName = "groot"

// Options configure a Manager. The zero value uses the process environment,
// the wall clock and the default version string.
type Options struct {
	// Logf receives connect failures. Nil discards them.
	Logf func(format string, args ...any)
	// Env reads the process environment. Nil uses os.LookupEnv.
	Env func(key string) (string, bool)
	// Now is the clock used for the reconnect backoff. Nil uses time.Now.
	Now func() time.Time
	// Dir is the working directory of stdio servers: the space path.
	Dir string
	// Version is reported to servers as the client version.
	Version string

	// dial replaces the real transports in tests.
	dial dialFunc
}

type dialFunc func(ctx context.Context, name string, r resolved) (sdk.Transport, error)

// Remote is one tool a server offers that the allowlist admits.
type Remote struct {
	Server      string
	Name        string
	Description string
	InputSchema json.RawMessage
	ReadOnly    bool
}

// Result is the outcome of one tool call. Text joins the text content with
// newlines and notes each omitted non-text block. Omitted lists the MIME type
// or resource of each such block.
type Result struct {
	Text    string
	IsError bool
	Omitted []string
}

// ServerStatus reports one server for doctor and the /mcp view.
type ServerStatus struct {
	Name      string
	Transport string
	Connected bool
	LastError string
	ToolCount int
	Missing   []string
}

// Manager owns the sessions of every configured server.
type Manager struct {
	opts    Options
	names   []string
	servers map[string]*server
}

type server struct {
	name string
	cfg  config.MCPServer

	// connectMu serialises connect and close for this server.
	connectMu sync.Mutex
	// stateMu guards the fields below, so Status never waits for a connect.
	stateMu   sync.Mutex
	session   *sdk.ClientSession
	lastErr   string
	nextTry   time.Time
	toolCount int
	missing   []string
}

// New returns a manager for servers. It connects nothing until a tool is used.
func New(servers map[string]config.MCPServer, opts Options) *Manager {
	if opts.Env == nil {
		opts.Env = os.LookupEnv
	}
	if opts.Now == nil {
		opts.Now = time.Now
	}
	if opts.Logf == nil {
		opts.Logf = func(string, ...any) {}
	}
	if opts.Version == "" {
		opts.Version = "dev"
	}
	m := &Manager{opts: opts, servers: make(map[string]*server, len(servers))}
	for name, cfg := range servers {
		m.servers[name] = &server{name: name, cfg: cfg}
		m.names = append(m.names, name)
	}
	sort.Strings(m.names)
	return m
}

// Connect starts the session of the named server if it is not live. A failed
// connect is not retried until reconnectBackoff has passed.
func (m *Manager) Connect(ctx context.Context, name string) error {
	s, err := m.server(name)
	if err != nil {
		return err
	}
	return m.connect(ctx, s)
}

func (m *Manager) connect(ctx context.Context, s *server) error {
	s.connectMu.Lock()
	defer s.connectMu.Unlock()

	if s.live() != nil {
		return nil
	}
	now := m.opts.Now()
	s.stateMu.Lock()
	next, lastErr := s.nextTry, s.lastErr
	s.stateMu.Unlock()
	if now.Before(next) {
		return fmt.Errorf("mcp server %s: retry after backoff (last error: %s)", s.name, lastErr)
	}

	session, err := m.dialSession(ctx, s)
	s.stateMu.Lock()
	defer s.stateMu.Unlock()
	if err != nil {
		s.lastErr = err.Error()
		s.nextTry = now.Add(reconnectBackoff)
		m.opts.Logf("mcp server %s: connect failed: %v", s.name, err)
		return fmt.Errorf("mcp server %s: %w", s.name, err)
	}
	s.session = session
	s.lastErr = ""
	s.nextTry = time.Time{}
	return nil
}

func (m *Manager) dialSession(ctx context.Context, s *server) (*sdk.ClientSession, error) {
	r, err := resolve(s.name, s.cfg, m.opts.Env, m.opts.Dir)
	if err != nil {
		return nil, err
	}
	var transport sdk.Transport
	if m.opts.dial != nil {
		transport, err = m.opts.dial(ctx, s.name, r)
	} else {
		transport, err = newTransport(r)
	}
	if err != nil {
		return nil, err
	}
	client := sdk.NewClient(&sdk.Implementation{Name: clientName, Version: m.opts.Version}, nil)
	// The handshake gets the server timeout. It is detached from the caller's
	// cancellation, so a session outlives the call that opened it.
	hctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), s.timeout())
	defer cancel()
	return client.Connect(hctx, transport, nil)
}

// Tools lists the tools of the named server that the allowlist admits. Allowed
// tools the server does not offer are reported by Missing.
func (m *Manager) Tools(ctx context.Context, name string) ([]Remote, error) {
	s, err := m.server(name)
	if err != nil {
		return nil, err
	}
	if err := m.connect(ctx, s); err != nil {
		return nil, err
	}
	session := s.live()
	if session == nil {
		return nil, fmt.Errorf("mcp server %s: not connected", name)
	}

	allowed := make(map[string]bool, len(s.cfg.Tools))
	for _, t := range s.cfg.Tools {
		allowed[t] = true
	}
	readOnly := make(map[string]bool, len(s.cfg.ReadOnly))
	for _, t := range s.cfg.ReadOnly {
		readOnly[t] = true
	}

	offered := map[string]bool{}
	var out []Remote
	cursor := ""
	for page := 0; page < maxListPages; page++ {
		res, err := session.ListTools(ctx, &sdk.ListToolsParams{Cursor: cursor})
		if err != nil {
			return nil, fmt.Errorf("mcp server %s: list tools: %w", name, err)
		}
		for _, t := range res.Tools {
			offered[t.Name] = true
			if !allowed[t.Name] {
				continue
			}
			out = append(out, Remote{
				Server:      name,
				Name:        t.Name,
				Description: t.Description,
				InputSchema: schemaJSON(t.InputSchema),
				ReadOnly:    readOnly[t.Name],
			})
		}
		if res.NextCursor == "" {
			break
		}
		cursor = res.NextCursor
	}

	var missing []string
	for _, t := range s.cfg.Tools {
		if !offered[t] {
			missing = append(missing, t)
		}
	}
	s.stateMu.Lock()
	s.toolCount = len(out)
	s.missing = missing
	s.stateMu.Unlock()
	return out, nil
}

// maxListPages stops a server that keeps returning cursors.
const maxListPages = 100

// Missing returns the allowlisted tools the named server did not offer at its
// last listing.
func (m *Manager) Missing(name string) []string {
	s, err := m.server(name)
	if err != nil {
		return nil
	}
	s.stateMu.Lock()
	defer s.stateMu.Unlock()
	return append([]string(nil), s.missing...)
}

// Call runs one allowlisted tool with the server timeout. A tool outside the
// allowlist is refused before any connection is made.
func (m *Manager) Call(ctx context.Context, serverName, tool string, args json.RawMessage) (Result, error) {
	s, err := m.server(serverName)
	if err != nil {
		return Result{}, err
	}
	if !contains(s.cfg.Tools, tool) {
		return Result{}, fmt.Errorf("tool %s is not in the allowlist of server %s", tool, serverName)
	}
	if len(bytes.TrimSpace(args)) == 0 {
		args = json.RawMessage("{}")
	}
	if !json.Valid(args) {
		return Result{}, errors.New("invalid arguments: not JSON")
	}

	timeout := s.timeout()
	cctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	if err := m.connect(cctx, s); err != nil {
		return Result{}, err
	}
	session := s.live()
	if session == nil {
		return Result{}, fmt.Errorf("mcp server %s: not connected", serverName)
	}

	res, err := session.CallTool(cctx, &sdk.CallToolParams{Name: tool, Arguments: args})
	if err != nil {
		if errors.Is(err, sdk.ErrConnectionClosed) {
			s.forget(session)
		}
		if errors.Is(cctx.Err(), context.DeadlineExceeded) {
			return Result{}, fmt.Errorf("tool %s timed out after %s", tool, timeout)
		}
		return Result{}, fmt.Errorf("tool %s: %w", tool, err)
	}
	return convertResult(res), nil
}

// Status reports every server in name order.
func (m *Manager) Status() []ServerStatus {
	out := make([]ServerStatus, 0, len(m.names))
	for _, name := range m.names {
		s := m.servers[name]
		s.stateMu.Lock()
		out = append(out, ServerStatus{
			Name:      name,
			Transport: s.cfg.Transport,
			Connected: s.session != nil,
			LastError: s.lastErr,
			ToolCount: s.toolCount,
			Missing:   append([]string(nil), s.missing...),
		})
		s.stateMu.Unlock()
	}
	return out
}

// Close ends every live session. A later call reconnects on demand.
func (m *Manager) Close() error {
	var errs []error
	for _, name := range m.names {
		s := m.servers[name]
		s.connectMu.Lock()
		s.stateMu.Lock()
		session := s.session
		s.session = nil
		s.stateMu.Unlock()
		if session != nil {
			if err := session.Close(); err != nil {
				errs = append(errs, fmt.Errorf("mcp server %s: close: %w", name, err))
			}
		}
		s.connectMu.Unlock()
	}
	return errors.Join(errs...)
}

// Names lists the configured servers in name order.
func (m *Manager) Names() []string {
	return append([]string(nil), m.names...)
}

func (m *Manager) server(name string) (*server, error) {
	s, ok := m.servers[name]
	if !ok {
		return nil, fmt.Errorf("mcp: unknown server %q", name)
	}
	return s, nil
}

func (s *server) live() *sdk.ClientSession {
	s.stateMu.Lock()
	defer s.stateMu.Unlock()
	return s.session
}

func (s *server) forget(session *sdk.ClientSession) {
	s.stateMu.Lock()
	defer s.stateMu.Unlock()
	if s.session == session {
		s.session = nil
	}
}

func (s *server) timeout() time.Duration {
	if s.cfg.Timeout > 0 {
		return s.cfg.Timeout
	}
	return config.DefaultMCPTimeout
}

func contains(list []string, want string) bool {
	for _, v := range list {
		if v == want {
			return true
		}
	}
	return false
}

// schemaJSON turns the SDK's decoded schema back into raw JSON.
func schemaJSON(schema any) json.RawMessage {
	if schema == nil {
		return nil
	}
	b, err := json.Marshal(schema)
	if err != nil {
		return nil
	}
	return b
}

// convertResult joins the text blocks and notes every other block.
func convertResult(res *sdk.CallToolResult) Result {
	var lines []string
	var omitted []string
	for _, c := range res.Content {
		switch v := c.(type) {
		case *sdk.TextContent:
			lines = append(lines, v.Text)
		case *sdk.ImageContent:
			omitted = append(omitted, v.MIMEType)
		case *sdk.AudioContent:
			omitted = append(omitted, v.MIMEType)
		case *sdk.ResourceLink:
			omitted = append(omitted, "resource: "+v.URI)
		case *sdk.EmbeddedResource:
			if v.Resource != nil && v.Resource.Text != "" {
				lines = append(lines, v.Resource.Text)
				continue
			}
			uri := ""
			if v.Resource != nil {
				uri = v.Resource.URI
			}
			omitted = append(omitted, "resource: "+uri)
		default:
			omitted = append(omitted, "unknown content")
		}
	}
	for _, o := range omitted {
		lines = append(lines, "[non-text content omitted: "+o+"]")
	}
	return Result{Text: strings.Join(lines, "\n"), IsError: res.IsError, Omitted: omitted}
}

// resolved is a server with every ${VAR} expanded and the stdio environment
// built. It holds the secrets, so it is never logged.
type resolved struct {
	Transport string
	Command   string
	Args      []string
	Env       []string
	Dir       string
	URL       string
	Headers   map[string]string
}

// resolve expands the connect-time values of a server from lookup. An unset
// variable is an error that names it.
func resolve(name string, s config.MCPServer, lookup func(string) (string, bool), dir string) (resolved, error) {
	r := resolved{Transport: s.Transport, Dir: dir}
	switch s.Transport {
	case "stdio":
		command, err := config.ExpandStrict(s.Command, lookup)
		if err != nil {
			return resolved{}, err
		}
		args, err := expandAll(s.Args, lookup)
		if err != nil {
			return resolved{}, err
		}
		env, err := buildEnv(s.Env, lookup)
		if err != nil {
			return resolved{}, err
		}
		r.Command, r.Args, r.Env = command, args, env
	case "http":
		url, err := config.ExpandStrict(s.URL, lookup)
		if err != nil {
			return resolved{}, err
		}
		headers := make(map[string]string, len(s.Headers))
		for k, v := range s.Headers {
			ev, err := config.ExpandStrict(v, lookup)
			if err != nil {
				return resolved{}, err
			}
			headers[k] = ev
		}
		r.URL, r.Headers = url, headers
	default:
		return resolved{}, fmt.Errorf("transport %q is invalid", s.Transport)
	}
	return r, nil
}

func expandAll(values []string, lookup func(string) (string, bool)) ([]string, error) {
	out := make([]string, 0, len(values))
	for _, v := range values {
		ev, err := config.ExpandStrict(v, lookup)
		if err != nil {
			return nil, err
		}
		out = append(out, ev)
	}
	return out, nil
}

// buildEnv returns the environment of a stdio server: PATH and HOME from the
// process, plus the configured values. Nothing else is inherited. The result is
// sorted, so it is deterministic.
func buildEnv(configured map[string]string, lookup func(string) (string, bool)) ([]string, error) {
	env := map[string]string{}
	for _, k := range []string{"PATH", "HOME"} {
		if v, ok := lookup(k); ok {
			env[k] = v
		}
	}
	for k, v := range configured {
		ev, err := config.ExpandStrict(v, lookup)
		if err != nil {
			return nil, err
		}
		env[k] = ev
	}
	keys := make([]string, 0, len(env))
	for k := range env {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	out := make([]string, 0, len(keys))
	for _, k := range keys {
		out = append(out, k+"="+env[k])
	}
	return out, nil
}

// newTransport builds the real transport of a resolved server.
func newTransport(r resolved) (sdk.Transport, error) {
	switch r.Transport {
	case "stdio":
		cmd := exec.Command(r.Command, r.Args...)
		cmd.Env = r.Env
		cmd.Dir = r.Dir
		return &sdk.CommandTransport{Command: cmd}, nil
	case "http":
		return newHTTPTransport(r), nil
	}
	return nil, fmt.Errorf("transport %q is invalid", r.Transport)
}

// newHTTPTransport is the streamable HTTP transport with the configured
// headers on every request. Retries are off: the manager re-dials after its
// backoff instead.
func newHTTPTransport(r resolved) sdk.Transport {
	return &sdk.StreamableClientTransport{
		Endpoint: r.URL,
		HTTPClient: &http.Client{
			Transport: &headerTransport{headers: r.Headers, base: http.DefaultTransport},
		},
		MaxRetries: -1,
	}
}

type headerTransport struct {
	headers map[string]string
	base    http.RoundTripper
}

func (h *headerTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	clone := req.Clone(req.Context())
	for k, v := range h.headers {
		clone.Header.Set(k, v)
	}
	return h.base.RoundTrip(clone)
}
