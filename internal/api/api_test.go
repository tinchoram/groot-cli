package api

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestBlockJSONRoundTrip(t *testing.T) {
	msg := Message{
		Role: RoleAssistant,
		Content: []Block{
			{Type: BlockText, Text: "checking pods"},
			{Type: BlockToolUse, ID: "tu_1", Name: "kubectl", Input: json.RawMessage(`{"args":"get pods"}`)},
		},
	}
	raw, err := json.Marshal(msg)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var back Message
	if err := json.Unmarshal(raw, &back); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if back.Role != RoleAssistant || len(back.Content) != 2 {
		t.Fatalf("unexpected round trip: %+v", back)
	}
	if back.Content[1].Name != "kubectl" || string(back.Content[1].Input) != `{"args":"get pods"}` {
		t.Fatalf("tool_use lost fields: %+v", back.Content[1])
	}
}

func TestToolResultFieldsSerialize(t *testing.T) {
	b := Block{Type: BlockToolResult, ToolUseID: "tu_1", Content: "denied by user", IsError: true}
	raw, _ := json.Marshal(b)
	s := string(raw)
	for _, want := range []string{`"tool_use_id":"tu_1"`, `"is_error":true`, `"content":"denied by user"`} {
		if !strings.Contains(s, want) {
			t.Errorf("serialized %s missing %s", s, want)
		}
	}
}

func TestUsageAdd(t *testing.T) {
	a := Usage{InputTokens: 10, OutputTokens: 5, CacheCreationTokens: 2, CacheReadTokens: 3}
	b := Usage{InputTokens: 1, OutputTokens: 1, CacheReadTokens: 7}
	a.Add(b)
	if a.InputTokens != 11 || a.OutputTokens != 6 || a.CacheCreationTokens != 2 || a.CacheReadTokens != 10 {
		t.Fatalf("Add produced %+v", a)
	}
}

func TestRenderTranscript(t *testing.T) {
	msgs := []Message{
		{Role: RoleUser, Content: []Block{{Type: BlockText, Text: "list pods"}}},
		{Role: RoleAssistant, Content: []Block{{Type: BlockToolUse, Name: "kubectl", Input: json.RawMessage(`{"args":"get pods"}`)}}},
		{Role: RoleUser, Content: []Block{{Type: BlockToolResult, ToolUseID: "x", Content: "pod-a Running", IsError: false}}},
	}
	out := RenderTranscript(msgs)
	for _, want := range []string{"[user]", "list pods", "[assistant]", "tool_use kubectl", "[user]", "tool_result", "pod-a Running"} {
		if !strings.Contains(out, want) {
			t.Errorf("transcript missing %q:\n%s", want, out)
		}
	}
}

func TestStopReasonConstants(t *testing.T) {
	if StopEndTurn != "end_turn" || StopToolUse != "tool_use" || StopMaxTokens != "max_tokens" {
		t.Fatal("stop reason constants drifted from the wire values")
	}
}
