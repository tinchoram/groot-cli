// Package jobs runs background commands for one conversation and turns their
// output into events. The Manager owns the processes: start, stop, the output
// ring buffer, the line filter and the exit event. The caller builds each
// command (the tool layer wraps it in the sandbox) and reads the events.
package jobs

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os/exec"
	"regexp"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"
)

const (
	// DefaultLimit is the number of jobs one manager runs at once.
	DefaultLimit = 4
	// DefaultTimeout bounds a job whose caller names no timeout.
	DefaultTimeout = 30 * time.Minute
	// OutputLimit is the size of the output ring buffer, in bytes.
	OutputLimit = 64 << 10
	// MonitorCap is the most line events one job emits before its cap event.
	MonitorCap = 50

	killGrace    = 5 * time.Second // SIGTERM to SIGKILL
	queueSize    = 256             // events waiting for the dispatcher
	maxLineBytes = 1000            // longer lines are cut in their events
	maxPending   = 8 << 10         // a line this long without a newline is flushed
)

// deliverWait is how long the dispatcher waits for the caller to take one
// event before it drops it and counts the drop. A variable so tests can shorten it.
var deliverWait = 5 * time.Second

// State is the lifecycle of a job.
type State string

const (
	Running State = "running"
	Exited  State = "exited"  // the command ended on its own, with ExitCode
	Stopped State = "stopped" // ended by Stop, the timeout or StopAll
)

// EventKind tells what an Event carries.
type EventKind int

const (
	EventLine   EventKind = iota + 1 // one matching output line (Monitor only)
	EventExit                        // the job ended; always sent
	EventCapped                      // the line cap was reached; no more line events
)

// Event is one thing that happened to a job. Line is raw command output: the
// caller must redact it before it reaches a model.
type Event struct {
	JobID    int
	Kind     EventKind
	Line     string
	ExitCode int
	Duration time.Duration
	Command  string
}

// Job is a snapshot of one job. ExitCode is -1 while running and when the
// process ended without a code (for example by a signal).
type Job struct {
	ID         int
	Command    string
	Started    time.Time
	State      State
	ExitCode   int
	Ended      time.Time
	Monitor    bool
	Pattern    string
	EventsSent int
	Capped     bool
	// Tool is the tool that started the job and Sandbox the backend that confined it.
	Tool    string
	Sandbox string
	// NetworkOff is set when the job ran with the network denied by its sandbox.
	NetworkOff bool
	// Denied lists the credential sets whose directories the job's sandbox denies.
	Denied []string
}

// Who stopped a job, as the observer and the audit log name it.
const (
	StopByTool  = "tool"  // the agent called job_stop
	StopByUser  = "user"  // the operator used /jobs stop
	StopByClear = "clear" // the conversation was cleared
)

// Observer hears the lifecycle of the jobs of a manager. Its methods run on the
// goroutine of the job or the caller, outside the manager's lock, and must be quick.
type Observer interface {
	// Started is called once a job runs.
	Started(j Job)
	// Stopped is called when a running job is stopped on request, with who asked.
	Stopped(j Job, by string)
	// Exited is called when a job ends, before its exit event is sent.
	Exited(j Job)
}

// StartSpec describes a job. Build returns the command to run; it receives a
// context that lives as long as the job. Timeout zero means DefaultTimeout.
type StartSpec struct {
	Command string
	Timeout time.Duration
	Monitor bool
	Pattern *regexp.Regexp // nil matches every line
	Build   func(ctx context.Context) (*exec.Cmd, error)
	// Tool and Sandbox are recorded on the job for the observer.
	Tool    string
	Sandbox string
	// NetworkOff is recorded on the job: the sandbox denies its network.
	NetworkOff bool
	// Denied is recorded on the job: the credential sets its sandbox denies.
	Denied []string
}

// Manager runs the jobs of one conversation. Events go to the channel given to
// NewManager. The caller creates it and owns its delivery, so a buffered channel
// absorbs bursts; a nil channel drops every event (and counts it). Delivery
// never blocks a job for long: events go through a bounded queue drained by one
// dispatcher, which keeps their order per job. A line event is dropped when the
// queue is full or the caller is not ready at that moment. Exit and cap events
// wait for the caller up to deliverWait, then are dropped and counted. Call
// StopAll to stop the jobs and release the dispatcher.
type Manager struct {
	limit      int
	out        chan<- Event
	queue      chan Event
	dispatched chan struct{}
	dropped    atomic.Int64
	jobsWG     sync.WaitGroup
	stopOnce   sync.Once

	mu     sync.Mutex
	nextID int
	jobs   map[int]*job
	closed bool
	// obs is set once, before the first Start, by SetObserver.
	obs Observer
}

