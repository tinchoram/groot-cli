package ui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"

	"github.com/tinchoram/groot-cli/internal/workspace"
)

func TestWorkModeTansTheInputAndNamesTheSpace(t *testing.T) {
	chat := &fakeSession{model: "m", workOn: true}
	m, _ := newTestModel(t, chat)
	if m.inputBorder() != lipgloss.Color(colorTan) {
		t.Fatal("in work mode the focused input border is tan")
	}
	out := stripANSI(m.View())
	if !strings.Contains(out, "WORK MODE · auto-approving inside /work/default · /work off to stop") {
		t.Fatalf("the hint should name the mode:\n%s", out)
	}

	chat.workOn = false
	if m.inputBorder() != lipgloss.Color(colorGreen) {
		t.Fatal("without work mode the focused input border stays green")
	}
}

func TestAutoApprovedCallsUseTheAutoLine(t *testing.T) {
	m, _ := newTestModel(t, &fakeSession{model: "m", workOn: true})
	m.busy = true
	m = update(t, m, autoMsg{name: "write_file", preview: "write_file {\"path\":\"a.md\"}\nmore"})
	m = update(t, m, toolCallMsg{name: "kubectl", preview: "kubectl get pods -n prod"})

	out := stripANSI(m.transcriptText())
	if !strings.Contains(out, "[auto] write_file write_file {\"path\":\"a.md\"}") {
		t.Fatalf("the approved call should be an [auto] line:\n%s", out)
	}
	if strings.Contains(out, "[auto] write_file more") {
		t.Fatal("only the first line of the preview is shown")
	}
	if !strings.Contains(out, "[tool] kubectl get pods -n prod") {
		t.Fatal("a call that still asks keeps the [tool] line")
	}
}

func TestWorkCommandPrintsTheLimitsAndTheOffLine(t *testing.T) {
	chat := &fakeSession{model: "m"}
	m, _ := newTestModel(t, chat)
	m = sendLine(t, m, "/work on")
	want := workspace.WorkOnLine("/work/default", 50, 5.0)
	if !strings.Contains(stripANSI(m.transcriptText()), want) {
		t.Fatalf("missing %q:\n%s", want, stripANSI(m.transcriptText()))
	}
	if !strings.Contains(want, "limits 50 turns / $5.00") {
		t.Fatalf("the limits are part of the line: %q", want)
	}
	m = sendLine(t, m, "/work off")
	if !strings.Contains(stripANSI(m.transcriptText()), workspace.WorkOffLine) {
		t.Fatal("/work off prints the off line")
	}
}

func TestLimitReachedLineHasNoWarningPrefix(t *testing.T) {
	m, _ := newTestModel(t, &fakeSession{model: "m"})
	m = update(t, m, warnMsg{text: "work mode off · limit reached (turns)"})
	out := stripANSI(m.transcriptText())
	if !strings.Contains(out, "work mode off · limit reached (turns)") {
		t.Fatalf("the limit line is missing:\n%s", out)
	}
	if strings.Contains(out, "warning: work mode") {
		t.Fatal("the limit line is not a warning prefix")
	}
	m = update(t, m, warnMsg{text: "a real warning"})
	if !strings.Contains(stripANSI(m.transcriptText()), "warning: a real warning") {
		t.Fatal("other warnings keep their prefix")
	}
}

func TestGearShowsForWorkModeAgentsEvenWhileBusy(t *testing.T) {
	m, ws := newTestModel(t, &fakeSession{model: "m"})
	if err := ws.AssignAgent("reviewer"); err != nil {
		t.Fatal(err)
	}
	ws.workModes = map[string]bool{"default/reviewer": true}
	m.runs["default/reviewer"] = &run{busy: true}
	m.refresh()
	found := false
	for _, line := range strings.Split(stripANSI(m.View()), "\n") {
		if strings.Contains(line, "reviewer") && strings.Contains(line, "⚙") {
			found = true
		}
	}
	if !found {
		t.Fatal("the gear shows while the agent works in work mode")
	}
}
