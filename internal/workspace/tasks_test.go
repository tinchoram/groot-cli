package workspace

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/tinchoram/groot-cli/internal/flow"
)

// seedTask stores a task of agent in the space named name, in phase, last
// updated at at. A done or failed task is closed at the same time.
func seedTask(t *testing.T, f *fixture, name, path, agent string, phase flow.Phase, at time.Time, goal string) flow.Task {
	t.Helper()
	ctx := context.Background()
	f.ws.mu.Lock()
	st, err := f.ws.storesFor(path)
	f.ws.mu.Unlock()
	if err != nil {
		t.Fatal(err)
	}
	tasks := st.mem.Tasks()
	saved, err := tasks.Open(ctx, flow.Task{
		Space: name, Agent: agent, Size: flow.SizeMedium, Phase: flow.PhaseClassified, Goal: goal,
		OpenedAt: at, PhaseAt: at, UpdatedAt: at,
	})
	if err != nil {
		t.Fatal(err)
	}
	saved.Phase, saved.PhaseAt, saved.UpdatedAt = phase, at, at
	if phase == flow.PhaseDone || phase == flow.PhaseFailed {
		saved.ClosedAt = at
	}
	if err := tasks.Update(ctx, saved); err != nil {
		t.Fatal(err)
	}
	return saved
}

// twoSpaces adds a second space, other, with its own directory and the reader
// agent. The fixture's default space keeps its own.
func twoSpaces(t *testing.T, f *fixture) string {
	t.Helper()
	dir := t.TempDir()
	if err := f.spaces.Add("other", dir, t0); err != nil {
		t.Fatal(err)
	}
	if err := f.spaces.Assign("other", "reader"); err != nil {
		t.Fatal(err)
	}
	return dir
}

