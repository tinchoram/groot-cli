package ui

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/tinchoram/groot-cli/internal/jobs"
	"github.com/tinchoram/groot-cli/internal/tool"
)

const (
	// jobsUsage is the reply to a /jobs command that does not parse.
	jobsUsage = "usage: /jobs | /jobs stop <id>"
	// jobMark precedes the count of job events waiting for the next turn.
	jobMark = "◆"
	// runningJobMark marks an agent with a background job that runs.
	runningJobMark = "▸"
)

// jobBadge marks an agent in the sidebar with its waiting job events and with a
// marker while a job runs. It is "" when neither applies.
func (m Model) jobBadge(space, agent string) string {
	jc := m.jobCounts[space+"/"+agent]
	var b string
	if jc.Waiting > 0 {
		b += " " + styleInboxBadge.Render(jobMark+" "+strconv.Itoa(jc.Waiting))
	}
	if jc.Running > 0 {
		b += " " + styleTool.Render(runningJobMark)
	}
	return b
}

// runningJobs counts the running jobs of the active conversation.
func (m Model) runningJobs() int {
	return m.jobCounts[m.ws.ConversationKey()].Running
}

// runJobsCommand handles /jobs, which lists the background jobs of the active
// conversation, and /jobs stop <id>, which stops one of them.
func (m *Model) runJobsCommand(arg string) {
	key := m.ws.ConversationKey()
	fields := strings.Fields(arg)
	switch {
	case len(fields) == 0:
		m.showJobs(key)
	case fields[0] == "stop" && len(fields) == 2:
		id, err := strconv.Atoi(fields[1])
		if err != nil || id <= 0 {
			m.append(styleError.Render(jobsUsage))
			return
		}
		job, err := m.ws.StopJob(key, id)
		if err != nil {
			m.append(styleError.Render("error: " + err.Error()))
			return
		}
		m.append(styleDim.Render(fmt.Sprintf("job #%d %s", job.ID, job.State)))
	default:
		m.append(styleError.Render(jobsUsage))
	}
}

// showJobs lists the jobs of a conversation: id, state, exit code, start and command.
func (m *Model) showJobs(key string) {
	list := m.ws.Jobs(key)
	if len(list) == 0 {
		m.append(styleDim.Render("no background jobs in this conversation"))
		return
	}
	for _, j := range list {
		exit := "-"
		if j.State != jobs.Running {
			exit = "exit " + strconv.Itoa(j.ExitCode)
		}
		cmd := strings.Join(strings.Fields(tool.Redact(j.Command)), " ")
		m.append(styleDim.Render(fmt.Sprintf("#%-3d %-8s %-8s %s  %s",
			j.ID, j.State, exit, j.Started.Format("15:04:05"), cmd)))
	}
}
