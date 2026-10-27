package tool

import (
	"context"
	"encoding/json"
	"slices"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/tinchoram/groot-cli/internal/jobs"
	shellsandbox "github.com/tinchoram/groot-cli/internal/sandbox"
)

// jobHarness is a manager whose events are read by the test.
type jobHarness struct {
	m      *jobs.Manager
	events chan jobs.Event
}

func newJobHarness(t *testing.T) jobHarness {
	t.Helper()
	events := make(chan jobs.Event, 1024)
	m := jobs.NewManager(0, events)
	t.Cleanup(m.StopAll)
	return jobHarness{m: m, events: events}
}

// untilExit waits for the exit event of job id and returns every event of that job.
func (h jobHarness) untilExit(t *testing.T, id int) []jobs.Event {
	t.Helper()
	var got []jobs.Event
	timeout := time.After(10 * time.Second)
	for {
		select {
		case ev := <-h.events:
			if ev.JobID != id {
				continue
			}
			got = append(got, ev)
			if ev.Kind == jobs.EventExit {
				return got
			}
		case <-timeout:
			t.Fatalf("job %d did not exit; events %+v", id, got)
		}
	}
}

func TestJobToolInputDecoding(t *testing.T) {
	h := newJobHarness(t)
	b := NewBash("")
	cases := []struct {
		name string
		tool interface {
			Execute(context.Context, string) (string, bool)
		}
		input string
		want  string
	}{
		{"background without command", NewBashBackground(b, h.m), `{}`, "non-empty command"},
		{"background zero timeout", NewBashBackground(b, h.m), `{"command":"true","timeout_seconds":0}`, "timeout_seconds must be a positive integer"},
		{"background bad json", NewBashBackground(b, h.m), `{"command":`, "non-empty command"},
		{"monitor bad pattern", NewMonitor(b, h.m), `{"command":"true","pattern":"("}`, "invalid pattern"},
		{"monitor without command", NewMonitor(b, h.m), `{"pattern":"x"}`, "non-empty command"},
		{"output without id", NewJobOutput(h.m, ""), `{}`, "id must be the job number"},
		{"output with string id", NewJobOutput(h.m, ""), `{"id":"1"}`, "invalid input"},
		{"stop without id", NewJobStop(h.m), `{"id":0}`, "id must be the job number"},
		{"stop unknown id", NewJobStop(h.m), `{"id":42}`, "no job #42"},
		{"output unknown id", NewJobOutput(h.m, ""), `{"id":42}`, "no job #42"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			out, isErr := c.tool.Execute(context.Background(), c.input)
			if !isErr || !strings.Contains(out, c.want) {
				t.Fatalf("out = %q, isErr = %v; want an error containing %q", out, isErr, c.want)
			}
		})
	}
}

func TestJobToolRisk(t *testing.T) {
	h := newJobHarness(t)
	b := NewBash("")
	cases := []struct {
		tool Tool
		want Risk
	}{
		{NewBashBackground(b, h.m), RiskExec},
		{NewMonitor(b, h.m), RiskExec},
		{NewJobStop(h.m), RiskExec},
		{NewJobList(h.m), RiskRead},
		{NewJobOutput(h.m, ""), RiskRead},
	}
	for _, c := range cases {
		name := c.tool.Definition().Name
		if got := c.tool.Risk(); got != c.want {
			t.Errorf("%s Risk() = %s, want %s", name, got, c.want)
		}
		if got := c.tool.RiskFor(`{"command":"ls","id":1}`); got != c.want {
			t.Errorf("%s RiskFor = %s, want %s", name, got, c.want)
		}
	}
}

func TestBashBackgroundIsExecEvenForReadOnlyCommands(t *testing.T) {
	h := newJobHarness(t)
	bg := NewBashBackground(NewBash(""), h.m)
	if got := bg.RiskFor(`{"command":"ls -la"}`); got != RiskExec {
		t.Fatalf("RiskFor(ls) = %s, want exec: a background command keeps running after the call", got)
	}
}

func TestJobStartToolsPassDestructiveCommandsThrough(t *testing.T) {
	h := newJobHarness(t)
	b := NewBash("")
	for _, tl := range []Tool{NewBashBackground(b, h.m), NewMonitor(b, h.m)} {
		d, ok := tl.(Destructive)
		if !ok {
			t.Fatalf("%s does not implement Destructive", tl.Definition().Name)
		}
		kind, reason, bad := d.DestructiveFor(`{"command":"rm -rf build"}`)
		if !bad || kind != DestructiveDelete || reason == "" {
			t.Fatalf("%s: rm -rf not reported: %s %q %v", tl.Definition().Name, kind, reason, bad)
		}
		if _, _, bad := d.DestructiveFor(`{"command":"ls build"}`); bad {
			t.Fatalf("%s: ls reported as destructive", tl.Definition().Name)
		}
	}
}

