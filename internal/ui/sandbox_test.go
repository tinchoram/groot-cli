package ui

import (
	"strings"
	"testing"
)

func TestHeaderShowsSandboxOnlyWhenNoneIsActive(t *testing.T) {
	chat := &fakeSession{model: "m"}
	ws := newFakeWorkspace(chat)

	none := NewModel(Options{Workspace: ws, Bridge: NewBridge(), SandboxName: "none"})
	if out := stripANSI(none.header()); !strings.Contains(out, "sandbox: none") {
		t.Fatalf("header must show the missing sandbox:\n%s", out)
	}

	working := NewModel(Options{Workspace: ws, Bridge: NewBridge(), SandboxName: "sandbox-exec"})
	if out := stripANSI(working.header()); strings.Contains(out, "sandbox") {
		t.Fatalf("header must not mention a working sandbox:\n%s", out)
	}
}

func TestSandboxNoticeAppearsOnceInTheTranscript(t *testing.T) {
	chat := &fakeSession{model: "m"}
	ws := newFakeWorkspace(chat)
	ws.active, ws.agent = "root", "groot"
	const notice = "bash runs without a sandbox: install bubblewrap"
	m := NewModel(Options{Workspace: ws, Bridge: NewBridge(), SandboxName: "none", SandboxNotice: notice})
	if n := strings.Count(m.transcriptText(), notice); n != 1 {
		t.Fatalf("notice appears %d times, want once:\n%s", n, m.transcriptText())
	}
}
