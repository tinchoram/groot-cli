package ui

import (
	"strings"
	"testing"

	"github.com/tinchoram/groot-cli/internal/feedback"
)

func TestBugCommandFilesTheItemWithContextAndConfirms(t *testing.T) {
	chat := &fakeSession{model: "m"}
	m, ws := newTestModel(t, chat)
	m.append("❯ earlier question")
	m = sendLine(t, m, "/bug the approval hides the path")
	out := stripANSI(m.transcriptText())
	if !strings.Contains(out, "sent to debug as message #7") {
		t.Fatalf("confirmation missing:\n%s", out)
	}
	found := false
	for _, l := range ws.lastContext {
		if strings.Contains(l, "earlier question") {
			found = true
		}
	}
	if !found {
		t.Fatalf("the report keeps the recent transcript: %v", ws.lastContext)
	}
}

func TestFeatCommandNeedsText(t *testing.T) {
	m, ws := newTestModel(t, &fakeSession{model: "m"})
	m = sendLine(t, m, "/feat")
	if !strings.Contains(stripANSI(m.transcriptText()), "usage: /feat <text>") {
		t.Fatal("a feature request without text is refused")
	}
	if ws.lastContext != nil {
		t.Fatal("nothing is filed without text")
	}
}

func TestBacklogCommandPrintsTheItems(t *testing.T) {
	m, ws := newTestModel(t, &fakeSession{model: "m"})
	ws.backlog = []feedback.Item{{ID: "FEAT-002", Status: "open", Title: "per-space routes"}}
	m = sendLine(t, m, "/backlog")
	if !strings.Contains(stripANSI(m.transcriptText()), "FEAT-002  open  per-space routes") {
		t.Fatalf("backlog output:\n%s", stripANSI(m.transcriptText()))
	}
}