func TestJobToolsReportSandboxName(t *testing.T) {
	h := newJobHarness(t)
	b := NewBash("")
	b.Runner = &recordRunner{name: shellsandbox.BackendBwrap}
	bg := NewBashBackground(b, h.m)
	if got := bg.SandboxName(); got != shellsandbox.BackendBwrap {
		t.Fatalf("SandboxName = %q", got)
	}
}

func TestBashBackgroundSpecMatchesBash(t *testing.T) {
	dir := t.TempDir()
	rec := &recordRunner{name: shellsandbox.BackendBwrap}
	b := NewBash(dir)
	b.Runner = rec
	b.Network = true
	b.ExtraWritable = []string{"/opt/cache"}

	if _, isErr := b.Execute(context.Background(), `{"command":"true"}`); isErr {
		t.Fatal("bash failed")
	}
	foreground := rec.spec

	h := newJobHarness(t)
	bg := NewBashBackground(b, h.m)
	out, isErr := bg.Execute(context.Background(), `{"command":"true"}`)
	if isErr {
		t.Fatalf("bash_background failed: %s", out)
	}
	ev := h.untilExit(t, 1)
	if ev[len(ev)-1].ExitCode != 0 {
		t.Fatalf("exit = %+v", ev)
	}
	if rec.spec.Dir != foreground.Dir || rec.spec.AllowNetwork != foreground.AllowNetwork ||
		!slices.Equal(rec.spec.WritablePaths, foreground.WritablePaths) || !slices.Equal(rec.spec.Command, foreground.Command) {
		t.Fatalf("background spec %+v differs from bash spec %+v", rec.spec, foreground)
	}
	if rec.spec.Dir != dir || !rec.spec.AllowNetwork || !slices.Equal(rec.spec.WritablePaths, []string{dir, "/opt/cache"}) {
		t.Fatalf("spec = %+v", rec.spec)
	}
}

func TestBashBackgroundStartsAndExitsWithoutSandbox(t *testing.T) {
	h := newJobHarness(t)
	bg := NewBashBackground(NewBash(t.TempDir()), h.m)
	out, isErr := bg.Execute(context.Background(), `{"command":"echo hello; exit 4"}`)
	if isErr {
		t.Fatalf("start failed: %s", out)
	}
	if !strings.HasPrefix(out, "job #1 started: echo hello; exit 4") {
		t.Fatalf("start line = %q", out)
	}
	if !strings.Contains(out, "event") || !strings.Contains(out, "job_stop") {
		t.Fatalf("start line does not state the rules: %q", out)
	}
	ev := h.untilExit(t, 1)
	if exit := ev[len(ev)-1]; exit.Kind != jobs.EventExit || exit.ExitCode != 4 {
		t.Fatalf("exit = %+v", exit)
	}

	list, _ := NewJobList(h.m).Execute(context.Background(), `{}`)
	for _, want := range []string{"#1", "exited", "4", "echo hello"} {
		if !strings.Contains(list, want) {
			t.Fatalf("job_list = %q, want %q", list, want)
		}
	}
	output, isErr := NewJobOutput(h.m, "").Execute(context.Background(), `{"id":1}`)
	if isErr || !strings.Contains(output, "hello") {
		t.Fatalf("job_output = %q, %v", output, isErr)
	}
}

func TestJobStopEndsASleep(t *testing.T) {
	h := newJobHarness(t)
	bg := NewBashBackground(NewBash(t.TempDir()), h.m)
	if out, isErr := bg.Execute(context.Background(), `{"command":"sleep 60"}`); isErr {
		t.Fatalf("start failed: %s", out)
	}
	began := time.Now()
	out, isErr := NewJobStop(h.m).Execute(context.Background(), `{"id":1}`)
	if isErr {
		t.Fatalf("job_stop failed: %s", out)
	}
	if time.Since(began) > 3*time.Second {
		t.Fatalf("job_stop took %s", time.Since(began))
	}
	if !strings.Contains(out, "job #1 stopped") || !strings.Contains(out, "sleep 60") {
		t.Fatalf("job_stop line = %q", out)
	}
}

func TestBashBackgroundRefusesTheFifthJob(t *testing.T) {
	h := newJobHarness(t)
	bg := NewBashBackground(NewBash(t.TempDir()), h.m)
	for i := 0; i < jobs.DefaultLimit; i++ {
		if out, isErr := bg.Execute(context.Background(), `{"command":"sleep 60"}`); isErr {
			t.Fatalf("start %d failed: %s", i+1, out)
		}
	}
	out, isErr := bg.Execute(context.Background(), `{"command":"sleep 61"}`)
	if !isErr || !strings.Contains(out, "sleep 60") {
		t.Fatalf("fifth start = %q, %v; want a refusal that lists the running jobs", out, isErr)
	}
}

