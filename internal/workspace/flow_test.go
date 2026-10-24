package workspace

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tinchoram/groot-cli/internal/api"
	"github.com/tinchoram/groot-cli/internal/catalog"
	"github.com/tinchoram/groot-cli/internal/flow"
	"github.com/tinchoram/groot-cli/internal/provider"
)

func toolStep(id, name string, input map[string]string) provider.Step {
	in, _ := json.Marshal(input)
	return provider.Step{Response: api.Response{
		Content:    []api.Block{{Type: api.BlockToolUse, ID: id, Name: name, Input: in}},
		StopReason: api.StopToolUse,
	}}
}

// flowAgent gives the fixture's reader agent the tools the flow needs.
func flowAgent(t *testing.T, f *fixture) {
	t.Helper()
	tools := []string{"read_file", "write_file", "bash", "task_classify", "task_open", "task_advance", "task_status"}
	if err := catalog.Save(f.home, catalog.AgentDef{Name: "reader", Description: "builds things", Tools: tools}, "You build things."); err != nil {
		t.Fatal(err)
	}
	if err := f.ws.reload(); err != nil {
		t.Fatal(err)
	}
	if err := f.ws.SelectAgent("reader"); err != nil {
		t.Fatal(err)
	}
}

func lastResult(t *testing.T, msgs []api.Message) api.Block {
	t.Helper()
	last := msgs[len(msgs)-1]
	if len(last.Content) == 0 {
		t.Fatalf("last message has no content: %+v", last)
	}
	return last.Content[0]
}

func TestFlowGatesWritesAndRunsTheLoop(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t,
		toolStep("tu_1", "task_classify", map[string]string{"size": "medium", "reason": "two files"}),
		toolStep("tu_2", "write_file", map[string]string{"path": "a.txt", "content": "x"}),
		toolStep("tu_3", "task_open", map[string]string{"goal": "ship", "scope": "space", "done_when": "tests pass"}),
		toolStep("tu_5", "bash", map[string]string{"command": "echo hi"}),
		text("done"),
		text("second reply"),
	)
	flowAgent(t, f)
	conv := f.ws.Chat()

	if _, err := conv.Send(ctx, "build it"); err != nil {
		t.Fatal(err)
	}
	if got := lastResult(t, f.mock.Calls[2].Messages); !strings.Contains(got.Content, "open a task first (task_open)") {
		t.Fatalf("the write must be refused before the task opens, got %q", got.Content)
	}
	if got := lastResult(t, f.mock.Calls[3].Messages); !strings.Contains(got.Content, "task #1 opened (medium)") {
		t.Fatalf("task_open result = %q", got.Content)
	}
	if got := lastResult(t, f.mock.Calls[4].Messages); !strings.Contains(got.Content, "hi") {
		t.Fatalf("bash result = %q", got.Content)
	}
	if f.mock.Model() != "claude-sonnet-5-5" {
		t.Fatalf("the medium route should switch the model, model = %s", f.mock.Model())
	}

	task, ok := conv.Task()
	if !ok || task.Phase != flow.PhaseImplement {
		t.Fatalf("open task = %+v, %v", task, ok)
	}
	var commands []flow.Evidence
	for _, e := range task.Evidence {
		if e.Kind == flow.KindCommand {
			commands = append(commands, e)
		}
	}
	if len(commands) != 1 || !strings.Contains(commands[0].Text, "$ echo hi") {
		t.Fatalf("bash must be recorded as command evidence, got %+v", commands)
	}

	// The next turn carries the active task in its system prompt.
	if _, err := conv.Send(ctx, "and again"); err != nil {
		t.Fatal(err)
	}
	if sys := f.mock.Calls[5].System; !strings.Contains(sys, "[task #1 · medium · implement]") {
		t.Fatalf("system prompt lacks the task block:\n%s", sys)
	}
	if !strings.Contains(f.mock.Calls[5].System, "You build things.") {
		t.Fatal("the agent's own prompt must stay after the flow block")
	}
}

func TestAdvisoryModeWarnsButLetsTheWriteThrough(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t,
		toolStep("tu_1", "task_classify", map[string]string{"size": "medium", "reason": "x"}),
		toolStep("tu_2", "write_file", map[string]string{"path": "a.txt", "content": "x"}),
		text("done"),
	)
	flowAgent(t, f)
	var warnings []string
	f.ws.opts.Config.Flow.Mode = "advisory"
	f.ws.opts.Hooks.OnWarning = func(key, msg string) { warnings = append(warnings, msg) }
	conv := f.ws.Chat()

	if _, err := conv.Send(ctx, "write a file"); err != nil {
		t.Fatal(err)
	}
	if got := lastResult(t, f.mock.Calls[2].Messages); got.IsError {
		t.Fatalf("advisory mode must not refuse: %q", got.Content)
	}
	if _, err := os.Stat(filepath.Join(f.dir, "a.txt")); err != nil {
		t.Fatalf("the write should have happened: %v", err)
	}
	if len(warnings) == 0 || !strings.Contains(warnings[0], "task_open") {
		t.Fatalf("warnings = %v", warnings)
	}
}
