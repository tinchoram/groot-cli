package ui

import (
	"context"
	"fmt"
	"strings"

	"github.com/tinchoram/groot-cli/internal/workspace"
)

// tasksMark is the sign before a space's open task count in the sidebar.
const tasksMark = "⚑"

// tasksUsage is the reply to a /tasks command that does not parse.
const tasksUsage = "usage: /tasks | /tasks <space> | /tasks all | /tasks <space> all"

// refreshTasks re-reads the open task count of each space behind the sidebar
// badges. It runs with refreshUnread.
func (m *Model) refreshTasks() {
	rows, err := m.ws.OpenTasks(context.Background())
	m.openTasks = map[string]int{}
	if err != nil {
		return
	}
	for _, r := range rows {
		m.openTasks[r.Space]++
	}
}

// taskBadge is the open task count of a space in the sidebar, "⚑ 3", dim. It
// shows nothing when the space has no open task.
func (m Model) taskBadge(space string) string {
	n := m.openTasks[space]
	if n == 0 {
		return ""
	}
	return styleDim.Render(fmt.Sprintf("%s %d", tasksMark, n))
}

// runTasksCommand handles /tasks: the open tasks of every space, of the named
// space, and with "all" the done and failed tasks of the last seven days.
func (m *Model) runTasksCommand(arg string) {
	var space string
	includeClosed := false
	for _, field := range strings.Fields(arg) {
		switch {
		case field == "all":
			includeClosed = true
		case space == "":
			space = field
		default:
			m.append(styleError.Render(tasksUsage))
			return
		}
	}
	rows, err := m.ws.ListTasks(context.Background(), space, includeClosed)
	if err != nil {
		m.append(styleError.Render("error: " + err.Error()))
		return
	}
	m.append(workspace.FormatTasks(rows))
}