// SetObserver sets the observer of the lifecycle of the jobs. Call it before
// the first Start; a nil observer reports nothing.
func (m *Manager) SetObserver(o Observer) { m.obs = o }

// job is the internal state of one process. Fields marked mu are guarded by
// Manager.mu; the rest belong to the goroutine that runs the process.
type job struct {
	mgr        *Manager
	id         int
	command    string
	monitor    bool
	pattern    *regexp.Regexp
	started    time.Time
	tool       string
	sandbox    string
	networkOff bool
	denied     []string
	cmd        *exec.Cmd
	cancel     context.CancelFunc
	timer      *time.Timer
	done       chan struct{}

	pending []byte // partial line; only the writer goroutine touches it

	state      State     // mu
	exitCode   int       // mu
	ended      time.Time // mu
	stopReq    bool      // mu
	eventsSent int       // mu
	capped     bool      // mu
	out        []byte    // mu: ring buffer, may hold up to 2*OutputLimit bytes
}

// NewManager returns a manager that runs at most limit jobs at once (DefaultLimit
// when limit is not positive) and sends events to events.
func NewManager(limit int, events chan<- Event) *Manager {
	if limit <= 0 {
		limit = DefaultLimit
	}
	m := &Manager{
		limit:      limit,
		out:        events,
		queue:      make(chan Event, queueSize),
		dispatched: make(chan struct{}),
		jobs:       map[int]*job{},
	}
	go m.dispatch()
	return m
}

// Dropped reports how many events were dropped: no channel, a full queue, or a
// caller that did not take an event within deliverWait.
func (m *Manager) Dropped() int64 { return m.dropped.Load() }

func (m *Manager) dispatch() {
	defer close(m.dispatched)
	for ev := range m.queue {
		if m.out == nil {
			m.dropped.Add(1)
			continue
		}
		select {
		case m.out <- ev:
			continue
		default:
		}
		if ev.Kind == EventLine {
			m.dropped.Add(1) // the caller is not ready: a line is not worth a wait
			continue
		}
		t := time.NewTimer(deliverWait)
		select {
		case m.out <- ev:
		case <-t.C:
			m.dropped.Add(1)
		}
		t.Stop()
	}
}

// Start runs spec as a new job. It refuses when the limit is reached, with an
// error that lists the running jobs. The job outlives ctx: the tool call that
// started it may return before the job ends.
func (m *Manager) Start(ctx context.Context, spec StartSpec) (Job, error) {
	if spec.Build == nil {
		return Job{}, errors.New("jobs: the command is missing")
	}
	timeout := spec.Timeout
	if timeout <= 0 {
		timeout = DefaultTimeout
	}

	m.mu.Lock()
	if m.closed {
		m.mu.Unlock()
		return Job{}, errors.New("background jobs are shut down")
	}
	if running := m.runningLocked(); len(running) >= m.limit {
		m.mu.Unlock()
		return Job{}, fmt.Errorf("%d background jobs are running (limit %d); stop one with job_stop before starting another: %s",
			len(running), m.limit, describe(running))
	}
	jobCtx, cancel := context.WithCancel(context.WithoutCancel(ctx))
	cmd, err := spec.Build(jobCtx)
	if err != nil {
		m.mu.Unlock()
		cancel()
		return Job{}, err
	}
	m.nextID++
	j := &job{
		mgr:        m,
		id:         m.nextID,
		command:    spec.Command,
		monitor:    spec.Monitor,
		pattern:    spec.Pattern,
		started:    time.Now(),
		cancel:     cancel,
		done:       make(chan struct{}),
		state:      Running,
		exitCode:   -1,
		tool:       spec.Tool,
		sandbox:    spec.Sandbox,
		networkOff: spec.NetworkOff,
		denied:     spec.Denied,
	}
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.WaitDelay = time.Second
	cmd.Stdout = j // the same writer for both streams: exec calls it from one goroutine
	cmd.Stderr = j
	j.cmd = cmd
	if err := cmd.Start(); err != nil {
		m.nextID--
		m.mu.Unlock()
		cancel()
		return Job{}, fmt.Errorf("start job: %w", err)
	}
	j.timer = time.AfterFunc(timeout, func() { m.terminate(j) })
	m.jobs[j.id] = j
	m.jobsWG.Add(1)
	snap := snapshotLocked(j)
	m.mu.Unlock()

	if m.obs != nil {
		m.obs.Started(snap)
	}
	go m.wait(j)
	return snap, nil
}

