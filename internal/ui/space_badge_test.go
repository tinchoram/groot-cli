package ui

import (
	"strings"
	"testing"
)

func TestSpaceRowShowsTotalUnreadOfItsAgents(t *testing.T) {
	m, _ := newTestModel(t, &fakeSession{model: "m"})
	m.unread = map[string]int{"default/reader": 2, "default/groot": 1, "other/x": 5}

	if got := stripANSI(m.spaceBadge("default")); !strings.Contains(got, "✉ 3") {
		t.Fatalf("default space badge = %q, want ✉ 3", got)
	}
	if got := stripANSI(m.spaceBadge("other")); !strings.Contains(got, "✉ 5") {
		t.Fatalf("other space badge = %q, want ✉ 5", got)
	}
	if got := stripANSI(m.spaceBadge("empty")); strings.Contains(got, "✉") {
		t.Fatalf("a space without unread messages shows no mail: %q", got)
	}
}

func TestSpaceBadgeOrderIsDotQuestionMail(t *testing.T) {
	m, _ := newTestModel(t, &fakeSession{model: "m"})
	m.unread = map[string]int{"default/reader": 1}
	m.pending = &confirmState{key: m.key, reply: make(chan bool, 1)}

	got := stripANSI(m.spaceBadge("default"))
	q, mail := strings.Index(got, "?"), strings.Index(got, "✉")
	if q < 0 || mail < 0 || q > mail {
		t.Fatalf("badge = %q, want the ? before the ✉", got)
	}
}
