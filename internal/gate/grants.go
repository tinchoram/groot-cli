package gate

import (
	"context"
	"slices"
	"sync"
)

// Grants are the tools the user approved for the rest of the session in one
// conversation. A grant covers only the calls work mode would approve in the
// same scope: writes inside the space and bash outside the always-ask list.
// Admin tools are never grantable. It is safe for concurrent use.
type Grants struct {
	mu    sync.Mutex
	tools map[string]bool
}

// NewGrants returns an empty set of session grants.
func NewGrants() *Grants { return &Grants{tools: map[string]bool{}} }

// Grantable reports whether a tool may be approved for the session. The admin
// tools never are: they keep asking the user on every call.
func Grantable(tool string) bool { return !adminChanges[tool] }

// Grant allows tool for the rest of the session. It reports false for tools that
// are not grantable.
func (g *Grants) Grant(tool string) bool {
	if !Grantable(tool) {
		return false
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	g.tools[tool] = true
	return true
}

// Has reports whether tool is granted.
func (g *Grants) Has(tool string) bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.tools[tool]
}

// List returns the granted tools in alphabetical order.
func (g *Grants) List() []string {
	g.mu.Lock()
	defer g.mu.Unlock()
	out := make([]string, 0, len(g.tools))
	for t := range g.tools {
		out = append(out, t)
	}
	slices.Sort(out)
	return out
}

// Clear drops every grant.
func (g *Grants) Clear() {
	g.mu.Lock()
	defer g.mu.Unlock()
	clear(g.tools)
}

// Covers reports whether work mode's scope rules allow this call, ignoring the
// on/off switch and the limits.
func (s *WorkState) Covers(req Request) bool { return s.approves(req) }

// Session approves the calls of granted tools, within the scope work mode
// gives them, before the user's gate is asked. Everything else goes to Inner.
type Session struct {
	Inner  Gate
	Grants *Grants
	Work   *WorkState
}

// Covers reports whether the session grants approve this call.
func (s Session) Covers(req Request) bool {
	return s.Grants != nil && s.Work != nil && s.Grants.Has(req.Tool) && s.Work.Covers(req)
}

// Decide allows the calls the session covers and leaves the rest to Inner.
func (s Session) Decide(ctx context.Context, req Request) Decision {
	if s.Covers(req) {
		return Allow
	}
	return s.Inner.Decide(ctx, req)
}
