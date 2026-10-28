package ui

import (
	"context"
	"strings"

	"github.com/tinchoram/groot-cli/internal/workspace"
)

// runWorkCommand handles /work on|off. With no argument it reports the state.
func (m *Model) runWorkCommand(arg string) {
	switch strings.TrimSpace(arg) {
	case "on":
		if err := m.setWork(true); err != nil {
			m.append(styleError.Render("error: " + err.Error()))
			return
		}
		m.append(styleDim.Render(m.workOnText()))
	case "off":
		if err := m.setWork(false); err != nil {
			m.append(styleError.Render("error: " + err.Error()))
			return
		}
		m.append(styleDim.Render(workspace.WorkOffLine))
	case "":
		state := "off"
		if m.session.WorkMode() {
			state = "on"
		}
		m.append(styleDim.Render("work mode " + state))
	default:
		m.append(styleError.Render("usage: /work on|off"))
	}
}

// setWork turns work mode on or off for every agent of the active space, the
// shown one included. Without an active space only the shown conversation changes.
func (m *Model) setWork(on bool) error {
	sp, ok := m.ws.ActiveSpace()
	if !ok {
		m.session.SetWorkMode(on)
		return nil
	}
	return m.ws.SetSpaceWorkMode(sp.Name, on)
}

// workLabel is the header part shown while work mode is on.
func (m Model) workLabel() string {
	if m.session == nil || !m.session.WorkMode() {
		return ""
	}
	return styleDim.Render(" · ") + styleTool.Render("work")
}

// workBadge is the gear after an agent in work mode.
func (m Model) workBadge(space, agent string) string {
	if !m.ws.WorkModes()[space+"/"+agent] {
		return ""
	}
	return " " + styleTool.Render("⚙")
}

// taskAccept and taskReject are the review answers of /task accept and /task reject.
func (m *Model) taskAccept() {
	out, err := m.session.TaskAccept(context.Background())
	if err != nil {
		m.append(styleError.Render("error: " + err.Error()))
		return
	}
	m.append(styleDim.Render(out))
}

func (m *Model) taskReject(reason string) {
	out, err := m.session.TaskReject(context.Background(), reason)
	if err != nil {
		m.append(styleError.Render("error: " + err.Error()))
		return
	}
	m.append(styleDim.Render(out))
}