func TestMonitorTurnsMatchingLinesIntoEvents(t *testing.T) {
	h := newJobHarness(t)
	mon := NewMonitor(NewBash(t.TempDir()), h.m)
	out, isErr := mon.Execute(context.Background(), `{"command":"printf 'ok\\nERROR one\\nERROR two\\n'","pattern":"ERROR"}`)
	if isErr || !strings.HasPrefix(out, "job #1 watching:") {
		t.Fatalf("monitor start = %q, %v", out, isErr)
	}
	ev := h.untilExit(t, 1)
	var lines []string
	for _, e := range ev {
		if e.Kind == jobs.EventLine {
			lines = append(lines, e.Line)
		}
	}
	if strings.Join(lines, "|") != "ERROR one|ERROR two" {
		t.Fatalf("line events = %q", lines)
	}
}

func TestJobListShowsEmptyAndRunningJobs(t *testing.T) {
	h := newJobHarness(t)
	if out, _ := NewJobList(h.m).Execute(context.Background(), `{}`); !strings.Contains(out, "no background jobs") {
		t.Fatalf("empty list = %q", out)
	}
	bg := NewBashBackground(NewBash(t.TempDir()), h.m)
	if _, isErr := bg.Execute(context.Background(), `{"command":"sleep 60"}`); isErr {
		t.Fatal("start failed")
	}
	out, _ := NewJobList(h.m).Execute(context.Background(), `{}`)
	if !strings.Contains(out, "running") || !strings.Contains(out, "sleep 60") {
		t.Fatalf("list = %q", out)
	}
	_, _ = NewJobStop(h.m).Execute(context.Background(), `{"id":1}`)
}

func TestJobOutputSpillsLikeBash(t *testing.T) {
	h := newJobHarness(t)
	dir := t.TempDir()
	bg := NewBashBackground(NewBash(dir), h.m)
	if _, isErr := bg.Execute(context.Background(), `{"command":"head -c 60000 /dev/zero | tr '\\0' a"}`); isErr {
		t.Fatal("start failed")
	}
	h.untilExit(t, 1)
	out, isErr := NewJobOutput(h.m, dir).Execute(context.Background(), `{"id":1}`)
	if isErr {
		t.Fatalf("job_output failed: %s", out)
	}
	if !strings.Contains(out, "characters total; full text saved to ") || !strings.Contains(out, "/.groot/outputs/job_output-") {
		t.Fatalf("output of 60000 bytes did not spill to a file:\n%s", firstRunes(out, 300))
	}
	if utf8.RuneCountInString(out) > outputInlineChars {
		t.Fatalf("inline output is %d characters, want at most %d", utf8.RuneCountInString(out), outputInlineChars)
	}
}

func TestJobTimeoutDefaultsAndCeiling(t *testing.T) {
	b := NewBash("")
	b.MaxTimeout = 10 * time.Minute

	got, notice, err := b.jobTimeout(nil)
	if err != nil || got != jobs.DefaultTimeout || notice != "" {
		t.Fatalf("default = %s, %q, %v; want 30m", got, notice, err)
	}
	secs := 900
	got, notice, err = b.jobTimeout(&secs)
	if err != nil || got != 10*time.Minute || !strings.Contains(notice, "clamped to 600") {
		t.Fatalf("900s = %s, %q, %v; want the 600s ceiling with a notice", got, notice, err)
	}
	secs = 120
	got, _, _ = b.jobTimeout(&secs)
	if got != 2*time.Minute {
		t.Fatalf("120s = %s", got)
	}
}

func TestJobOutputRedactsSecrets(t *testing.T) {
	h := newJobHarness(t)
	bg := NewBashBackground(NewBash(t.TempDir()), h.m)
	if _, isErr := bg.Execute(context.Background(), `{"command":"echo password=hunter2"}`); isErr {
		t.Fatal("start failed")
	}
	h.untilExit(t, 1)
	out, _ := NewJobOutput(h.m, "").Execute(context.Background(), `{"id":1}`)
	if strings.Contains(out, "hunter2") {
		t.Fatalf("job_output leaks a secret: %q", out)
	}
}

func TestJobToolDefinitionsAreValidJSONSchemas(t *testing.T) {
	h := newJobHarness(t)
	b := NewBash("")
	for _, tl := range []Tool{NewBashBackground(b, h.m), NewMonitor(b, h.m), NewJobList(h.m), NewJobOutput(h.m, ""), NewJobStop(h.m)} {
		def := tl.Definition()
		if def.Description == "" {
			t.Errorf("%s has no description", def.Name)
		}
		var schema map[string]any
		if err := json.Unmarshal(def.InputSchema, &schema); err != nil {
			t.Errorf("%s schema: %v", def.Name, err)
		}
	}
}
