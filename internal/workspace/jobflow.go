package workspace

import (
	"fmt"
	"strings"
	"time"

	"github.com/tinchoram/groot-cli/internal/audit"
	"github.com/tinchoram/groot-cli/internal/gate"
	"github.com/tinchoram/groot-cli/internal/jobs"
	"github.com/tinchoram/groot-cli/internal/tool"
)

const (
	// jobQueue is how many events a conversation can hold before the manager
	// drops line events.
	jobQueue = 256
	// jobEventMax is the length of one job event line as the model reads it.
	jobEventMax = 500
	// jobBlockHeader opens the block of job events in a user turn.
	jobBlockHeader = "Background job events"
)

// jobEventLine renders one event as a line of the turn. The text is redacted
// before it is cut, so a cut never leaves a secret half-hidden.
func jobEventLine(ev jobs.Event) string {
	var line string
	switch ev.Kind {
	case jobs.EventExit:
		line = fmt.Sprintf("job #%d exited with code %d after %s: %s",
			ev.JobID, ev.ExitCode, ev.Duration.Round(100*time.Millisecond), ev.Command)
	case jobs.EventCapped:
		line = fmt.Sprintf("job #%d reached the %d-line event cap", ev.JobID, jobs.MonitorCap)
	default:
		line = fmt.Sprintf("job #%d: %s", ev.JobID, ev.Line)
	}
	return cutJobLine(tool.Redact(line))
}

// cutJobLine keeps the first jobEventMax characters and marks the cut.
func cutJobLine(s string) string {
	r := []rune(s)
	if len(r) <= jobEventMax {
		return s
	}
	return string(r[:jobEventMax-1]) + "…"
}

// drainJobs takes the job events of one conversation until its jobs stop. Each
// event waits in the conversation for the next turn; in work mode it may start one.
func (w *Workspace) drainJobs(key string, c *Conversation) {
	for {
		select {
		case ev := <-c.jobEvents:
			c.queueJobEvent(ev)
			w.jobEventArrived(key, c, ev)
		case <-c.jobStop:
			return
		}
	}
}

func (c *Conversation) queueJobEvent(ev jobs.Event) {
	line := jobEventLine(ev)
	c.jobMu.Lock()
	defer c.jobMu.Unlock()
	c.jobPending = append(c.jobPending, line)
}

// jobEventArrived tells the UI that events wait. In work mode an idle
// conversation starts a turn for them, through the same path as a message.
// Line events never warn at the turn limit: only the exit and the cap do.
func (w *Workspace) jobEventArrived(key string, c *Conversation, ev jobs.Event) {
	w.notifyJobs(key)
	if c.jobsClosing.Load() || !c.WorkMode() {
		return
	}
	notice := fmt.Sprintf("⟶ triggered by job #%d output", ev.JobID)
	if ev.Kind != jobs.EventLine {
		notice = fmt.Sprintf("⟶ triggered by job #%d", ev.JobID)
	}
	capped := fmt.Sprintf("work mode: the automatic turn limit (%d per session) was reached; job events wait for the next turn", w.opts.Config.Work.MaxAutoTurns)
	warn := capSilent
	if ev.Kind != jobs.EventLine {
		warn = capEach
	}
	w.startAutoTurn(key, c, notice, capped, warn)
}

// jobBlock takes the waiting job events out of the queue and renders them as
// one block of the turn: a header, then one line per event. It is empty when
// nothing waits.
func (c *Conversation) jobBlock() string {
	c.jobMu.Lock()
	lines := c.jobPending
	c.jobPending = nil
	c.jobMu.Unlock()
	if len(lines) == 0 {
		return ""
	}
	return jobBlockHeader + "\n" + strings.Join(lines, "\n")
}

// stopJobs stops the jobs of the conversation for good: they are stopped and
// their exits are recorded before it returns, and the drain ends. Later starts
// are refused. It is safe to call more than once.
func (c *Conversation) stopJobs() {
	if c.jobs == nil {
		return
	}
	c.jobStopOnce.Do(func() {
		c.jobsClosing.Store(true)
		c.jobs.StopAll()
		if c.jobStop != nil {
			close(c.jobStop)
		}
	})
}

// runningJobIDs lists the running jobs of the conversation.
func (c *Conversation) runningJobIDs() []int {
	if c.jobs == nil {
		return nil
	}
	var ids []int
	for _, j := range c.jobs.List() {
		if j.State == jobs.Running {
			ids = append(ids, j.ID)
		}
	}
	return ids
}

