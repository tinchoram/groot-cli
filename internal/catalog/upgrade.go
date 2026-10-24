package catalog

import "slices"

// ManagedTools is the tool set every agent should carry: messaging and the
// task flow are how agents work together, and the memory tools keep their
// context. Upgrades add these when an agent lacks them.
func ManagedTools() []string {
	return []string{
		"send_message", "read_inbox",
		"task_classify", "task_open", "task_advance", "task_status",
		"recall", "remember",
		"list_dir", "glob",
	}
}

// Upgrade adds the managed tools that d lacks. It never removes a tool and keeps
// the order of the existing ones. It returns the definition and the tools added.
func Upgrade(d AgentDef) (AgentDef, []string) {
	tools := slices.Clone(d.Tools)
	var added []string
	for _, t := range ManagedTools() {
		if !slices.Contains(tools, t) {
			tools = append(tools, t)
			added = append(added, t)
		}
	}
	d.Tools = tools
	return d, added
}
