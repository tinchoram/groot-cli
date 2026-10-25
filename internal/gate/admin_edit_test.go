package gate

import "testing"

// Work mode must keep asking for the tools that delete, rename, move and copy
// spaces and agents, as it does for the other admin changes.
func TestWorkModeNeverApprovesTheEditTools(t *testing.T) {
	s := &WorkState{}
	for _, name := range []string{"delete_space", "edit_space", "delete_agent", "edit_agent", "move_agent", "copy_agent"} {
		if s.approves(Request{Tool: name}) {
			t.Errorf("work mode approves %s without asking", name)
		}
	}
}
