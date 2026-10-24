package api

import "testing"

func userText(s string) Message {
	return Message{Role: RoleUser, Content: []Block{{Type: BlockText, Text: s}}}
}

func toolUses(ids ...string) Message {
	m := Message{Role: RoleAssistant}
	for _, id := range ids {
		m.Content = append(m.Content, Block{Type: BlockToolUse, ID: id, Name: "bash"})
	}
	return m
}

func toolResults(ids ...string) Message {
	m := Message{Role: RoleUser}
	for _, id := range ids {
		m.Content = append(m.Content, Block{Type: BlockToolResult, ToolUseID: id, Content: "ok"})
	}
	return m
}

func assertCancelled(t *testing.T, b Block, id string) {
	t.Helper()
	if b.Type != BlockToolResult || b.ToolUseID != id || !b.IsError || b.Content != "cancelled: no result recorded" {
		t.Fatalf("block for %s = %+v, want a cancelled error result", id, b)
	}
}

func TestRepairHistoryLeavesConsistentHistoryAlone(t *testing.T) {
	msgs := []Message{userText("hi"), toolUses("a"), toolResults("a"), {Role: RoleAssistant, Content: []Block{{Type: BlockText, Text: "done"}}}}
	out, changed := RepairHistory(msgs)
	if changed || len(out) != len(msgs) {
		t.Fatalf("consistent history must be unchanged: changed=%v len=%d", changed, len(out))
	}
}

func TestRepairHistoryCompletesMissingResultsInTheNextMessage(t *testing.T) {
	msgs := []Message{userText("hi"), toolUses("a", "b"), userText("next")}
	out, changed := RepairHistory(msgs)
	if !changed || len(out) != 3 {
		t.Fatalf("changed=%v len=%d", changed, len(out))
	}
	got := out[2].Content
	if len(got) != 3 {
		t.Fatalf("merged message has %d blocks: %+v", len(got), got)
	}
	assertCancelled(t, got[0], "a")
	assertCancelled(t, got[1], "b")
	if got[2].Type != BlockText || got[2].Text != "next" {
		t.Fatalf("the user's text must follow the results: %+v", got[2])
	}
	if len(msgs[1].Content) != 2 || len(msgs[2].Content) != 1 {
		t.Fatal("the input history must not be changed")
	}
}

func TestRepairHistoryKeepsExistingResultsFirst(t *testing.T) {
	msgs := []Message{userText("hi"), toolUses("a", "b"), toolResults("a")}
	out, changed := RepairHistory(msgs)
	if !changed || len(out) != 3 {
		t.Fatalf("changed=%v len=%d", changed, len(out))
	}
	got := out[2].Content
	if len(got) != 2 || got[0].ToolUseID != "a" || got[0].IsError || got[0].Content != "ok" {
		t.Fatalf("the real result of a must stay: %+v", got)
	}
	assertCancelled(t, got[1], "b")
}

func TestRepairHistoryAddsAMessageForTrailingToolUse(t *testing.T) {
	msgs := []Message{userText("hi"), toolUses("a")}
	out, changed := RepairHistory(msgs)
	if !changed || len(out) != 3 || out[2].Role != RoleUser {
		t.Fatalf("changed=%v out=%+v", changed, out)
	}
	assertCancelled(t, out[2].Content[0], "a")
}

func TestRepairHistoryInsertsResultsBeforeAnotherAssistantMessage(t *testing.T) {
	msgs := []Message{toolUses("a"), {Role: RoleAssistant, Content: []Block{{Type: BlockText, Text: "again"}}}}
	out, changed := RepairHistory(msgs)
	if !changed || len(out) != 3 || out[1].Role != RoleUser || out[2].Role != RoleAssistant {
		t.Fatalf("changed=%v out=%+v", changed, out)
	}
	assertCancelled(t, out[1].Content[0], "a")
}

func TestRepairReportsInsertedMessagesByIndex(t *testing.T) {
	msgs := []Message{userText("hi"), toolUses("a")}
	out, inserted, changed := Repair(msgs)
	if !changed || len(inserted) != 1 || inserted[0] != 2 || len(out) != 3 {
		t.Fatalf("inserted=%v changed=%v", inserted, changed)
	}
	// A merge completes an existing message: nothing is inserted.
	_, inserted, changed = Repair([]Message{toolUses("a", "b"), userText("next")})
	if !changed || len(inserted) != 0 {
		t.Fatalf("merge: inserted=%v changed=%v", inserted, changed)
	}
}
