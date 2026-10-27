package admintool

import (
	"context"
	"strings"

	"github.com/tinchoram/groot-cli/internal/tool"
)

// NewListTasks returns list_tasks: the open tasks of every space, or of one
// space, as the same table /tasks prints.
func NewListTasks(m Manager) tool.Tool {
	return listTasks{base{"list_tasks",
		"List the open ODD flow tasks (phase not done or failed) across spaces, with space, agent, task id, size, phase, evidence count, opened and updated time, whether its conversation is running a turn, and the goal. Set space to limit it to one space. Set include_closed to add the done and failed tasks of the last seven days with their final phase.",
		`{"type":"object","properties":{"space":{"type":"string","description":"space name; omit for every space"},"include_closed":{"type":"boolean","description":"also list done and failed tasks closed in the last seven days; default false"}},"additionalProperties":false}`,
		tool.RiskRead}, m}
}

type listTasks struct {
	base
	m Manager
}

type tasksInput struct {
	Space         string `json:"space"`
	IncludeClosed bool   `json:"include_closed"`
}

func (t listTasks) Preview(input string) string {
	var in tasksInput
	if err := decode(input, &in); err != nil || in.Space == "" {
		return "list the open tasks of every space"
	}
	return "list the open tasks of space " + in.Space
}

func (t listTasks) Execute(ctx context.Context, input string) (string, bool) {
	var in tasksInput
	if err := decode(input, &in); err != nil {
		return err.Error(), true
	}
	out, err := t.m.TaskReport(ctx, strings.TrimSpace(in.Space), in.IncludeClosed)
	if err != nil {
		return err.Error(), true
	}
	return out, false
}
