package ui

import (
	"strings"
	"testing"
)

func TestWorkCommandTogglesAndReportsWorkMode(t *testing.T) {
	chat := &fakeSession{model: "m"}
	m, _ := newTestModel(t, chat)
	m = sendLine(t, m, "/work on")
	if !chat.workOn {
		t.Fatal("/work on turns work mode on")
	}
	if out := stripANSI(m.View()); !strings.Contains(out, "· work") {
		t.Fatalf("the header should show work:\n%s", out)
	}
	m = sendLine(t, m, "/work off")
	if chat.workOn {
		t.Fatal("/work off turns work mode off")
	}
	m = sendLine(t, m, "/work")
	if !strings.Contains(stripANSI(m.transcriptText()), "work mode off") {
		t.Fatal("/work with no argument reports the state")
	}
}

func TestWorkCommandAppliesToTheWholeSpace(t *testing.T) {
	chat := &fakeSession{model: "m"}
	m, ws := newTestModel(t, chat)
	m = sendLine(t, m, "/work on")
	sendLine(t, m, "/work off")
	if len(ws.spaceWork) != 2 || ws.spaceWork[0] != "default:true" || ws.spaceWork[1] != "default:false" {
		t.Fatalf("space work calls = %v, want on then off for the active space", ws.spaceWork)
	}
}

func TestSidebarShowsTheGearForAgentsInWorkMode(t *testing.T) {
	m, ws := newTestModel(t, &fakeSession{model: "m"})
	if err := ws.AssignAgent("reviewer"); err != nil {
		t.Fatal(err)
	}
	ws.workModes = map[string]bool{"default/devjunior": true}
	m.refresh()
	var gear string
	for _, line := range strings.Split(stripANSI(m.View()), "\n") {
		if strings.Contains(line, "devjunior") && strings.Contains(line, "⚙") {
			gear = line
		}
		if strings.Contains(line, "reviewer") && strings.Contains(line, "⚙") {
			t.Fatal("an agent not in work mode has no gear")
		}
	}
	if gear == "" {
		t.Fatal("the agent in work mode should show a gear")
	}
}

func TestTaskAcceptAndRejectAnswerTheReview(t *testing.T) {
	chat := &fakeSession{model: "m"}
	m, _ := newTestModel(t, chat)
	m = sendLine(t, m, "/task accept")
	if len(chat.accepted) != 1 {
		t.Fatal("/task accept closes the delivered task")
	}
	m = sendLine(t, m, "/task reject the edge case is missing")
	if len(chat.rejected) != 1 || chat.rejected[0] != "the edge case is missing" {
		t.Fatalf("rejected = %v", chat.rejected)
	}
	m = sendLine(t, m, "/task reject")
	if !strings.Contains(stripANSI(m.transcriptText()), "usage: /task reject <reason>") {
		t.Fatal("a reject without a reason is refused")
	}
}

func TestReviewIsShownWhenItsConversationIsSelected(t *testing.T) {
	chat := &fakeSession{model: "m", review: "review · task #3 · medium\nevidence: 2 records"}
	m, _ := newTestModel(t, chat)
	m.syncConversation()
	if !strings.Contains(stripANSI(m.transcriptText()), "review · task #3") {
		t.Fatalf("the review should be shown:\n%s", m.transcriptText())
	}
	m.syncConversation()
	if strings.Count(stripANSI(m.transcriptText()), "review · task #3") != 1 {
		t.Fatal("the review is shown once")
	}
}
