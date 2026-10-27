package flow

// InferPhase returns the phase a tool call shows the task is in, from the tool
// name and whether the call is read-only. It is a fixed table: nothing is read
// from assistant text. ok is false for tools that say nothing about the phase:
// flow tools that move the phase themselves, memory, feedback, catalog and
// admin tools, and MCP tools.
func InferPhase(tool string, readOnly bool) (Phase, bool) {
	switch tool {
	case "read_file", "list_dir", "glob", "recall", "memory_get", "fetch_url", "kubectl", "read_inbox", "task_status":
		return PhasePlanned, true
	case "bash":
		if readOnly {
			return PhasePlanned, true
		}
		return PhaseImplement, true
	case "write_file":
		return PhaseImplement, true
	case "task_verify":
		return PhaseVerify, true
	case "send_message":
		return PhaseDeliver, true
	}
	return "", false
}
