package ui

import (
	"strings"
	"testing"
	"time"

	"github.com/tinchoram/groot-cli/internal/jobs"
	"github.com/tinchoram/groot-cli/internal/workspace"
)

func TestJobBadgeShowsTheWaitingEventCount(t *testing.T) {
	cases := []struct {
		waiting int
		want    string
	}{{0, ""}, {1, "◆ 1"}, {3, "◆ 3"}}
	for _, c := range cases {
		m := Model{jobCounts: map[string]workspace.JobCount{"default/reader": {Waiting: c.waiting}}}
		if got := strings.TrimSpace(stripANSI(m.jobBadge("default", "reader"))); got != c.want {
			t.Fatalf("waiting %d: badge = %q, want %q", c.waiting, got, c.want)
		}
	}
}

func TestJobBadgeMarksARunningJob(t *testing.T) {
	m := Model{jobCounts: map[string]workspace.JobCount{"default/reader": {Running: 2}}}
	got := stripANSI(m.jobBadge("default", "reader"))
	if !strings.Contains(got, runningJobMark) || strings.Contains(got, "◆") {
		t.Fatalf("running badge = %q", got)
	}
	if other := m.jobBadge("default", "writer"); other != "" {
		t.Fatalf("another agent shows %q", other)
	}
}

func TestStatusLineNamesTheRunningJobs(t *testing.T) {
	m, ws := newTestModel(t, &fakeSession{model: "m"})
	ws.jobCounts = map[string]workspace.JobCount{ws.ConversationKey(): {Running: 3}}
	m.refreshUnread()
	if out := stripANSI(m.statusLine()); !strings.Contains(out, "jobs: 3 running") {
		t.Fatalf("status line = %q, want the running jobs", out)
	}

	ws.jobCounts = map[string]workspace.JobCount{}
	m.refreshUnread()
	if out := stripANSI(m.statusLine()); strings.Contains(out, "jobs:") {
		t.Fatalf("status line = %q, want no job count without running jobs", out)
	}
}

func TestJobsCommandListsAndStopsJobsOfTheConversation(t *testing.T) {
	m, ws := newTestModel(t, &fakeSession{model: "m"})
	ws.bgJobs = []jobs.Job{
		{ID: 1, Command: "sleep 60 # password=hunter2", State: jobs.Running, ExitCode: -1, Started: time.Now().Add(-90 * time.Second)},
		{ID: 2, Command: "sh -c 'exit 3'", State: jobs.Exited, ExitCode: 3, Started: time.Now().Add(-time.Minute)},
	}
	m = sendLine(t, m, "/jobs")
	out := stripANSI(m.transcriptText())
	for _, want := range []string{"#1", "running", "sleep 60", "#2", "exited", "exit 3", "sh -c 'exit 3'"} {
		if !strings.Contains(out, want) {
			t.Fatalf("/jobs lacks %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "hunter2") {
		t.Fatalf("/jobs shows a secret:\n%s", out)
	}

	m = sendLine(t, m, "/jobs stop 1")
	if len(ws.stopped) != 1 || ws.stopped[0] != 1 {
		t.Fatalf("stopped = %v, want job 1", ws.stopped)
	}
	if out := stripANSI(m.transcriptText()); !strings.Contains(out, "job #1 stopped") {
		t.Fatalf("stop reply missing:\n%s", out)
	}

	m = sendLine(t, m, "/jobs stop x")
	if out := stripANSI(m.transcriptText()); !strings.Contains(out, jobsUsage) {
		t.Fatalf("bad id reply missing the usage:\n%s", out)
	}
}
