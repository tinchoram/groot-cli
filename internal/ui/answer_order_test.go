package ui

import (
	"context"
	"strings"
	"testing"
)

// TestAnswerArrivesAfterItsDeltasAndLateDeltasAreIgnored replays the live bug:
// the answer used to overtake the last stream deltas, which then refilled the
// partial text and produced a second, raw copy with literal ** marks.
func TestAnswerArrivesAfterItsDeltasAndLateDeltasAreIgnored(t *testing.T) {
	chat := &fakeSession{model: "m", reply: "Hello **x**"}
	ws := newFakeWorkspace(chat)
	bridge := NewBridge()
	m := NewModel(Options{Workspace: ws, Bridge: bridge, Version: "test"})
	m.width, m.height = 120, 40
	m.resize()
	m.busy = true

	bridge.Text(m.key, "Hel")
	cmd := m.sendCmd(context.Background(), m.key, "hi")
	if msg := cmd(); msg != nil {
		t.Fatalf("with a bridge the answer travels in the event stream, got %T", msg)
	}
	bridge.Text(m.key, "lo") // a delta that lands after the answer

	for i := 0; i < 3; i++ {
		next, _ := m.Update(<-bridge.Events())
		m = next.(Model)
	}

	out := stripANSI(m.transcriptText())
	if n := strings.Count(out, "Hello"); n != 1 {
		t.Fatalf("the answer must appear exactly once, found %d:\n%s", n, out)
	}
	if strings.Contains(out, "**") {
		t.Fatalf("no raw markdown marks may show:\n%s", out)
	}
	if m.partial != "" || m.busy {
		t.Fatalf("the turn is over: partial=%q busy=%v", m.partial, m.busy)
	}
}