// wait owns the process: it reaps it, flushes the partial line and sends the exit event.
func (m *Manager) wait(j *job) {
	defer m.jobsWG.Done()
	_ = j.cmd.Wait() // a non-zero exit is an ExitError; the code is read below
	j.timer.Stop()
	j.cancel()
	if len(j.pending) > 0 {
		m.line(j, string(j.pending))
		j.pending = nil
	}
	code := -1
	if j.cmd.ProcessState != nil {
		code = j.cmd.ProcessState.ExitCode()
	}

	m.mu.Lock()
	j.ended = time.Now()
	j.exitCode = code
	j.state = Exited
	if j.stopReq {
		j.state = Stopped
	}
	ev := Event{JobID: j.id, Kind: EventExit, ExitCode: code, Duration: j.ended.Sub(j.started), Command: j.command}
	snap := snapshotLocked(j)
	m.mu.Unlock()

	if m.obs != nil {
		m.obs.Exited(snap)
	}
	m.queue <- ev
	close(j.done)
}

// Write receives the combined stdout and stderr of the process.
func (j *job) Write(p []byte) (int, error) {
	m := j.mgr
	m.mu.Lock()
	j.out = appendRing(j.out, p)
	m.mu.Unlock()

	j.pending = append(j.pending, p...)
	for {
		i := bytes.IndexByte(j.pending, '\n')
		if i < 0 {
			break
		}
		text := string(j.pending[:i])
		j.pending = append([]byte(nil), j.pending[i+1:]...)
		m.line(j, text)
	}
	if len(j.pending) > maxPending {
		m.line(j, string(j.pending))
		j.pending = nil
	}
	return len(p), nil
}

// line handles one complete output line of a job.
func (m *Manager) line(j *job, text string) {
	if !j.monitor || (j.pattern != nil && !j.pattern.MatchString(text)) {
		return
	}
	m.mu.Lock()
	if j.capped {
		m.mu.Unlock()
		return
	}
	if j.eventsSent >= MonitorCap {
		j.capped = true
		m.mu.Unlock()
		m.queue <- Event{JobID: j.id, Kind: EventCapped, Command: j.command}
		return
	}
	j.eventsSent++
	m.mu.Unlock()
	ev := Event{JobID: j.id, Kind: EventLine, Line: clip(text), Command: j.command}
	select {
	case m.queue <- ev:
	default:
		m.dropped.Add(1)
	}
}

// Stop ends a running job: SIGTERM to its process group, then SIGKILL after
// 5 seconds. It returns when the job has ended. Stopping an ended job is not an error.
func (m *Manager) Stop(id int) (Job, error) {
	return m.StopBy(id, StopByTool)
}

// StopBy is Stop with the name of who asked, which the observer records. The
// observer hears of it only when the job was running and ends stopped.
func (m *Manager) StopBy(id int, by string) (Job, error) {
	m.mu.Lock()
	j, ok := m.jobs[id]
	wasRunning := ok && j.state == Running && !j.stopReq
	m.mu.Unlock()
	if !ok {
		return Job{}, fmt.Errorf("no job #%d", id)
	}
	m.terminate(j)
	snap := m.snapshot(j)
	if wasRunning && snap.State == Stopped && m.obs != nil {
		m.obs.Stopped(snap, by)
	}
	return snap, nil
}

// terminate runs the stop sequence once and waits for the job to end.
func (m *Manager) terminate(j *job) {
	m.mu.Lock()
	if j.state != Running || j.stopReq {
		m.mu.Unlock()
		<-j.done
		return
	}
	j.stopReq = true
	pid := j.cmd.Process.Pid
	m.mu.Unlock()

	signalGroup(pid, syscall.SIGTERM)
	select {
	case <-j.done:
	case <-time.After(killGrace):
		signalGroup(pid, syscall.SIGKILL)
		<-j.done
	}
}

