package ui

import (
	"fmt"
	"strings"

	"github.com/tinchoram/groot-cli/internal/feedback"
)

// feedbackContextLines is how many recent transcript lines a report keeps.
const feedbackContextLines = 10

// reportFeedback handles /bug and /feat. It files the item without a model
// call, with the recent transcript as context, and confirms with its id.
func (m *Model) reportFeedback(kind, text string) {
	if strings.TrimSpace(text) == "" {
		m.append(styleError.Render("usage: /" + map[string]string{feedback.TypeBug: "bug", feedback.TypeFeat: "feat"}[kind] + " <text>"))
		return
	}
	space, _ := m.ws.ActiveSpace()
	id, _, err := m.ws.SendFeedback(space.Name, m.ws.ActiveAgent(), kind, text, m.recentContext())
	if err != nil {
		m.append(styleError.Render("error: " + err.Error()))
		return
	}
	m.append(styleDim.Render(fmt.Sprintf("sent to debug as message #%d", id)))
	m.refreshUnread()
}

// showBacklog prints the backlog filtered by status: open items by default,
// done items with "done", every item with "all".
func (m *Model) showBacklog(arg string) {
	var status string
	switch strings.TrimSpace(arg) {
	case "", "open":
		status = feedback.StatusOpen
	case "done":
		status = feedback.StatusDone
	case "all":
		status = ""
	default:
		m.append(styleError.Render("usage: /backlog [open|done|all]"))
		return
	}
	items, err := m.ws.Backlog(status)
	if err != nil {
		m.append(styleError.Render("error: " + err.Error()))
		return
	}
	if len(items) == 0 {
		m.append(styleDim.Render("backlog is empty"))
		return
	}
	for _, it := range items {
		m.append(styleDim.Render(fmt.Sprintf("%s  %s  %s", it.ID, it.Status, it.Title)))
	}
}

// recentContext is the last lines the user typed, without styling. Replies and
// the workspace blocks are not part of a report's context.

func (m *Model) recentContext() []string {
	var out []string
	for _, l := range m.lines {
		if strings.HasPrefix(l, mdMarker) {
			continue
		}
		s := strings.TrimSpace(stripCells(l))
		if !strings.HasPrefix(s, "❯ ") {
			continue
		}
		s = strings.TrimSpace(strings.TrimPrefix(s, "❯ "))
		if strings.HasPrefix(s, "/bug") || strings.HasPrefix(s, "/feat") {
			continue // an earlier report is not context for this one
		}
		if s != "" {
			out = append(out, s)
		}
	}
	if len(out) > feedbackContextLines {
		out = out[len(out)-feedbackContextLines:]
	}
	return out
}
