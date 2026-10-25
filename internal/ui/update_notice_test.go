package ui

import (
	"strings"
	"testing"
	"time"
)

func TestUpdateNoticeShowsInTheStatusLine(t *testing.T) {
	m, _ := newTestModel(t, costSession())

	next, cmd := m.Update(updateNoticeMsg{version: "0.6.0"})
	m = next.(Model)
	if cmd == nil {
		t.Fatal("the notice must expire on its own")
	}
	want := "v0.6.0 available · run: groot upgrade"
	if out := stripANSI(m.statusLine()); !strings.Contains(out, want) {
		t.Fatalf("status line lacks %q: %q", want, out)
	}
}

func TestUpdateNoticeExpires(t *testing.T) {
	m, _ := newTestModel(t, costSession())
	next, _ := m.Update(updateNoticeMsg{version: "0.6.0"})
	m = next.(Model)

	m.noteUntil = time.Now().Add(-time.Second)
	next, _ = m.Update(clearNoteMsg{})
	m = next.(Model)
	if out := stripANSI(m.statusLine()); strings.Contains(out, "available") {
		t.Fatalf("an expired notice is still shown: %q", out)
	}
}

func TestUpdateNoticeWaitsForOneValueOnTheChannel(t *testing.T) {
	m, _ := newTestModel(t, costSession())
	ch := make(chan string, 1)
	m.updates = ch

	cmd := m.updateNoticeCmd()
	if cmd == nil {
		t.Fatal("a notice channel must start a wait command")
	}
	ch <- "0.6.0"
	msg, ok := cmd().(updateNoticeMsg)
	if !ok || msg.version != "0.6.0" {
		t.Fatalf("message = %#v, want the version from the channel", msg)
	}
}

func TestUpdateNoticeChannelClosedWithoutValueSendsNothing(t *testing.T) {
	m, _ := newTestModel(t, costSession())
	ch := make(chan string)
	close(ch)
	m.updates = ch

	if msg := m.updateNoticeCmd()(); msg != nil {
		t.Fatalf("a closed channel must send no message, got %#v", msg)
	}
}

func TestNoUpdateChannelMeansNoWaitCommand(t *testing.T) {
	m, _ := newTestModel(t, costSession())
	if m.updateNoticeCmd() != nil {
		t.Fatal("without a channel there is nothing to wait for")
	}
}