func TestOpenTasksAggregatesEverySpaceAndSkipsClosed(t *testing.T) {
	f := newFixture(t, text("ok"))
	other := twoSpaces(t, f)
	now := time.Now().UTC()
	seedTask(t, f, "default", f.dir, "reader", flow.PhaseImplement, now.Add(-time.Hour), "fix the parser")
	seedTask(t, f, "default", f.dir, "reader", flow.PhaseDone, now.Add(-2*time.Hour), "old work")
	seedTask(t, f, "other", other, "reader", flow.PhaseClassified, now.Add(-3*time.Hour), "first")
	seedTask(t, f, "other", other, "reader", flow.PhaseVerify, now.Add(-30*time.Minute), "second")

	rows, err := f.ws.OpenTasks(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 3 {
		t.Fatalf("rows = %+v, want the three open tasks of both spaces", rows)
	}
	want := []struct {
		space string
		phase flow.Phase
	}{{"default", flow.PhaseImplement}, {"other", flow.PhaseVerify}, {"other", flow.PhaseClassified}}
	for i, w := range want {
		if rows[i].Space != w.space || rows[i].Phase != w.phase {
			t.Fatalf("row %d = %s/%s, want %s/%s (space, then newest first)", i, rows[i].Space, rows[i].Phase, w.space, w.phase)
		}
	}
}

func TestListTasksIncludesClosedTasksOfTheLastSevenDays(t *testing.T) {
	f := newFixture(t, text("ok"))
	now := time.Now().UTC()
	seedTask(t, f, "default", f.dir, "reader", flow.PhaseDone, now.Add(-2*24*time.Hour), "shipped")
	seedTask(t, f, "default", f.dir, "reader", flow.PhaseFailed, now.Add(-10*24*time.Hour), "abandoned long ago")

	open, err := f.ws.ListTasks(context.Background(), "", false)
	if err != nil || len(open) != 0 {
		t.Fatalf("open only = %+v, %v; closed tasks must stay out", open, err)
	}
	all, err := f.ws.ListTasks(context.Background(), "", true)
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != 1 || all[0].Phase != flow.PhaseDone || all[0].Goal != "shipped" {
		t.Fatalf("with closed = %+v, want only the task closed two days ago with its final phase", all)
	}
}

func TestListTasksFiltersBySpaceAndRefusesUnknownSpaces(t *testing.T) {
	f := newFixture(t, text("ok"))
	other := twoSpaces(t, f)
	now := time.Now().UTC()
	seedTask(t, f, "default", f.dir, "reader", flow.PhaseImplement, now, "here")
	seedTask(t, f, "other", other, "reader", flow.PhaseImplement, now, "there")

	rows, err := f.ws.ListTasks(context.Background(), "other", false)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 || rows[0].Space != "other" || rows[0].Goal != "there" {
		t.Fatalf("filtered rows = %+v, want only the task of other", rows)
	}
	if _, err := f.ws.ListTasks(context.Background(), "nope", false); err == nil {
		t.Fatal("an unknown space must be refused")
	}
}

func TestTasksSkipASpaceWhoseDirectoryIsGone(t *testing.T) {
	f := newFixture(t, text("ok"))
	gone := t.TempDir()
	if err := f.spaces.Add("gone", gone, t0); err != nil {
		t.Fatal(err)
	}
	if err := os.RemoveAll(gone); err != nil {
		t.Fatal(err)
	}
	if _, err := f.ws.OpenTasks(context.Background()); err != nil {
		t.Fatalf("a missing directory must not fail the listing: %v", err)
	}
	if _, err := os.Stat(gone); !os.IsNotExist(err) {
		t.Fatalf("the directory must not be recreated: %v", err)
	}
}

func TestTaskRowIsActiveWhileItsConversationRunsATurn(t *testing.T) {
	f := newFixture(t, text("ok"))
	seedTask(t, f, "default", f.dir, "reader", flow.PhaseImplement, time.Now().UTC(), "busy work")
	conv, ok := f.ws.Chat().(*Conversation)
	if !ok {
		t.Fatal("Chat must return the open conversation of the active agent")
	}

	rows, err := f.ws.OpenTasks(context.Background())
	if err != nil || len(rows) != 1 || rows[0].Active {
		t.Fatalf("idle conversation: rows = %+v, %v; want one inactive row", rows, err)
	}
	conv.busy.Store(true)
	rows, _ = f.ws.OpenTasks(context.Background())
	if len(rows) != 1 || !rows[0].Active {
		t.Fatalf("running conversation: rows = %+v; want the row active", rows)
	}
	conv.busy.Store(false)
}

func TestTaskGoalIsTruncatedToSixtyRunes(t *testing.T) {
	f := newFixture(t, text("ok"))
	long := strings.Repeat("é", 100)
	seedTask(t, f, "default", f.dir, "reader", flow.PhaseImplement, time.Now().UTC(), long)

	rows, err := f.ws.OpenTasks(context.Background())
	if err != nil || len(rows) != 1 {
		t.Fatalf("rows = %+v, %v", rows, err)
	}
	if n := len([]rune(rows[0].Goal)); n != 60 || !strings.HasSuffix(rows[0].Goal, "…") {
		t.Fatalf("goal = %q (%d runes), want 60 runes ending in an ellipsis", rows[0].Goal, n)
	}
	if got := shortGoal("short goal"); got != "short goal" {
		t.Fatalf("a short goal must stay as it is: %q", got)
	}
}

func TestFormatTasksShowsEveryColumn(t *testing.T) {
	out := FormatTasks([]TaskRow{{
		Space: "default", Agent: "reader", ID: 7, Size: flow.SizeLarge, Phase: flow.PhaseVerify,
		Goal: "ship it", Evidence: 4, OpenedAt: t0, UpdatedAt: t0, Active: true,
		Delegate: "developer", Assigned: "developer (handed over)",
	}})
	for _, want := range []string{"SPACE", "AGENT", "TASK", "SIZE", "PHASE", "EVIDENCE", "UPDATED", "ACTIVE", "ASSIGNED", "GOAL",
		"default", "reader", "#7", "large", "verify", "4", "yes", "developer (handed over)", "ship it"} {
		if !strings.Contains(out, want) {
			t.Fatalf("table lacks %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "reader →") {
		t.Fatalf("the agent column must not repeat the handover:\n%s", out)
	}
	if empty := FormatTasks(nil); !strings.Contains(empty, "no tasks") {
		t.Fatalf("empty table = %q", empty)
	}
}

func TestAgentCellNamesOnlyTheOriginOfATakenOverTask(t *testing.T) {
	handed := TaskRow{Agent: "reader", Delegate: "developer", Assigned: "developer (handed over)"}
	if got := agentCell(handed); got != "reader" {
		t.Fatalf("agentCell = %q, the handover is in the ASSIGNED column", got)
	}
	taken := TaskRow{Agent: "developer", Parent: "default/reader #1"}
	if got := agentCell(taken); got != "developer ← default/reader #1" {
		t.Fatalf("agentCell = %q", got)
	}
}

func TestListTasksShowsTheAssignedHandoverState(t *testing.T) {
	ctx := context.Background()
	f := assignFixture(t, openMediumSteps("1", "split billing")...)
	if _, err := f.ws.Chat().Send(ctx, "build it"); err != nil {
		t.Fatal(err)
	}
	rows, err := f.ws.ListTasks(ctx, "default", false)
	if err != nil || len(rows) != 1 || rows[0].Assigned != "" {
		t.Fatalf("an open task nobody holds has no ASSIGNED state: %+v, %v", rows, err)
	}
	if _, err := f.ws.assignTask(ctx, "default", "space-manager", 1, "developer", ""); err != nil {
		t.Fatal(err)
	}
	rows, err = f.ws.ListTasks(ctx, "default", false)
	if err != nil || len(rows) != 1 || rows[0].Assigned != "developer (handed over)" {
		t.Fatalf("rows = %+v, %v; want the delegate and the handover state", rows, err)
	}
}
