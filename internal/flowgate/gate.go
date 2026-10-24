// Package flowgate puts the ODD flow rules in front of the user's permission
// gate. A call the flow refuses is denied with the reason, without asking the
// user. A call the flow allows goes on to the user's gate as before.
package flowgate

import (
	"context"

	"github.com/tinchoram/groot-cli/internal/flow"
	"github.com/tinchoram/groot-cli/internal/gate"
	"github.com/tinchoram/groot-cli/internal/tool"
)

// Gate wraps the user's gate with the flow rules of one conversation.
type Gate struct {
	// Inner is the user's permission policy. Nil means read-only.
	Inner gate.Gate
	Ctl   *flow.Controller
	// Warn receives advisory warnings. Nil drops them.
	Warn func(msg string)

	last string // the reason of the last flow denial
}

// Decide applies the flow rules, then the user's gate.
func (g *Gate) Decide(ctx context.Context, req gate.Request) gate.Decision {
	g.last = ""
	deny, reason, warning := g.Ctl.Check(ctx, req.Tool, req.Input, req.Risk != tool.RiskRead)
	if warning != "" && g.Warn != nil {
		g.Warn(warning)
	}
	if deny {
		g.last = reason
		return gate.Deny
	}
	if g.Inner == nil {
		return gate.ReadOnly{}.Decide(ctx, req)
	}
	return g.Inner.Decide(ctx, req)
}

// Explain returns the flow reason for the last denial, or "" when the user denied.
func (g *Gate) Explain(gate.Request) string { return g.last }
