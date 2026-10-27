package jobs

import (
	"context"
	"os/exec"
	"regexp"
	"strings"
	"testing"
	"time"
)

// shBuild runs command with sh -c, the way the tool layer builds a job without a sandbox.
func shBuild(command string) func(context.Context) (*exec.Cmd, error) {
	return func(ctx context.Context) (*exec.Cmd, error) {
		return exec.CommandContext(ctx, "sh", "-c", command), nil
	}
}

func newTestManager(t *testing.T, limit int) (*Manager, chan Event) {
	t.Helper()
	events := make(chan Event, 4096)
	m := NewManager(limit, events)
	t.Cleanup(m.StopAll)
	return m, events
}

// untilExit reads events until the exit event of job id arrives, or fails the test.
func untilExit(t *testing.T, events <-chan Event, id int) []Event {
	t.Helper()
	var got []Event
	timeout := time.After(10 * time.Second)
	for {
		select {
		case ev := <-events:
			if ev.JobID != id {
				continue
			}
			got = append(got, ev)
			if ev.Kind == EventExit {
				return got
			}
		case <-timeout:
			t.Fatalf("no exit event for job %d; got %v", id, got)
		}
	}
}

func kinds(evs []Event, kind EventKind) []Event {
	var out []Event
	for _, ev := range evs {
		if ev.Kind == kind {
			out = append(out, ev)
		}
	}
	return out
}

func lines(evs []Event) []string {
	var out []string
	for _, ev := range kinds(evs, EventLine) {
		out = append(out, ev.Line)
	}
	return out
}

func TestExitEventCarriesCodeAndDuration(t *testing.T) {
	m, events := newTestManager(t, DefaultLimit)
	job, err := m.Start(context.Background(), StartSpec{Command: "exit 3", Build: shBuild("exit 3")})
	if err != nil {
		t.Fatal(err)
	}
	evs := untilExit(t, events, job.ID)
	exit := kinds(evs, EventExit)
	if len(exit) != 1 {
		t.Fatalf("exit events = %d, want 1", len(exit))
	}
	if exit[0].ExitCode != 3 || exit[0].Command != "exit 3" || exit[0].Duration <= 0 {
		t.Fatalf("exit event = %+v", exit[0])
	}
	got, ok := m.Get(job.ID)
	if !ok || got.State != Exited || got.ExitCode != 3 || got.Ended.IsZero() {
		t.Fatalf("job after exit = %+v, ok %v", got, ok)
	}
}

func TestNoLineEventsWithoutMonitor(t *testing.T) {
	m, events := newTestManager(t, DefaultLimit)
	job, err := m.Start(context.Background(), StartSpec{Command: "echo hi", Build: shBuild("echo hi")})
	if err != nil {
		t.Fatal(err)
	}
	evs := untilExit(t, events, job.ID)
	if len(evs) != 1 {
		t.Fatalf("events = %+v, want only the exit event", evs)
	}
}

func TestMonitorEmitsEveryLineWithoutPattern(t *testing.T) {
	m, events := newTestManager(t, DefaultLimit)
	job, err := m.Start(context.Background(), StartSpec{Command: "printf", Monitor: true, Build: shBuild("printf 'a\\nb\\nlast'")})
	if err != nil {
		t.Fatal(err)
	}
	evs := untilExit(t, events, job.ID)
	if got := lines(evs); strings.Join(got, ",") != "a,b,last" {
		t.Fatalf("line events = %q, want a,b,last (the unterminated last line is flushed at exit)", got)
	}
}

func TestMonitorFiltersByPattern(t *testing.T) {
	m, events := newTestManager(t, DefaultLimit)
	job, err := m.Start(context.Background(), StartSpec{
		Command: "grep", Monitor: true, Pattern: regexp.MustCompile(`ERROR`),
		Build: shBuild("printf 'ok 1\\nERROR disk full\\nok 2\\nERROR again\\n'"),
	})
	if err != nil {
		t.Fatal(err)
	}
	evs := untilExit(t, events, job.ID)
	if got := lines(evs); strings.Join(got, "|") != "ERROR disk full|ERROR again" {
		t.Fatalf("line events = %q", got)
	}
	got, _ := m.Get(job.ID)
	if got.EventsSent != 2 || got.Pattern != "ERROR" || got.Capped {
		t.Fatalf("job = %+v", got)
	}
}

