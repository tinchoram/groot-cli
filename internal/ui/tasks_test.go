package ui

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/tinchoram/groot-cli/internal/flow"
	"github.com/tinchoram/groot-cli/internal/workspace"
)

// OpenTasks answers the fake's open tasks.
func (f *fakeWorkspace) OpenTasks(context.Context) ([]workspace.TaskRow, error) {
	return f.tasks, nil
}

// ListTasks filters the fake's tasks by space and records the call.
func (f *fakeWorkspace) ListTasks(_ context.Context, name string, includeClosed bool) ([]workspace.TaskRow, error) {
	f.taskQueries = append(f.taskQueries, fmt.Sprintf("%s|%v", name, includeClosed))
	if name != "" && name != "other" && name != "default" {
		return nil, fmt.Errorf("unknown space %q", name)
	}
	var out []workspace.TaskRow
	for _, t := range f.tasks {
		if name == "" || t.Space == name {
			out = append(out, t)
		}
	}
	return out, nil
}

func TestSidebarShowsTheOpenTaskCountOfEachSpace(t *testing.T) {
	m, _ := newTestModel(t, &fakeSession{model: "m"})
	m.openTasks = map[string]int{"default": 2}
	if got := stripANSI(m.taskBadge("default")); got != "⚑ 2" {
		t.Fatalf("task badge = %q, want ⚑ 2", got)
	}
	if got := m.taskBadge("empty"); got != "" {
		t.Fatalf("a space without open tasks shows no badge: %q", got)
	}
	if got := m.taskBadge("default"); got != styleDim.Render("⚑ 2") {
		t.Fatalf("task badge must be dim: %q", got)
	}
}

func TestSidebarComposesTheTaskBadgeWithTheSpendBadge(t *testing.T) {
	m, ws := newTestModel(t, &fakeSession{model: "m"})
	active, _ := ws.ActiveSpace()
	m.openTasks = map[string]int{active.Name: 3}
	m.spend = workspace.SpendView{Spaces: map[string]workspace.Money{active.Name: {Spent: 0.9, Budget: 2}}}
	out := stripANSI(strings.Join(m.sidebarLines(40, false), "\n"))
	if !strings.Contains(out, active.Name+" $0.90/$2.00 · ⚑ 3") {
		t.Fatalf("sidebar lacks the joined spend and task badges:\n%s", out)
	}
}

func TestRefreshUnreadReadsTheOpenTasks(t *testing.T) {
	m, ws := newTestModel(t, &fakeSession{model: "m"})
	ws.tasks = []workspace.TaskRow{
		{Space: "default", ID: 1, Phase: flow.PhaseImplement},
		{Space: "default", ID: 2, Phase: flow.PhaseVerify},
		{Space: "other", ID: 3, Phase: flow.PhaseClassified},
	}
	m.refreshUnread()
	if m.openTasks["default"] != 2 || m.openTasks["other"] != 1 {
		t.Fatalf("open task counts = %v, want default 2 and other 1", m.openTasks)
	}
}

func TestTasksCommandPrintsTheOpenTasksOfEverySpace(t *testing.T) {
	m, ws := newTestModel(t, &fakeSession{model: "m"})
	ws.tasks = []workspace.TaskRow{{Space: "default", Agent: "reader", ID: 4, Size: flow.SizeMedium, Phase: flow.PhaseImplement, Goal: "fix the parser"}}
	m = sendLine(t, m, "/tasks")
	out := stripANSI(m.transcriptText())
	for _, want := range []string{"SPACE", "#4", "implement", "fix the parser"} {
		if !strings.Contains(out, want) {
			t.Fatalf("/tasks lacks %q:\n%s", want, out)
		}
	}
	if got := ws.taskQueries[len(ws.taskQueries)-1]; got != "|false" {
		t.Fatalf("/tasks queried %q, want every space without closed tasks", got)
	}
}

func TestTasksCommandFiltersBySpaceAndAddsClosedWithAll(t *testing.T) {
	m, ws := newTestModel(t, &fakeSession{model: "m"})
	m = sendLine(t, m, "/tasks other")
	m = sendLine(t, m, "/tasks all")
	m = sendLine(t, m, "/tasks other all")
	want := []string{"other|false", "|true", "other|true"}
	if strings.Join(ws.taskQueries, ",") != strings.Join(want, ",") {
		t.Fatalf("queries = %v, want %v", ws.taskQueries, want)
	}
}

func TestTasksCommandRejectsBadArgumentsAndUnknownSpaces(t *testing.T) {
	m, _ := newTestModel(t, &fakeSession{model: "m"})
	m = sendLine(t, m, "/tasks a b")
	if out := stripANSI(m.transcriptText()); !strings.Contains(out, "usage: /tasks") {
		t.Fatalf("two spaces must show the usage:\n%s", out)
	}
	m = sendLine(t, m, "/tasks ghost")
	if out := stripANSI(m.transcriptText()); !strings.Contains(out, `unknown space "ghost"`) {
		t.Fatalf("an unknown space must show the error:\n%s", out)
	}
}

func TestHelpListsTheTasksCommand(t *testing.T) {
	m, _ := newTestModel(t, &fakeSession{model: "m"})
	m = sendLine(t, m, "/help")
	if out := m.transcriptText(); !strings.Contains(out, "/tasks") {
		t.Fatalf("help lacks /tasks:\n%s", out)
	}
}
