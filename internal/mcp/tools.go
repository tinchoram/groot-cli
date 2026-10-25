package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"

	"github.com/tinchoram/groot-cli/internal/api"
	"github.com/tinchoram/groot-cli/internal/tool"
)

const (
	toolNameMax    = 64
	remoteDescMax  = 400
	previewArgsMax = 120
	separator      = "__"
)

var unsafeName = regexp.MustCompile(`[^a-zA-Z0-9_-]`)

// ToolName is the model-facing name of a remote tool: server__tool, with every
// rune outside [a-zA-Z0-9_-] replaced by "_" and the result cut to 64 runes.
func ToolName(server, tool string) string {
	return cutRunes(unsafeName.ReplaceAllString(server+separator+tool, "_"), toolNameMax)
}

// SplitToolName splits a name made by ToolName at its first separator. Server
// names never contain an underscore, so the first separator is the right one.
func SplitToolName(name string) (server, tool string, ok bool) {
	i := strings.Index(name, separator)
	if i <= 0 {
		return "", "", false
	}
	return name[:i], name[i+len(separator):], true
}

// AdaptTool exposes one remote tool to the agent. Its output is untrusted text.
func AdaptTool(m *Manager, r Remote) tool.Tool {
	return &remoteTool{m: m, r: r}
}

// AdaptServer connects the named server and adapts its allowlisted tools.
func AdaptServer(ctx context.Context, m *Manager, name string) ([]tool.Tool, error) {
	remotes, err := m.Tools(ctx, name)
	if err != nil {
		return nil, err
	}
	out := make([]tool.Tool, 0, len(remotes))
	for _, r := range remotes {
		out = append(out, AdaptTool(m, r))
	}
	return out, nil
}

// AdaptAll adapts every configured server. A server that fails contributes no
// tools and one error, so the caller decides how to show it.
func AdaptAll(ctx context.Context, m *Manager) ([]tool.Tool, []error) {
	var tools []tool.Tool
	var errs []error
	for _, name := range m.Names() {
		ts, err := AdaptServer(ctx, m, name)
		if err != nil {
			errs = append(errs, err)
			continue
		}
		tools = append(tools, ts...)
	}
	return tools, errs
}

type remoteTool struct {
	m *Manager
	r Remote
}

var _ tool.Tool = (*remoteTool)(nil)

func (t *remoteTool) Definition() api.ToolDef {
	schema := t.r.InputSchema
	if len(schema) == 0 || string(schema) == "null" {
		schema = json.RawMessage(`{"type":"object"}`)
	}
	return api.ToolDef{
		Name:        ToolName(t.r.Server, t.r.Name),
		Description: "[mcp:" + t.r.Server + "] " + cutRunes(t.r.Description, remoteDescMax),
		InputSchema: schema,
	}
}

// Risk follows the read_only list: a tool the config does not mark read-only
// is treated as a write, and the permission gate asks before it runs.
func (t *remoteTool) Risk() tool.Risk {
	if t.r.ReadOnly {
		return tool.RiskRead
	}
	return tool.RiskWrite
}

func (t *remoteTool) RiskFor(string) tool.Risk { return t.Risk() }

// Untrusted: remote servers write the text the model reads back.
func (*remoteTool) Untrusted() bool { return true }

func (t *remoteTool) Preview(input string) string {
	head := t.r.Server + " " + t.r.Name
	args := compactJSON(input)
	if args == "" {
		return head
	}
	return head + " " + cutRunes(args, previewArgsMax)
}

func (t *remoteTool) Execute(ctx context.Context, input string) (string, bool) {
	res, err := t.m.Call(ctx, t.r.Server, t.r.Name, json.RawMessage(input))
	if err != nil {
		return fmt.Sprintf("mcp %s/%s: %v", t.r.Server, t.r.Name, err), true
	}
	return res.Text, res.IsError
}

// compactJSON returns s without insignificant whitespace, or s trimmed when it
// is not JSON.
func compactJSON(s string) string {
	var buf bytes.Buffer
	if err := json.Compact(&buf, []byte(s)); err != nil {
		return strings.TrimSpace(s)
	}
	return buf.String()
}

// cutRunes returns at most n runes of s.
func cutRunes(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n])
}