func TestMonitorCapsLineEventsAt50(t *testing.T) {
	m, events := newTestManager(t, DefaultLimit)
	script := `i=0; while [ $i -lt 120 ]; do echo line$i; i=$((i+1)); done`
	job, err := m.Start(context.Background(), StartSpec{Command: script, Monitor: true, Build: shBuild(script)})
	if err != nil {
		t.Fatal(err)
	}
	evs := untilExit(t, events, job.ID)
	if n := len(lines(evs)); n != MonitorCap {
		t.Fatalf("line events = %d, want %d", n, MonitorCap)
	}
	if n := len(kinds(evs, EventCapped)); n != 1 {
		t.Fatalf("capped events = %d, want 1", n)
	}
	if n := len(kinds(evs, EventExit)); n != 1 {
		t.Fatalf("exit events = %d, want 1", n)
	}
	if last := evs[len(evs)-1]; last.Kind != EventExit {
		t.Fatalf("last event = %v, want exit", last.Kind)
	}
	got, _ := m.Get(job.ID)
	if !got.Capped || got.EventsSent != MonitorCap {
		t.Fatalf("job = %+v", got)
	}
}

func TestConcurrencyLimitRefusesFifthJob(t *testing.T) {
	m, _ := newTestManager(t, 4)
	for i := 0; i < 4; i++ {
		if _, err := m.Start(context.Background(), StartSpec{Command: "sleep 30", Build: shBuild("sleep 30")}); err != nil {
			t.Fatalf("start %d: %v", i, err)
		}
	}
	_, err := m.Start(context.Background(), StartSpec{Command: "sleep 31", Build: shBuild("sleep 31")})
	if err == nil {
		t.Fatal("fifth start succeeded, want refusal")
	}
	for _, want := range []string{"4", "limit", "#1", "sleep 30", "#4"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("error %q does not mention %q", err, want)
		}
	}
}

