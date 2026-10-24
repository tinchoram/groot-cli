package compact

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/tinchoram/groot-cli/internal/api"
)

func msgText(role api.Role, s string) api.Message {
	return api.Message{Role: role, Content: []api.Block{{Type: api.BlockText, Text: s}}}
}

func msgUse(id string) api.Message {
	return api.Message{Role: api.RoleAssistant, Content: []api.Block{
		{Type: api.BlockToolUse, ID: id, Name: "bash", Input: json.RawMessage(`{}`)}}}
}

func msgResult(id string) api.Message {
	return api.Message{Role: api.RoleUser, Content: []api.Block{
		{Type: api.BlockToolResult, ToolUseID: id, Content: "ok"}}}
}

// consistentPairs reports whether every tool_use is answered in the user message
// right after it, and every tool_result answers a tool_use right before it.
func consistentPairs(msgs []api.Message) bool {
	for i, m := range msgs {
		uses := map[string]bool{}
		for _, b := range m.Content {
			if b.Type == api.BlockToolUse {
				uses[b.ID] = true
			}
		}
		if m.Role == api.RoleUser {
			for _, b := range m.Content {
				if b.Type == api.BlockToolResult && (i == 0 || !prevUses(msgs[i-1], b.ToolUseID)) {
					return false
				}
			}
		}
		if len(uses) == 0 {
			continue
		}
		if i+1 >= len(msgs) || msgs[i+1].Role != api.RoleUser {
			return false
		}
		answered := map[string]bool{}
		for _, b := range msgs[i+1].Content {
			if b.Type == api.BlockToolResult {
				answered[b.ToolUseID] = true
			}
		}
		for id := range uses {
			if !answered[id] {
				return false
			}
		}
	}
	return true
}

func prevUses(m api.Message, id string) bool {
	for _, b := range m.Content {
		if b.Type == api.BlockToolUse && b.ID == id {
			return true
		}
	}
	return false
}

func TestRepairThenCompactKeepsToolPairsWhole(t *testing.T) {
	history := []api.Message{
		msgText(api.RoleUser, "one"),
		msgUse("a"),
		msgResult("a"),
		msgText(api.RoleAssistant, "saw it"),
		msgText(api.RoleUser, "two"),
		msgUse("b"), // the turn was cut here: no result was recorded
	}
	repaired, changed := api.RepairHistory(history)
	if !changed || !consistentPairs(repaired) {
		t.Fatalf("repair must leave the history consistent: changed=%v", changed)
	}
	for keep := 1; keep <= len(repaired); keep++ {
		out, err := SlidingWindow{Keep: keep}.Compact(context.Background(), repaired)
		if err != nil {
			t.Fatal(err)
		}
		if _, again := api.RepairHistory(out); again {
			t.Fatalf("keep %d: compaction broke a pair that the repair had fixed: %+v", keep, out)
		}
		if !consistentPairs(out) {
			t.Fatalf("keep %d: a tool_result lost its tool_use or a tool_use lost its result: %+v", keep, out)
		}
	}
}
