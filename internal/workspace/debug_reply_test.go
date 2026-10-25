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
	prompt := debugDef().Prompt
	if strings.Contains(prompt, `"<ID> recorded"`) {
		t.Fatal("the managed prompt must not ask for a confirmation on every report")
	}
	if !strings.Contains(prompt, "feedback_report") {
		t.Fatal("the managed prompt must still write the items")
	}
}

func TestDebugRulesCarryTheReleaseSweep(t *testing.T) {
	rules := debugRules(false)
	for _, want := range []string{"sweep the backlog", "close the release", "read_changelog", "shipped in", "feedback_update", "wontfix", "list those"} {
		if !strings.Contains(rules, want) {
			t.Fatalf("release sweep rule lacks %q:\n%s", want, rules)
		}
	}
	if !strings.Contains(rules, "Confirmations are off") {
		t.Fatal("the reply rule must still be part of the debug rules")
	}
}
