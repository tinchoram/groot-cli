package workspace

import (
	"strings"
	"testing"
)

func TestDebugReplyRuleFollowsTheConfirmSetting(t *testing.T) {
	off := debugReplyRule(false)
	if !strings.Contains(off, "Confirmations are off") || !strings.Contains(off, "do not reply") {
		t.Fatalf("silent rule missing: %q", off)
	}
	on := debugReplyRule(true)
	if !strings.Contains(on, "Confirmations are on") || !strings.Contains(on, "send_message") {
		t.Fatalf("confirming rule missing: %q", on)
	}
}

func TestDebugPromptNoLongerConfirmsByDefault(t *testing.T) {
	if strings.Contains(debugPrompt, `"<ID> recorded"`) {
		t.Fatal("the managed prompt must not ask for a confirmation on every report")
	}
	if !strings.Contains(debugPrompt, "feedback_report") {
		t.Fatal("the managed prompt must still write the items")
	}
}
