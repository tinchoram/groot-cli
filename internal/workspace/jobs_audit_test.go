package workspace

import (
	"context"
	"strings"
	"testing"

	"github.com/tinchoram/groot-cli/internal/audit"
	"github.com/tinchoram/groot-cli/internal/tool"
)

func TestJobStartExitAndStopAreAuditedWithTheSpace(t *testing.T) {
	f := newJobFixture(t,
		jobCall("t1", "bash_background", `{"command":"sh -c 'exit 4'"}`),
		text("started"),
		jobCall("t2", "bash_background", `{"command":"sleep 60"}`),
		text("second started"))
	ctx := context.Background()
	if _, err := f.ws.Chat().Send(ctx, "first"); err != nil {
		t.Fatal(err)
	}
	waitEvents(t, f, 1)
	if _, err := f.ws.Chat().Send(ctx, "second"); err != nil {
		t.Fatal(err)
	}
	if _, err := f.ws.StopJob(f.ws.ConversationKey(), 2); err != nil {
		t.Fatal(err)
	}

	starts, _, err := f.ws.Audit(ctx, "", audit.Query{Tool: "job_start", Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	if len(starts) != 2 {
		t.Fatalf("job_start records = %d, want 2", len(starts))
	}
	start := starts[1] // oldest first is not guaranteed; find job 1
	for _, r := range starts {
		if strings.Contains(r.InputPreview, "#1 ") {
			start = r
		}
	}
	if start.Space != "default" || start.Agent != "runner" || start.Session == "" {
		t.Fatalf("job_start does not name the space and agent: %+v", start)
	}
	if start.Sandbox != tool.NewBash(f.dir).SandboxName() || !strings.Contains(start.InputPreview, "sh -c 'exit 4'") {
		t.Fatalf("job_start sandbox %q, preview %q", start.Sandbox, start.InputPreview)
	}

	exits, _, err := f.ws.Audit(ctx, "", audit.Query{Tool: "job_exit", Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	if len(exits) != 2 {
		t.Fatalf("job_exit records = %+v; want the exit of job 1 and the stop of job 2", exits)
	}
	first := exits[1] // newest first
	if !strings.Contains(first.InputPreview, "#1 exited exit 4") || !first.IsError || first.Agent != "runner" || first.DurationMS < 0 {
		t.Fatalf("job_exit of job 1 = %+v", first)
	}

	stops, _, err := f.ws.Audit(ctx, "", audit.Query{Tool: "job_stop", Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	if len(stops) != 1 || !strings.Contains(stops[0].InputPreview, "by user") {
		t.Fatalf("job_stop records = %+v; want one stop by user", stops)
	}

	stoppedExit, _, _ := f.ws.Audit(ctx, "", audit.Query{Tool: "job_exit", Find: "sleep 60", Limit: 10})
	if len(stoppedExit) != 1 || !strings.Contains(stoppedExit[0].InputPreview, "stopped") {
		t.Fatalf("the stopped job exit = %+v", stoppedExit)
	}
}

func TestJobStopByToolIsAudited(t *testing.T) {
	f := newJobFixture(t,
		jobCall("t1", "bash_background", `{"command":"sleep 60"}`),
		text("started"),
		jobCall("t2", "job_stop", `{"id":1}`),
		text("stopped"))
	ctx := context.Background()
	if _, err := f.ws.Chat().Send(ctx, "start"); err != nil {
		t.Fatal(err)
	}
	if _, err := f.ws.Chat().Send(ctx, "stop it"); err != nil {
		t.Fatal(err)
	}
	// The agent also audits its own job_stop call; the job record is the one that names the job.
	stops, _, err := f.ws.Audit(ctx, "", audit.Query{Tool: "job_stop", Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	var byTool []audit.Record
	for _, r := range stops {
		if strings.Contains(r.InputPreview, "by tool") {
			byTool = append(byTool, r)
		}
	}
	if len(byTool) != 1 {
		t.Fatalf("job_stop records = %+v; want one stop by tool", stops)
	}
}