// stopJobIDs stops the given jobs. The manager stays open, so later jobs run.
func (c *Conversation) stopJobIDs(ids []int, by string) {
	for _, id := range ids {
		_, _ = c.jobs.StopBy(id, by)
	}
}

// convFor returns the open conversation with the key, or nil.
func (w *Workspace) convFor(key string) *Conversation {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.convs[key]
}

// WaitingJobEvents counts the job events that wait for the next turn of the conversation.
func (w *Workspace) WaitingJobEvents(key string) int {
	c := w.convFor(key)
	if c == nil {
		return 0
	}
	c.jobMu.Lock()
	defer c.jobMu.Unlock()
	return len(c.jobPending)
}

// Jobs lists the background jobs of the conversation, by id.
func (w *Workspace) Jobs(key string) []jobs.Job {
	c := w.convFor(key)
	if c == nil || c.jobs == nil {
		return nil
	}
	return c.jobs.List()
}

// StopJob stops one job of the conversation at the operator's request. The
// stop is recorded in the audit log as made by the user.
func (w *Workspace) StopJob(key string, id int) (jobs.Job, error) {
	c := w.convFor(key)
	if c == nil || c.jobs == nil {
		return jobs.Job{}, fmt.Errorf("no open conversation %s", key)
	}
	return c.jobs.StopBy(id, jobs.StopByUser)
}

// JobCount is what the UI shows of the background jobs of one conversation.
type JobCount struct {
	Waiting int // job events waiting for the next turn
	Running int // jobs that run now
}

// JobCounts returns the job counts of every open conversation, by key.
func (w *Workspace) JobCounts() map[string]JobCount {
	w.mu.Lock()
	convs := make(map[string]*Conversation, len(w.convs))
	for key, c := range w.convs {
		convs[key] = c
	}
	w.mu.Unlock()
	out := make(map[string]JobCount, len(convs))
	for key, c := range convs {
		var jc JobCount
		c.jobMu.Lock()
		jc.Waiting = len(c.jobPending)
		c.jobMu.Unlock()
		jc.Running = len(c.runningJobIDs())
		out[key] = jc
	}
	return out
}

// notifyJobs tells the UI that the jobs of a conversation changed.
func (w *Workspace) notifyJobs(key string) {
	if h := w.opts.Hooks.OnJobs; h != nil {
		h(key)
	}
}

// jobWatch is the observer of the jobs of one conversation. It writes the job
// records of the space audit log and tells the UI that the jobs changed.
type jobWatch struct {
	w    *Workspace
	key  string
	conv *Conversation
	log  *audit.Log
}

func (o *jobWatch) Started(j jobs.Job) {
	o.record(audit.Record{
		Tool:         "job_start",
		InputPreview: fmt.Sprintf("#%d %s: %s", j.ID, j.Tool, j.Command),
		Sandbox:      j.Sandbox,
	})
	o.w.notifyJobs(o.key)
}

func (o *jobWatch) Stopped(j jobs.Job, by string) {
	o.record(audit.Record{
		Tool:         "job_stop",
		InputPreview: fmt.Sprintf("#%d by %s: %s", j.ID, by, j.Command),
		Sandbox:      j.Sandbox,
	})
}

func (o *jobWatch) Exited(j jobs.Job) {
	o.record(audit.Record{
		Tool:         "job_exit",
		InputPreview: fmt.Sprintf("#%d %s exit %d: %s", j.ID, j.State, j.ExitCode, j.Command),
		IsError:      j.State != jobs.Exited || j.ExitCode != 0,
		DurationMS:   j.Ended.Sub(j.Started).Milliseconds(),
		Sandbox:      j.Sandbox,
	})
	o.w.notifyJobs(o.key)
}

// record fills the caller and the session of a job record, then writes it.
func (o *jobWatch) record(r audit.Record) {
	r.Time = time.Now().UTC()
	r.Space = o.conv.space
	r.Agent = o.conv.name
	r.Session = o.conv.SessionID()
	r.Risk = tool.RiskExec.String()
	r.Decision = gate.Allow.String()
	if err := o.log.Write(r); err != nil {
		o.conv.warn(fmt.Sprintf("audit: job record not written: %v", err))
	}
}
