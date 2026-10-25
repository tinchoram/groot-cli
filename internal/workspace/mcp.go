package workspace

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/tinchoram/groot-cli/internal/catalog"
	"github.com/tinchoram/groot-cli/internal/config"
	"github.com/tinchoram/groot-cli/internal/mcp"
	"github.com/tinchoram/groot-cli/internal/tool"
)

// MCPServerInfo describes one MCP server of the active space, for /mcp.
type MCPServerInfo struct {
	Name      string
	Transport string
	Connected bool
	// Err is the reason of the last failed connect, empty when none.
	Err string
	// Tools are the allowlisted tools the server offers. They are listed only
	// for a connected server.
	Tools []MCPToolInfo
	// Missing are the allowlisted tools the server did not offer at its last listing.
	Missing []string
}

// MCPToolInfo is one allowlisted MCP tool of a connected server.
type MCPToolInfo struct {
	// Name is the model-facing name: <server>__<tool>.
	Name     string
	ReadOnly bool
	// Agents are the agents of the active space that hold the tool.
	Agents []string
}

// mcpNeed is what the agents of a conversation ask of one server.
type mcpNeed struct {
	all   bool
	names map[string]bool // model-facing names
}

// mcpTools returns the MCP tools that a tool list names, and one warning for
// each name that cannot be served. A server is listed once, however many names
// point at it. A server that fails contributes no tools.
func (w *Workspace) mcpTools(ctx context.Context, mgr *mcp.Manager, names []string) ([]tool.Tool, []string) {
	if mgr == nil {
		return nil, nil
	}
	need := map[string]*mcpNeed{}
	var order []string
	for _, n := range names {
		server, name, ok := mcp.SplitToolName(n)
		if !ok {
			continue
		}
		nd, seen := need[server]
		if !seen {
			nd = &mcpNeed{names: map[string]bool{}}
			need[server] = nd
			order = append(order, server)
		}
		if name == "*" {
			nd.all = true
			continue
		}
		nd.names[mcp.ToolName(server, name)] = true
	}
	sort.Strings(order)

	var out []tool.Tool
	var warns []string
	for _, server := range order {
		nd := need[server]
		if !slices.Contains(mgr.Names(), server) {
			warns = append(warns, fmt.Sprintf("mcp %s: not configured or not enabled in this space", server))
			continue
		}
		lctx, cancel := context.WithTimeout(ctx, w.mcpTimeout(server))
		remotes, err := mgr.Tools(lctx, server)
		cancel()
		if err != nil {
			warns = append(warns, fmt.Sprintf("mcp %s: %v", server, err))
			continue
		}
		found := map[string]bool{}
		for _, r := range remotes {
			id := mcp.ToolName(r.Server, r.Name)
			if nd.all || nd.names[id] {
				out = append(out, mcp.AdaptTool(mgr, r))
				found[id] = true
			}
		}
		var missing []string
		for id := range nd.names {
			if !found[id] {
				missing = append(missing, strings.TrimPrefix(id, server+"__"))
			}
		}
		sort.Strings(missing)
		for _, m := range missing {
			warns = append(warns, fmt.Sprintf("mcp %s: tool %s is not offered by the server or not in its allowlist", server, m))
		}
	}
	return out, warns
}

// warnLater delivers warnings to the hook off the caller's goroutine. Callers
// hold the workspace lock, and the hook may block on the UI.
func (w *Workspace) warnLater(key string, msgs []string) {
	if len(msgs) == 0 || w.opts.Hooks.OnWarning == nil {
		return
	}
	warn := keyedWarn(w.opts.Hooks, key)
	go func() {
		for _, m := range msgs {
			warn(m)
		}
	}()
}

// mcpTimeout bounds one listing or connect of a server: its own timeout when
// set, else the default.
func (w *Workspace) mcpTimeout(server string) time.Duration {
	if s, ok := w.opts.Config.MCP.Servers[server]; ok && s.Timeout > 0 {
		return s.Timeout
	}
	return config.DefaultMCPTimeout
}