func TestStopFreesASlot(t *testing.T) {
	m, _ := newTestManager(t, 1)
	job, err := m.Start(context.Background(), StartSpec{Command: "sleep 30", Build: shBuild("sleep 30")})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := m.Start(context.Background(), StartSpec{Command: "true", Build: shBuild("true")}); err == nil {
		t.Fatal("second start with limit 1 succeeded")
	}
	if _, err := m.Stop(job.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := m.Start(context.Background(), StartSpec{Command: "true", Build: shBuild("true")}); err != nil {
		t.Fatalf("start after stop: %v", err)
	}
}

func TestStopEndsSleepWithStoppedState(t *testing.T) {
	m, events := newTestManager(t, DefaultLimit)
	job, err := m.Start(context.Background(), StartSpec{Command: "sleep 60", Build: shBuild("sleep 60")})
	if err != nil {
		t.Fatal(err)
	}
	began := time.Now()
	stopped, err := m.Stop(job.ID)
	if err != nil {
		t.Fatal(err)
	}
	if elapsed := time.Since(began); elapsed > 2*time.Second {
		t.Fatalf("stop took %s, want about 1s", elapsed)
	}
	if stopped.State != Stopped {
		t.Fatalf("state = %s, want stopped", stopped.State)
	}
	exit := kinds(untilExit(t, events, job.ID), EventExit)
	if len(exit) != 1 || exit[0].ExitCode != -1 {
		t.Fatalf("exit after SIGTERM = %+v, want one exit event with code -1", exit)
	}
	again, err := m.Stop(job.ID)
	if err != nil || again.State != Stopped {
		t.Fatalf("second stop = %+v, %v; want idempotent", again, err)
	}
}

func TestStopEscalatesToKillWhenTermIsIgnored(t *testing.T) {
	m, events := newTestManager(t, DefaultLimit)
	script := "trap '' TERM; sleep 60"
	job, err := m.Start(context.Background(), StartSpec{Command: script, Build: shBuild(script)})
	if err != nil {
		t.Fatal(err)
	}
	time.Sleep(200 * time.Millisecond) // let the shell install the trap
	began := time.Now()
	stopped, err := m.Stop(job.ID)
	if err != nil {
		t.Fatal(err)
	}
	elapsed := time.Since(began)
	if elapsed < killGrace-500*time.Millisecond || elapsed > killGrace+3*time.Second {
		t.Fatalf("stop took %s, want about %s (SIGKILL after the grace period)", elapsed, killGrace)
	}
	if stopped.State != Stopped {
		t.Fatalf("state = %s, want stopped", stopped.State)
	}
	untilExit(t, events, job.ID)
}

func TestTimeoutCeilingStopsJob(t *testing.T) {
	m, events := newTestManager(t, DefaultLimit)
	job, err := m.Start(context.Background(), StartSpec{Command: "sleep 60", Timeout: 300 * time.Millisecond, Build: shBuild("sleep 60")})
	if err != nil {
		t.Fatal(err)
	}
	evs := untilExit(t, events, job.ID)
	if exit := kinds(evs, EventExit); len(exit) != 1 || exit[0].ExitCode != -1 {
		t.Fatalf("exit = %+v, want code -1 after the ceiling", exit)
	}
	got, _ := m.Get(job.ID)
	if got.State != Stopped {
		t.Fatalf("state = %s, want stopped", got.State)
	}
}

func TestDefaultTimeoutIsThirtyMinutes(t *testing.T) {
	if DefaultTimeout != 30*time.Minute {
		t.Fatalf("DefaultTimeout = %s, want 30m", DefaultTimeout)
	}
}

func TestOutputAndTail(t *testing.T) {
	m, events := newTestManager(t, DefaultLimit)
	job, err := m.Start(context.Background(), StartSpec{Command: "printf", Build: shBuild("printf 'l1\\nl2\\nl3\\n'")})
	if err != nil {
		t.Fatal(err)
	}
	untilExit(t, events, job.ID)
	out, ok := m.Output(job.ID)
	if !ok || out != "l1\nl2\nl3\n" {
		t.Fatalf("output = %q, %v", out, ok)
	}
	if got := m.Tail(job.ID, 2); strings.Join(got, ",") != "l2,l3" {
		t.Fatalf("tail 2 = %q", got)
	}
	if got := m.Tail(job.ID, 10); len(got) != 3 {
		t.Fatalf("tail 10 = %q, want all three lines", got)
	}
}

func TestOutputKeepsLast64KiB(t *testing.T) {
	m, events := newTestManager(t, DefaultLimit)
	script := "head -c 200000 /dev/zero | tr '\\0' a; printf END"
	job, err := m.Start(context.Background(), StartSpec{Command: "big", Build: shBuild(script)})
	if err != nil {
		t.Fatal(err)
	}
	untilExit(t, events, job.ID)
	out, _ := m.Output(job.ID)
	if len(out) != OutputLimit {
		t.Fatalf("output length = %d, want %d", len(out), OutputLimit)
	}
	if !strings.HasSuffix(out, "END") {
		t.Fatalf("output does not end with the last bytes written: %q", out[len(out)-10:])
	}
}

func TestUnknownJobIDs(t *testing.T) {
	m, _ := newTestManager(t, DefaultLimit)
	if _, ok := m.Get(99); ok {
		t.Fatal("Get found an unknown job")
	}
	if _, ok := m.Output(99); ok {
		t.Fatal("Output found an unknown job")
	}
	if got := m.Tail(99, 5); got != nil {
		t.Fatalf("Tail of unknown job = %q", got)
	}
	if _, err := m.Stop(99); err == nil {
		t.Fatal("Stop of unknown job returned no error")
	}
}

func TestStopOnEndedJobIsIdempotent(t *testing.T) {
	m, events := newTestManager(t, DefaultLimit)
	job, err := m.Start(context.Background(), StartSpec{Command: "exit 0", Build: shBuild("exit 0")})
	if err != nil {
		t.Fatal(err)
	}
	untilExit(t, events, job.ID)
	got, err := m.Stop(job.ID)
	if err != nil || got.State != Exited || got.ExitCode != 0 {
		t.Fatalf("stop of ended job = %+v, %v", got, err)
	}
}

func TestListIsOrderedByID(t *testing.T) {
	m, events := newTestManager(t, DefaultLimit)
	var ids []int
	for i := 0; i < 3; i++ {
		j, err := m.Start(context.Background(), StartSpec{Command: "true", Build: shBuild("true")})
		if err != nil {
			t.Fatal(err)
		}
		ids = append(ids, j.ID)
	}
	for _, id := range ids {
		untilExit(t, events, id)
	}
	list := m.List()
	if len(list) != 3 || list[0].ID != 1 || list[1].ID != 2 || list[2].ID != 3 {
		t.Fatalf("list = %+v", list)
	}
}

func TestStartFailureReturnsErrorAndNoJob(t *testing.T) {
	m, _ := newTestManager(t, DefaultLimit)
	_, err := m.Start(context.Background(), StartSpec{Command: "x", Build: func(ctx context.Context) (*exec.Cmd, error) {
		return nil, context.Canceled
	}})
	if err == nil {
		t.Fatal("start with a failing build succeeded")
	}
	if len(m.List()) != 0 {
		t.Fatalf("list after failed start = %+v", m.List())
	}
}

func TestStopAllStopsEverythingAndRefusesNewJobs(t *testing.T) {
	m, _ := newTestManager(t, DefaultLimit)
	for i := 0; i < 2; i++ {
		if _, err := m.Start(context.Background(), StartSpec{Command: "sleep 60", Build: shBuild("sleep 60")}); err != nil {
			t.Fatal(err)
		}
	}
	began := time.Now()
	m.StopAll()
	if elapsed := time.Since(began); elapsed > 3*time.Second {
		t.Fatalf("StopAll took %s", elapsed)
	}
	for _, j := range m.List() {
		if j.State != Stopped {
			t.Fatalf("job %d state = %s after StopAll", j.ID, j.State)
		}
	}
	if _, err := m.Start(context.Background(), StartSpec{Command: "true", Build: shBuild("true")}); err == nil {
		t.Fatal("start after StopAll succeeded")
	}
	m.StopAll() // idempotent
}

func TestNilEventsChannelDropsEvents(t *testing.T) {
	m := NewManager(DefaultLimit, nil)
	t.Cleanup(m.StopAll)
	job, err := m.Start(context.Background(), StartSpec{Command: "printf", Monitor: true, Build: shBuild("echo x")})
	if err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(10 * time.Second)
	for {
		got, _ := m.Get(job.ID)
		if got.State == Exited {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("job did not exit")
		}
		time.Sleep(10 * time.Millisecond)
	}
	if m.Dropped() == 0 {
		t.Fatal("events with no channel were not counted as dropped")
	}
}

func TestSlowConsumerNeverBlocksTheJob(t *testing.T) {
	saved := deliverWait
	deliverWait = 20 * time.Millisecond
	t.Cleanup(func() { deliverWait = saved })

	unread := make(chan Event) // nobody reads it
	m := NewManager(DefaultLimit, unread)
	t.Cleanup(m.StopAll)
	// Each job emits at most MonitorCap line events, so eight jobs in a row
	// overflow the 256-slot queue while nothing is read.
	script := `i=0; while [ $i -lt 60 ]; do echo line$i; i=$((i+1)); done`
	deadline := time.Now().Add(20 * time.Second)
	for n := 0; n < 8; n++ {
		job, err := m.Start(context.Background(), StartSpec{Command: "flood", Monitor: true, Build: shBuild(script)})
		if err != nil {
			t.Fatal(err)
		}
		for {
			got, _ := m.Get(job.ID)
			if got.State != Running {
				break
			}
			if time.Now().After(deadline) {
				t.Fatalf("job %d blocked on an unread events channel", job.ID)
			}
			time.Sleep(10 * time.Millisecond)
		}
	}
	if m.Dropped() == 0 {
		t.Fatal("no events were dropped although nobody read the channel")
	}
}
