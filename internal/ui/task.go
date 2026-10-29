package ui

import (
	"context"
	"fmt"
	"strconv"
	"strings"
)

// taskLabel is the header part that names the open task, or "" without one.
func (m Model) taskLabel() string {
	if m.session == nil {
		return ""
	}
	t, ok := m.session.Task()
	if !ok {
		return ""
	}
	label := styleAccentBold.Render(fmt.Sprintf("task #%d %s/%s", t.ID, t.Size, t.Phase))
	if seen, ok := m.session.Observed(); ok && seen != t.Phase {
		label += styleDim.Render(" · seen: " + string(seen))
	}
	return styleDim.Render(" · ") + label
}

// taskAssignUsage is the reply to /task assign that does not parse.
const taskAssignUsage = "usage: /task assign #<n> <agent>"

// taskAssign hands the task #n to an agent of the space. The workspace checks
// the rules and reports a refusal, which the transcript shows.
func (m *Model) taskAssign(args []string) {
	if len(args) != 2 {
		m.append(styleError.Render(taskAssignUsage))
		return
	}
	id, err := strconv.ParseInt(strings.TrimPrefix(args[0], "#"), 10, 64)
	if err != nil || id <= 0 {
		m.append(styleError.Render(taskAssignUsage))
		return
	}
	out, err := m.session.AssignTask(context.Background(), id, args[1], "")
	if err != nil {
		m.append(styleError.Render("error: " + err.Error()))
		return
	}
	m.append(styleDim.Render(out))
}

// runTaskCommand handles /task, which prints the classification and the open
// task, and /task fail <reason>, which closes the open task as failed.
func (m *Model) runTaskCommand(arg string) {
	ctx := context.Background()
	if strings.TrimSpace(arg) == "accept" {
		m.taskAccept()
		return
	}
	if rest, ok := strings.CutPrefix(arg, "reject"); ok && (rest == "" || rest[0] == ' ') {
		reason := strings.TrimSpace(rest)
		if reason == "" {
			m.append(styleError.Render("usage: /task reject <reason>"))
			return
		}
		m.taskReject(reason)
		return
	}
	if rest, ok := strings.CutPrefix(arg, "fail"); ok && (rest == "" || rest[0] == ' ') {
		reason := strings.TrimSpace(rest)
		if reason == "" {
			m.append(styleError.Render("usage: /task fail <reason>"))
			return
		}
		out, err := m.session.FailTask(ctx, reason)
		if err != nil {
			m.append(styleError.Render("error: " + err.Error()))
			return
		}
		m.append(styleDim.Render(out))
		return
	}
	if rest, ok := strings.CutPrefix(arg, "assign"); ok && (rest == "" || rest[0] == ' ') {
		m.taskAssign(strings.Fields(rest))
		return
	}
	for _, line := range strings.Split(m.session.TaskStatus(ctx), "\n") {
		m.append(styleDim.Render(line))
	}
	if seen, ok := m.session.Observed(); ok {
		if t, has := m.session.Task(); has && seen != t.Phase {
			m.append(styleDim.Render("observed: " + string(seen)))
		}
	}
}