// checkMCPTools refuses an MCP name that the config does not allow: the server
// must be enabled for this groot and space, and a named tool must be on its
// allowlist. A wildcard needs only the server.
func (w *Workspace) checkMCPTools(names []string) error {
	servers := w.opts.Config.MCP.EnabledServers()
	for _, n := range names {
		server, name, ok := mcp.SplitToolName(n)
		if !ok {
			continue
		}
		s, found := servers[server]
		if !found {
			return fmt.Errorf("mcp tool %s: server %q is not configured in mcp.servers or not enabled in this space", n, server)
		}
		if name != "*" && !slices.Contains(s.Tools, name) {
			return fmt.Errorf("mcp tool %s: %q is not in the allowlist of server %q", n, name, server)
		}
	}
	return nil
}

// activeMCP returns the MCP manager of the active space and the definitions of
// the agents assigned to it.
func (w *Workspace) activeMCP() (*mcp.Manager, []catalog.AgentDef, error) {
	cur, ok := w.opts.Spaces.Current()
	if !ok {
		return nil, nil, errors.New("no space selected")
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	st, err := w.storesFor(cur.Path)
	if err != nil {
		return nil, nil, err
	}
	var defs []catalog.AgentDef
	for _, name := range cur.Agents {
		if d, ok := w.def(name); ok {
			defs = append(defs, d)
		}
	}
	return st.mcp, defs, nil
}

// MCPServers reports the MCP servers of the active space in name order. A
// connected server also lists its allowlisted tools and the agents holding them.
// It connects nothing itself.
func (w *Workspace) MCPServers(ctx context.Context) ([]MCPServerInfo, error) {
	mgr, defs, err := w.activeMCP()
	if err != nil {
		return nil, err
	}
	var out []MCPServerInfo
	for _, s := range mgr.Status() {
		info := MCPServerInfo{Name: s.Name, Transport: s.Transport, Connected: s.Connected, Err: s.LastError}
		if s.Connected {
			lctx, cancel := context.WithTimeout(ctx, w.mcpTimeout(s.Name))
			remotes, err := mgr.Tools(lctx, s.Name)
			cancel()
			if err != nil {
				info.Err = err.Error()
			}
			for _, r := range remotes {
				info.Tools = append(info.Tools, MCPToolInfo{
					Name:     mcp.ToolName(r.Server, r.Name),
					ReadOnly: r.ReadOnly,
					Agents:   holders(defs, r.Server, r.Name),
				})
			}
			info.Missing = mgr.Missing(s.Name)
		}
		out = append(out, info)
	}
	return out, nil
}

// MCPConnect connects the named server of the active space now and lists its
// allowlisted tools. It returns how many tools the allowlist admits.
func (w *Workspace) MCPConnect(ctx context.Context, name string) (int, error) {
	mgr, _, err := w.activeMCP()
	if err != nil {
		return 0, err
	}
	lctx, cancel := context.WithTimeout(ctx, w.mcpTimeout(name))
	defer cancel()
	if err := mgr.Connect(lctx, name); err != nil {
		return 0, err
	}
	remotes, err := mgr.Tools(lctx, name)
	if err != nil {
		return 0, err
	}
	return len(remotes), nil
}

// holders lists the agents whose tools admit the server's tool: by its exact
// name, or by the server wildcard.
func holders(defs []catalog.AgentDef, server, name string) []string {
	id := mcp.ToolName(server, name)
	var out []string
	for _, d := range defs {
		for _, t := range d.Tools {
			s, n, ok := mcp.SplitToolName(t)
			if !ok || s != server {
				continue
			}
			if n == "*" || mcp.ToolName(s, n) == id {
				out = append(out, d.Name)
				break
			}
		}
	}
	return out
}