// signalGroup sends sig to the process group pid. A group that is already gone is not an error.
func signalGroup(pid int, sig syscall.Signal) {
	_ = syscall.Kill(-pid, sig)
}

// List returns every job of the manager by id.
func (m *Manager) List() []Job {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]Job, 0, len(m.jobs))
	for _, j := range m.jobs {
		out = append(out, snapshotLocked(j))
	}
	sort.Slice(out, func(a, b int) bool { return out[a].ID < out[b].ID })
	return out
}

// Get returns the job with the given id.
func (m *Manager) Get(id int) (Job, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	j, ok := m.jobs[id]
	if !ok {
		return Job{}, false
	}
	return snapshotLocked(j), true
}

func (m *Manager) snapshot(j *job) Job {
	m.mu.Lock()
	defer m.mu.Unlock()
	return snapshotLocked(j)
}

// Output returns the last OutputLimit bytes of the combined output of a job.
func (m *Manager) Output(id int) (string, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	j, ok := m.jobs[id]
	if !ok {
		return "", false
	}
	b := j.out
	if len(b) > OutputLimit {
		b = b[len(b)-OutputLimit:]
	}
	return string(b), true
}

// Tail returns the last n lines of the output of a job. The first line may
// start inside the ring buffer, so it can be cut.
func (m *Manager) Tail(id, n int) []string {
	out, ok := m.Output(id)
	if !ok || out == "" || n <= 0 {
		return nil
	}
	lines := strings.Split(strings.TrimRight(out, "\n"), "\n")
	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	return lines
}

// StopAll stops every running job, waits for their processes and exit events,
// and releases the dispatcher. Later Start calls are refused. It is safe to call twice.
func (m *Manager) StopAll() {
	m.stopOnce.Do(func() {
		m.mu.Lock()
		m.closed = true
		var running []*job
		for _, j := range m.jobs {
			if j.state == Running {
				running = append(running, j)
			}
		}
		m.mu.Unlock()

		var wg sync.WaitGroup
		for _, j := range running {
			wg.Add(1)
			go func(j *job) {
				defer wg.Done()
				m.terminate(j)
			}(j)
		}
		wg.Wait()
		m.jobsWG.Wait()
		close(m.queue)
		<-m.dispatched
	})
}

func (m *Manager) runningLocked() []*job {
	var out []*job
	for _, j := range m.jobs {
		if j.state == Running {
			out = append(out, j)
		}
	}
	sort.Slice(out, func(a, b int) bool { return out[a].id < out[b].id })
	return out
}

func snapshotLocked(j *job) Job {
	pattern := ""
	if j.pattern != nil {
		pattern = j.pattern.String()
	}
	return Job{
		ID: j.id, Command: j.command, Started: j.started, State: j.state,
		ExitCode: j.exitCode, Ended: j.ended, Monitor: j.monitor, Pattern: pattern,
		EventsSent: j.eventsSent, Capped: j.capped,
		Tool: j.tool, Sandbox: j.sandbox, NetworkOff: j.networkOff, Denied: j.denied,
	}
}

func describe(running []*job) string {
	parts := make([]string, 0, len(running))
	for _, j := range running {
		parts = append(parts, fmt.Sprintf("#%d %q (%s)", j.id, j.command, time.Since(j.started).Round(time.Second)))
	}
	return strings.Join(parts, ", ")
}

// appendRing appends p and keeps the buffer between OutputLimit and 2*OutputLimit
// bytes, so the copy is paid once per OutputLimit bytes, not on every write.
func appendRing(buf, p []byte) []byte {
	buf = append(buf, p...)
	if len(buf) > 2*OutputLimit {
		buf = append([]byte(nil), buf[len(buf)-OutputLimit:]...)
	}
	return buf
}

// clip cuts a line to maxLineBytes without splitting a UTF-8 sequence.
func clip(s string) string {
	if len(s) <= maxLineBytes {
		return s
	}
	return strings.ToValidUTF8(s[:maxLineBytes], "") + "..."
}
