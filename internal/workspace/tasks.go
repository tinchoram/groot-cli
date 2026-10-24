package workspace

import (
	"context"
	"fmt"
	"os"
	"slices"
	"strings"
	"text/tabwriter"
	"time"
	"unicode/utf8"

	"github.com/tinchoram/groot-cli/internal/flow"
	"github.com/tinchoram/groot-cli/internal/space"
)

// closedTaskWindow is how far back /tasks all and the list_tasks tool look for
// done and failed tasks.
const closedTaskWindow = 7 * 24 * time.Hour

// taskGoalRunes is the longest goal a listing shows, ellipsis included.
const taskGoalRunes = 60

// TaskRow is one task of the listing across spaces.
type TaskRow struct {
	Space     string
	Agent     string
	ID        int64
	Size      flow.Size
	Phase     flow.Phase
	Goal      string // truncated to taskGoalRunes
	Evidence  int
	OpenedAt  time.Time
	UpdatedAt time.Time
	// Delegate is the agent the task was handed over to, and Parent the origin
	// "<space>/<agent> #<id>" of a task another agent opened to take over.
	Delegate string
	Parent   string
	// Active is true while the agent's open conversation runs a turn in this
	// process. A task whose conversation is idle, or not open, is not active.
	Active bool
}

// OpenTasks lists every open task of every space: the tasks whose phase is
// neither done nor failed. See ListTasks for the order.
func (w *Workspace) OpenTasks(ctx context.Context) ([]TaskRow, error) {
	return w.ListTasks(ctx, "", false)
}

// ListTasks lists the tasks of one space, or of every space when name is empty.
// With includeClosed it adds the done and failed tasks closed in the last
// seven days, with their final phase. Rows are sorted by space, then by the
// newest update first. A space whose directory is gone has no ledger and is
// skipped; its directory is not recreated.
func (w *Workspace) ListTasks(ctx context.Context, name string, includeClosed bool) ([]TaskRow, error) {
	w.mu.Lock()
	spaces := w.opts.Spaces.List()
	if name != "" {
		sp, ok := w.opts.Spaces.Get(name)
		if !ok {
			w.mu.Unlock()
			return nil, fmt.Errorf("unknown space %q", name)
		}
		spaces = []space.Space{sp}
	}
	w.mu.Unlock()

	since := time.Now().UTC().Add(-closedTaskWindow)
	var rows []TaskRow
	for _, sp := range spaces {
		if _, err := os.Stat(sp.Path); err != nil {
			continue
		}
		w.mu.Lock()
		st, err := w.storesFor(sp.Path)
		w.mu.Unlock()
		if err != nil {
			return nil, fmt.Errorf("space %s: %w", sp.Name, err)
		}
		list, err := st.mem.TaskSummaries(ctx, includeClosed, since)
		if err != nil {
			return nil, fmt.Errorf("space %s: %w", sp.Name, err)
		}
		for _, t := range list {
			rows = append(rows, TaskRow{
				Space: t.Space, Agent: t.Agent, ID: t.ID, Size: t.Size, Phase: t.Phase,
				Goal: shortGoal(t.Goal), Evidence: t.Evidence, OpenedAt: t.OpenedAt, UpdatedAt: t.UpdatedAt,
				Delegate: t.Delegation.Delegate, Parent: parentLabel(t.Parent),
				Active: w.runningTurn(sp.Name, t.Agent),
			})
		}
	}
	slices.SortStableFunc(rows, func(a, b TaskRow) int {
		if c := strings.Compare(a.Space, b.Space); c != 0 {
			return c
		}
		return b.UpdatedAt.Compare(a.UpdatedAt)
	})
	return rows, nil
}

// TaskReport is ListTasks as the table /tasks prints, for the admin tools.
func (w *Workspace) TaskReport(ctx context.Context, name string, includeClosed bool) (string, error) {
	rows, err := w.ListTasks(ctx, name, includeClosed)
	if err != nil {
		return "", err
	}
	return FormatTasks(rows), nil
}

// runningTurn reports whether the open conversation of agent in space is running
// a turn.
func (w *Workspace) runningTurn(spaceName, agent string) bool {
	c, ok := w.liveConversation(spaceName, agent)
	return ok && c.busy.Load()
}

// parentLabel names the origin of a task as "<space>/<agent> #<id>".
func parentLabel(p flow.Parent) string {
	if !p.Set() {
		return ""
	}
	return fmt.Sprintf("%s/%s #%d", p.Space, p.Agent, p.Task)
}

// agentCell is the agent column of the listing: "reader → developer" for a task
// handed over, "developer ← space/reader #1" for a task taken over.
func agentCell(r TaskRow) string {
	switch {
	case r.Delegate != "":
		return r.Agent + " → " + r.Delegate
	case r.Parent != "":
		return r.Agent + " ← " + r.Parent
	}
	return r.Agent
}

// shortGoal cuts a goal to taskGoalRunes runes, ending it with an ellipsis.
func shortGoal(goal string) string {
	if utf8.RuneCountInString(goal) <= taskGoalRunes {
		return goal
	}
	return string([]rune(goal)[:taskGoalRunes-1]) + "…"
}

// FormatTasks lays out task rows as an aligned table.
func FormatTasks(rows []TaskRow) string {
	if len(rows) == 0 {
		return "no tasks"
	}
	var sb strings.Builder
	tw := tabwriter.NewWriter(&sb, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "SPACE\tAGENT\tTASK\tSIZE\tPHASE\tEVIDENCE\tOPENED\tUPDATED\tACTIVE\tGOAL")
	for _, r := range rows {
		active := "no"
		if r.Active {
			active = "yes"
		}
		fmt.Fprintf(tw, "%s\t%s\t#%d\t%s\t%s\t%d\t%s\t%s\t%s\t%s\n", r.Space, agentCell(r), r.ID, r.Size, r.Phase,
			r.Evidence, r.OpenedAt.Local().Format("2006-01-02 15:04"), r.UpdatedAt.Local().Format("2006-01-02 15:04"),
			active, r.Goal)
	}
	_ = tw.Flush()
	return strings.TrimRight(sb.String(), "\n")
}
