package ui

import (
	"fmt"
	"strings"
	"testing"
)

func TestBugContextKeepsTheLastTenTypedLines(t *testing.T) {
	m, ws := newTestModel(t, &fakeSession{model: "m"})
	for i := 1; i <= 12; i++ {
		m.append(fmt.Sprintf("❯ line %d", i))
	}
	m.append("❯ /bug an earlier report body")
	m = sendLine(t, m, "/bug the new report")

	got := ws.lastContext
	if len(got) != 10 || got[0] != "line 3" || got[9] != "line 12" {
		t.Fatalf("context = %v, want the last 10 typed lines", got)
	}
	for _, l := range got {
		if strings.Contains(l, "/bug") || strings.Contains(l, "earlier report") {
			t.Fatalf("previous reports must not leak into the context: %v", got)
		}
	}
}
