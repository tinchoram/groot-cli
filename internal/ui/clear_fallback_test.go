package ui

import (
	"fmt"
	"strings"
	"testing"

	"github.com/tinchoram/groot-cli/internal/workspace"
)

// A summary fallback after /clear is a note, not an error line.
func TestClearFallbackIsShownAsANoteNotAnError(t *testing.T) {
	chat := &fakeSession{model: "m", resumed: storedSession()}
	chat.clearErr = fmt.Errorf("%w: summary for groot: the model did not answer in time; a local summary was kept", workspace.ErrSummaryFallback)
	m, _ := newTestModel(t, chat)
	m.input.SetValue("/clear")
	_, cmd := m.submit()
	if cmd == nil {
		t.Fatal("/clear must return the command that finishes the old session")
	}
	note, ok := cmd().(noteMsg)
	if !ok || note.err != nil {
		t.Fatalf("fallback must not be an error: %#v", cmd())
	}
	if !strings.Contains(stripANSI(note.text), "local summary was kept") {
		t.Fatalf("note = %q", note.text)
	}
}
