package workspace

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/tinchoram/groot-cli/internal/api"
	"github.com/tinchoram/groot-cli/internal/catalog"
	"github.com/tinchoram/groot-cli/internal/jobs"
	"github.com/tinchoram/groot-cli/internal/provider"
)

// jobCall is a model step that calls one tool with the given JSON input.
func jobCall(id, name, input string) provider.Step {
	return provider.Step{Response: api.Response{
		Content:    []api.Block{{Type: api.BlockToolUse, ID: id, Name: name, Input: json.RawMessage(input)}},
		StopReason: api.StopToolUse,
	}}
}

// jobFlow classifies the task and opens it, so the job tools are allowed. Every
// job test starts with it: the flow refuses running commands before a task opens.
func jobFlow() []provider.Step {
	return []provider.Step{
		classifyStep("c_1", "medium", true, "background jobs"),
		toolStep("o_1", "task_open", map[string]string{"goal": "watch jobs", "scope": "space", "done_when": "events seen"}),
	}
}

// newJobFixture is a fixture whose agent "runner" may start and watch jobs.
// The steps follow the flow steps that open the task.
func newJobFixture(t *testing.T, steps ...provider.Step) *fixture {
	t.Helper()
	all := append(jobFlow(), steps...)
	f := newFixture(t, all...)
	tools := []string{
		"bash_background", "monitor", "job_list", "job_output", "job_stop",
		"task_classify", "task_open", "task_advance", "task_verify", "task_status",
	}
	if err := catalog.Save(f.home, catalog.AgentDef{Name: "runner", Description: "runs jobs", Tools: tools}, "You run jobs."); err != nil {
		t.Fatal(err)
	}
	if err := f.ws.reload(); err != nil {
		t.Fatal(err)
	}
	if err := f.spaces.Assign("default", "runner"); err != nil {
		t.Fatal(err)
	}
	if err := f.ws.SelectAgent("runner"); err != nil {
		t.Fatal(err)
	}
	return f
}

// waitEvents waits until n job events wait for the conversation of runner.
func waitEvents(t *testing.T, f *fixture, n int) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if f.ws.WaitingJobEvents(f.ws.ConversationKey()) >= n {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("waiting job events = %d, want %d", f.ws.WaitingJobEvents(f.ws.ConversationKey()), n)
}

// turnText is the user text of the first model call made at or after index from:
// the turn the model saw when a Send started there.
func turnText(f *fixture, from int) string {
	msgs := f.mock.Calls[from].Messages
	return msgs[len(msgs)-1].Content[0].Text
}

// send runs one turn and returns the index of its first model call.
func send(t *testing.T, f *fixture, prompt string) int {
	t.Helper()
	from := len(f.mock.Calls)
	if _, err := f.ws.Chat().Send(context.Background(), prompt); err != nil {
		t.Fatal(err)
	}
	return from
}

func TestJobExitReachesTheNextTurn(t *testing.T) {
	f := newJobFixture(t,
		jobCall("t1", "bash_background", `{"command":"sh -c 'exit 3'"}`),
		text("started"), text("saw it"), text("quiet"))
	send(t, f, "start a job")
	waitEvents(t, f, 1)

	from := send(t, f, "next")
	prompt := turnText(f, from)
	if !strings.Contains(prompt, "Background job events\njob #1 exited with code 3 after ") {
		t.Fatalf("the turn does not carry the exit event:\n%s", prompt)
	}
	if !strings.HasSuffix(prompt, "next") {
		t.Fatalf("the prompt comes last:\n%s", prompt)
	}

	from = send(t, f, "again")
	if strings.Contains(turnText(f, from), "Background job events") {
		t.Fatal("the queue was not cleared after delivery")
	}
}

func TestMonitorDeliversOnlyMatchingLines(t *testing.T) {
	f := newJobFixture(t,
		jobCall("t1", "monitor", `{"command":"printf 'a\\nERROR one\\nb\\nERROR two\\n'","pattern":"ERROR"}`),
		text("watching"), text("seen"))
	send(t, f, "watch")
	waitEvents(t, f, 3) // two matching lines and the exit

	prompt := turnText(f, send(t, f, "go"))
	for _, want := range []string{"job #1: ERROR one", "job #1: ERROR two"} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("missing %q:\n%s", want, prompt)
		}
	}
	for _, not := range []string{"job #1: a", "job #1: b"} {
		if strings.Contains(prompt, not) {
			t.Fatalf("non-matching line %q was delivered:\n%s", not, prompt)
		}
	}
}

func TestJobEventsRedactSecrets(t *testing.T) {
	f := newJobFixture(t,
		jobCall("t1", "monitor", `{"command":"printf 'password=hunter2\\n'"}`),
		text("watching"), text("seen"))
	send(t, f, "watch")
	waitEvents(t, f, 2)

	prompt := turnText(f, send(t, f, "go"))
	if strings.Contains(prompt, "hunter2") || !strings.Contains(prompt, "job #1: ") {
		t.Fatalf("the event is not redacted or not delivered:\n%s", prompt)
	}
}

