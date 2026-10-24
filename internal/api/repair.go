package api

// cancelledText is the content of the result recorded for a tool call that
// never produced one: the turn was cut before the tool finished.
const cancelledText = "cancelled: no result recorded"

// RepairHistory makes every tool_use answered by a tool_result block in the
// user message right after it. It returns the repaired copy and whether
// anything changed. See Repair.
func RepairHistory(msgs []Message) ([]Message, bool) {
	out, _, changed := Repair(msgs)
	return out, changed
}

// Repair is RepairHistory with more detail. Missing results are filled with
// cancelled errors: they join the following user message, and the existing
// results of that message stay first. When no user message follows the
// assistant message, a new user message is inserted right after it. inserted
// holds the indexes in out of those new messages; a merge adds none. msgs is
// not changed.
func Repair(msgs []Message) (out []Message, inserted []int, changed bool) {
	out = make([]Message, 0, len(msgs)+1)
	for i := 0; i < len(msgs); i++ {
		m := msgs[i]
		uses := toolUseIDs(m)
		if m.Role != RoleAssistant || len(uses) == 0 {
			out = append(out, m)
			continue
		}

		var next *Message
		if i+1 < len(msgs) && msgs[i+1].Role == RoleUser {
			next = &msgs[i+1]
		}
		answered := map[string]bool{}
		if next != nil {
			for _, b := range next.Content {
				if b.Type == BlockToolResult {
					answered[b.ToolUseID] = true
				}
			}
		}
		var missing []Block
		for _, id := range uses {
			if !answered[id] {
				missing = append(missing, CancelledResult(id))
				answered[id] = true
			}
		}

		out = append(out, m)
		if len(missing) == 0 {
			continue
		}
		changed = true
		if next == nil {
			out = append(out, Message{Role: RoleUser, Content: missing})
			inserted = append(inserted, len(out)-1)
			continue
		}
		out = append(out, Message{Role: RoleUser, Content: withResults(next.Content, missing)})
		i++ // the following user message is consumed by the merge
	}
	return out, inserted, changed
}

// toolUseIDs lists the ids of the tool_use blocks of m, in order.
func toolUseIDs(m Message) []string {
	var ids []string
	for _, b := range m.Content {
		if b.Type == BlockToolUse {
			ids = append(ids, b.ID)
		}
	}
	return ids
}

// CancelledResult is the error result recorded for a tool call that did not
// finish, so every tool_use keeps its tool_result.
func CancelledResult(id string) Block {
	return Block{Type: BlockToolResult, ToolUseID: id, Content: cancelledText, IsError: true}
}

// withResults returns content with the missing results placed after the results
// it already has and before its other blocks. content is not changed.
func withResults(content, missing []Block) []Block {
	results := make([]Block, 0, len(content)+len(missing))
	var rest []Block
	for _, b := range content {
		if b.Type == BlockToolResult {
			results = append(results, b)
		} else {
			rest = append(rest, b)
		}
	}
	results = append(results, missing...)
	return append(results, rest...)
}
