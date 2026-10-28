package jobs

import (
	"context"
	"sync"
	"testing"
)

// recObserver records what the manager reports about its jobs.
type recObserver struct {
	mu      sync.Mutex
	started []Job
	stopped []Job
	by      []string
	exited  []Job
}

func (r *recObserver) Started(j Job) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.started = append(r.started, j)
}

func (r *recObserver) Stopped(j Job, by string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.stopped = append(r.stopped, j)
	r.by = append(r.by, by)
}

func (r *recObserver) Exited(j Job) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.exited = append(r.exited, j)
}

func TestObserverSeesStartStopAndExitWithTheirTool(t *testing.T) {
	m, events := newTestManager(t, DefaultLimit)
	obs := &recObserver{}
	m.SetObserver(obs)

	job, err := m.Start(context.Background(), StartSpec{
		Command: "sleep 60", Tool: "bash_background", Sandbox: "none", Build: shBuild("sleep 60"),
	})
	if err != nil {
		t.Fatal(err)
	}
	if job.Tool != "bash_background" || job.Sandbox != "none" {
		t.Fatalf("snapshot = tool %q sandbox %q", job.Tool, job.Sandbox)
	}
	if _, err := m.StopBy(job.ID, StopByUser); err != nil {
		t.Fatal(err)
	}
	untilExit(t, events, job.ID)

	obs.mu.Lock()
	defer obs.mu.Unlock()
	if len(obs.started) != 1 || obs.started[0].Tool != "bash_background" {
		t.Fatalf("started = %+v", obs.started)
	}
	if len(obs.stopped) != 1 || obs.by[0] != StopByUser || obs.stopped[0].ID != job.ID {
		t.Fatalf("stopped = %+v by %v", obs.stopped, obs.by)
	}
	if len(obs.exited) != 1 || obs.exited[0].State != Stopped || obs.exited[0].Ended.IsZero() {
		t.Fatalf("exited = %+v", obs.exited)
	}
}

func TestObserverIgnoresStopOfEndedJob(t *testing.T) {
	m, events := newTestManager(t, DefaultLimit)
	obs := &recObserver{}
	m.SetObserver(obs)

	job, err := m.Start(context.Background(), StartSpec{Command: "true", Build: shBuild("true")})
	if err != nil {
		t.Fatal(err)
	}
	untilExit(t, events, job.ID)
	if _, err := m.StopBy(job.ID, StopByTool); err != nil {
		t.Fatal(err)
	}

	obs.mu.Lock()
	defer obs.mu.Unlock()
	if len(obs.stopped) != 0 {
		t.Fatalf("stop of an ended job was recorded: %+v", obs.stopped)
	}
	if len(obs.exited) != 1 || obs.exited[0].State != Exited {
		t.Fatalf("exited = %+v, want one exited job", obs.exited)
	}
}
