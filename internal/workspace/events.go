package workspace

import (
	"github.com/tinchoram/groot-cli/internal/gate"
	"github.com/tinchoram/groot-cli/internal/guard"
)

// keyedText binds the text hook to one conversation key.
func keyedText(h Hooks, key string) func(string) {
	return func(delta string) {
		if h.OnText != nil {
			h.OnText(key, delta)
		}
	}
}

// keyedTool binds the tool-call hook to one conversation key.
func keyedTool(h Hooks, key string) func(string, string) {
	return func(name, input string) {
		if h.OnToolCall != nil {
			h.OnToolCall(key, name, input)
		}
	}
}

// keyedWarn binds the warning hook to one conversation key.
func keyedWarn(h Hooks, key string) func(string) {
	return func(msg string) {
		if h.OnWarning != nil {
			h.OnWarning(key, msg)
		}
	}
}

// confirmingGate makes the interactive policy ask the user through the hook
// of this conversation, so the request reaches the transcript it belongs to.
// Other policies are returned unchanged.
// A call that work mode or a session grant approves prints as [auto].
func autoAwareTool(h Hooks, key string, work *gate.WorkState, sess gate.Session, g *guard.Guard) func(string, string) {
	return func(name, input string) {
		req := gate.Request{Tool: name, Input: input, Tainted: g.Tainted(input)}
		if work.Would(req) || sess.Covers(req) {
			if h.OnAuto != nil {
				h.OnAuto(key, name, input)
			}
			return
		}
		if h.OnToolCall != nil {
			h.OnToolCall(key, name, input)
		}
	}
}

func confirmingGate(inner gate.Gate, h Hooks, key string) gate.Gate {
	if _, ok := inner.(gate.Interactive); !ok || h.OnConfirm == nil {
		return inner
	}
	return gate.Interactive{Confirm: func(req gate.Request) bool {
		return h.OnConfirm(key, req)
	}}
}
