package workspace

import (
	"github.com/tinchoram/groot-cli/internal/guard"
	"github.com/tinchoram/groot-cli/internal/tool"
)

// newConversationGuard returns the guard of one conversation in the configured
// mode. Config validation refuses unknown modes; an empty mode means warn.
func newConversationGuard(mode string) *guard.Guard {
	m, _ := guard.ParseMode(mode)
	return guard.New(m)
}

// bindGuard gives the tools that wrap their own output the conversation's guard.
func bindGuard(reg *tool.Registry, g *guard.Guard) {
	t, ok := reg.Get("read_inbox")
	if !ok {
		return
	}
	if ri, ok := t.(*tool.ReadInbox); ok {
		ri.Guard = g
	}
}
