// Package gate decides whether a tool call may run. The agent asks the gate
// before every tool execution.
package gate

import (
	"context"

	"github.com/tinchoram/groot-cli/internal/tool"
)

// Decision is the gate's verdict for one call.
type Decision int

const (
	Allow Decision = iota
	Deny
)

func (d Decision) String() string {
	if d == Allow {
		return "allow"
	}
	return "deny"
}

// Request describes a pending tool call.
type Request struct {
	Tool   string
	Input  string
	Risk   tool.Risk
	Detail string // command or diff preview shown to the user
	// Tainted means the input carries untrusted content: the model copied the
	// id of a recent guard envelope into the call. A tainted call is never
	// auto-approved by work mode or a session grant, and always reaches the user.
	Tainted bool
}

// CanGrant reports whether the user may approve this call for the rest of the
// session. Admin tools never are, and neither is a tainted call.
func (r Request) CanGrant() bool { return Grantable(r.Tool) && !r.Tainted }

// Gate is the permission policy port.
type Gate interface {
	Decide(ctx context.Context, req Request) Decision
}

// Explainer is implemented by a gate that can say why it denied the last call.
// The agent returns that reason to the model instead of a bare refusal. An
// empty reason means the denial came from the user.
type Explainer interface {
	Explain(req Request) string
}

// AutoApprove allows every call. Use only in trusted, scripted runs.
type AutoApprove struct{}

func (AutoApprove) Decide(context.Context, Request) Decision { return Allow }

// ReadOnly allows read-risk calls and denies everything else.
type ReadOnly struct{}

func (ReadOnly) Decide(_ context.Context, req Request) Decision {
	if req.Risk == tool.RiskRead {
		return Allow
	}
	return Deny
}

// Interactive allows reads and asks Confirm about writes and executions.
// Without a Confirm function it denies, so it can never allow silently.
type Interactive struct {
	Confirm func(req Request) bool
}

func (g Interactive) Decide(_ context.Context, req Request) Decision {
	if req.Risk == tool.RiskRead && !req.Tainted {
		return Allow
	}
	if g.Confirm == nil {
		return Deny
	}
	if g.Confirm(req) {
		return Allow
	}
	return Deny
}