func TestJobEventBlockFollowsTheInboxAndPrecedesThePrompt(t *testing.T) {
	f := newJobFixture(t,
		jobCall("t1", "bash_background", `{"command":"sh -c 'exit 0'"}`),
		text("started"), text("ok"))
	send(t, f, "start")
	waitEvents(t, f, 1)
	addAgentToDefault(t, f, "writer")
	// A delivered message starts a turn outside work mode too: that turn carries
	// the inbox and the waiting job events, inbox first.
	answered := make(chan struct{}, 1)
	f.ws.opts.Hooks.OnAnswer = func(string, string, error) { answered <- struct{}{} }
	if _, _, err := f.ws.messengerFor("default", "writer").Send(context.Background(), "runner", "note", "body"); err != nil {
		t.Fatal(err)
	}
	select {
	case <-answered:
	case <-time.After(2 * time.Second):
		t.Fatal("the delivered message did not start a turn")
	}

	msgs := f.mock.Calls[len(f.mock.Calls)-1].Messages
	last := msgs[len(msgs)-1].Content[0].Text
	inbox := strings.Index(last, "[inbox] from default/writer")
	events := strings.Index(last, "Background job events")
	if inbox < 0 || events < 0 || inbox > events {
		t.Fatalf("want inbox, then job events:\n%s", last)
	}
}

func TestJobExitInWorkModeStartsATurnAgainstTheLimit(t *testing.T) {
	f := newJobFixture(t,
		jobCall("t1", "bash_background", `{"command":"sh -c 'sleep 0.3'"}`),
		text("started"),
		text("first auto reply"),
		jobCall("t2", "bash_background", `{"command":"sh -c 'sleep 0.3'"}`),
		text("second started"),
		text("never sent"))
	f.ws.opts.Config.Work.MaxAutoTurns = 1
	h := wireTriggers(f)
	if err := f.ws.SetWorkMode("default", "runner", true); err != nil {
		t.Fatal(err)
	}
	conv := f.ws.Chat().(*Conversation)

	send(t, f, "start")
	if got := awaitString(t, h.answers, "answer of the triggered turn"); got != "first auto reply" {
		t.Fatalf("triggered answer = %q", got)
	}
	// The triggered turn is the first call after the first turn's four calls.
	if prompt := turnText(f, 4); !strings.Contains(prompt, "Background job events") || !strings.Contains(prompt, "exited with code 0") {
		t.Fatalf("the triggered turn does not carry the exit:\n%s", prompt)
	}
	if n := conv.autoTurns.Load(); n != 1 {
		t.Fatalf("automatic turns = %d, want 1", n)
	}

	send(t, f, "again")
	awaitWarning(t, h.warnings, "automatic turn limit (1 per session)")
	if n := len(f.mock.Calls); n != 7 {
		t.Fatalf("model calls = %d, want 7: the capped exit made no call", n)
	}
	if n := f.ws.WaitingJobEvents(f.ws.ConversationKey()); n != 1 {
		t.Fatalf("the capped exit waits for the next turn, waiting = %d", n)
	}
}

func TestJobExitWaitsOutsideWorkMode(t *testing.T) {
	f := newJobFixture(t,
		jobCall("t1", "bash_background", `{"command":"sh -c 'exit 0'"}`),
		text("started"), text("never"))
	h := wireTriggers(f)
	send(t, f, "start")
	waitEvents(t, f, 1)

	select {
	case <-h.answers:
		t.Fatal("outside work mode nothing runs by itself")
	case <-time.After(200 * time.Millisecond):
	}
	if n := len(f.mock.Calls); n != 4 {
		t.Fatalf("model calls = %d, want the 4 of the first turn", n)
	}
	if n := f.ws.WaitingJobEvents(f.ws.ConversationKey()); n != 1 {
		t.Fatalf("waiting = %d, want the exit to wait", n)
	}
}

func TestClosingTheWorkspaceStopsRunningJobs(t *testing.T) {
	f := newJobFixture(t,
		jobCall("t1", "bash_background", `{"command":"sleep 60"}`),
		text("started"))
	conv := f.ws.Chat().(*Conversation)
	send(t, f, "start")

	began := time.Now()
	if err := f.ws.CloseFast(); err != nil {
		t.Fatal(err)
	}
	if elapsed := time.Since(began); elapsed > 3*time.Second {
		t.Fatalf("close took %s, want the job stopped within about a second", elapsed)
	}
	if got, ok := conv.jobs.Get(1); !ok || got.State != jobs.Stopped {
		t.Fatalf("job after close = %+v, %v; want stopped", got, ok)
	}
}

func TestJobEventLineIsRedactedAndCut(t *testing.T) {
	long := "password=hunter2 " + strings.Repeat("x", 2000)
	line := jobEventLine(jobs.Event{JobID: 2, Kind: jobs.EventLine, Line: long})
	if strings.Contains(line, "hunter2") {
		t.Fatalf("secret in event line: %q", line)
	}
	if n := len([]rune(line)); n > jobEventMax {
		t.Fatalf("event line is %d characters, want at most %d", n, jobEventMax)
	}
	if got := jobEventLine(jobs.Event{JobID: 3, Kind: jobs.EventCapped}); got != "job #3 reached the 50-line event cap" {
		t.Fatalf("cap line = %q", got)
	}
	got := jobEventLine(jobs.Event{JobID: 4, Kind: jobs.EventExit, ExitCode: 3, Duration: 1234 * time.Millisecond, Command: "make test"})
	if got != "job #4 exited with code 3 after 1.2s: make test" {
		t.Fatalf("exit line = %q", got)
	}
}
