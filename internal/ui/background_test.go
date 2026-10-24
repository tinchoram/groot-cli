package ui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/tinchoram/groot-cli/internal/gate"
	"github.com/tinchoram/groot-cli/internal/tool"
)

// bgModel is a model on devjunior with reviewer assigned as a second conversation.
func bgModel(t *testing.T) (Model, *fakeWorkspace) {
	t.Helper()
	m, ws := newTestModel(t, &fakeSession{model: "m"})
	if err := ws.AssignAgent("reviewer"); err != nil {
		t.Fatal(err)
	}
	m.refresh()
	return m, ws
}

func update(t *testing.T, m Model, msg tea.Msg) Model {
	t.Helper()
	next, _ := m.Update(msg)
	return next.(Model)
}

func TestBackgroundStreamLandsInItsOwnTranscript(t *testing.T) {
	m, _ := bgModel(t)
	m.runs["default/reviewer"] = &run{busy: true}
	m = update(t, m, streamMsg{key: "default/reviewer", delta: "review text"})
	if strings.Contains(stripANSI(m.View()), "review text") {
		t.Fatal("a background conversation must not draw on the screen in view")
	}

	m.selectAgent("reviewer")
	if m.partial != "review text" {
		t.Fatalf("switching back shows what arrived: partial = %q", m.partial)
	}

	m.selectAgent("devjunior")
	if strings.Contains(stripANSI(m.transcriptText()), "review text") {
		t.Fatal("the text must stay in the reviewer transcript")
	}
}

func TestBackgroundApprovalWaitsForTheUser(t *testing.T) {
	m, _ := bgModel(t)
	reply := make(chan bool, 1)
	m = update(t, m, confirmMsg{key: "default/reviewer", req: gate.Request{Tool: "bash", Risk: tool.RiskExec, Detail: "rm x"}, reply: reply})

	if m.pending != nil {
		t.Fatal("a background approval must not take over the screen")
	}
	if !strings.Contains(stripANSI(m.View()), "?") {
		t.Fatalf("waiting approval should show a ? mark:\n%s", stripANSI(m.View()))
	}

	m.selectAgent("reviewer")
	if m.pending == nil {
		t.Fatal("opening the conversation shows its approval")
	}
	m.answer(true)
	if !<-reply {
		t.Fatal("the approval is answered only by the user")
	}
}

func TestBackgroundTurnFinishesInItsTranscript(t *testing.T) {
	m, _ := bgModel(t)
	m = update(t, m, streamMsg{key: "default/reviewer", delta: "working"})
	m = update(t, m, answerMsg{key: "default/reviewer", text: "all done"})

	if busy, _ := m.keyState("default/reviewer"); busy {
		t.Fatal("the answer ends the background turn")
	}
	m.selectAgent("reviewer")
	if !strings.Contains(stripANSI(m.transcriptText()), "all done") {
		t.Fatalf("the answer belongs to reviewer:\n%s", m.transcriptText())
	}
}

func TestBusyBackgroundShowsOnSpaceAndAgentRows(t *testing.T) {
	m, _ := bgModel(t)
	m.runs["default/reviewer"] = &run{busy: true}
	out := stripANSI(m.View())
	var spaceRow, agentRow string
	for _, line := range strings.Split(out, "\n") {
		if strings.Contains(line, "default") && spaceRow == "" && !strings.Contains(line, "devjunior") {
			spaceRow = line
		}
		if strings.Contains(line, "reviewer") {
			agentRow = line
		}
	}
	if !strings.Contains(spaceRow, "●") {
		t.Fatalf("space row should show a working dot:\n%s", out)
	}
	if strings.Contains(agentRow, "?") {
		t.Fatalf("a working agent is not waiting: %q", agentRow)
	}
	if !strings.Contains(m.statusLine(), "1 agents working in background") {
		t.Fatalf("status line should count the background work: %q", m.statusLine())
	}
}

func TestSubmitRefusedWhileTheShownConversationWorks(t *testing.T) {
	m, _ := bgModel(t)
	m.busy = true
	m.input.SetValue("hello")
	next, _ := m.submit()
	if !strings.Contains(stripANSI(next.(Model).transcriptText()), "agent is working; wait or switch") {
		t.Fatal("the user should be told the agent is busy")
	}
}

func TestQuitDeniesPendingApprovalsAndCancelsTurns(t *testing.T) {
	m, _ := bgModel(t)
	reply := make(chan bool, 1)
	m = update(t, m, confirmMsg{key: "default/reviewer", req: gate.Request{Tool: "bash", Risk: tool.RiskExec}, reply: reply})
	cancelled := false
	m.cancels["default/reviewer"] = func() { cancelled = true }

	_, _ = m.quit()
	if <-reply {
		t.Fatal("quitting must deny the pending approval")
	}
	if !cancelled {
		t.Fatal("quitting must cancel the running turn")
	}
}
