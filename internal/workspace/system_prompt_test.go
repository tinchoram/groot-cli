package workspace

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/tinchoram/groot-cli/internal/clock"
)

func TestSystemPromptCarriesTheEnvironmentOnce(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t, text("one"), text("two"))
	_ = f.ws.SelectAgent("reader")
	conv := f.ws.Chat()
	if _, err := conv.Send(ctx, "hi"); err != nil {
		t.Fatal(err)
	}
	if _, err := conv.Send(ctx, "again"); err != nil {
		t.Fatal(err)
	}
	sys := f.mock.Calls[0].System
	for _, want := range []string{
		"## Environment", "- Working directory: " + f.dir, "- OS: ", "- Shell: ", "- Date: ", "- AGENTS.md: loaded",
	} {
		if !strings.Contains(sys, want) {
			t.Errorf("system prompt misses %q:\n%s", want, sys)
		}
	}
	if strings.Contains(sys, "- Git branch:") {
		t.Error("a directory that is not a git work tree has no git lines")
	}
	if strings.Index(sys, "## Environment") > strings.Index(sys, "## Project context") {
		t.Error("the environment comes before the project context")
	}
}

func TestSystemPromptStartsWithTheBaseThenTheAgentPrompt(t *testing.T) {
	f := newFixture(t, text("ok"))
	_ = f.ws.SelectAgent("reader")
	if _, err := f.ws.Chat().Send(context.Background(), "hi"); err != nil {
		t.Fatal(err)
	}
	sys := f.mock.Calls[0].System
	base := strings.Index(sys, "You are Groot")
	agent := strings.Index(sys, "You read files carefully.")
	if base < 0 || agent < 0 || base > agent {
		t.Fatalf("the configured base must come before the agent prompt:\n%s", sys)
	}
	if strings.Contains(sys, "Long-term memory") {
		t.Fatal("memory is not part of the system prompt")
	}
}

func TestConfiguredSystemPromptReplacesOnlyTheBase(t *testing.T) {
	f := newFixture(t, text("ok"))
	f.ws.opts.Config.SystemPrompt = "You are a custom SRE assistant."
	_ = f.ws.SelectAgent("reader")
	if _, err := f.ws.Chat().Send(context.Background(), "hi"); err != nil {
		t.Fatal(err)
	}
	sys := f.mock.Calls[0].System
	if strings.Contains(sys, "You are Groot") || !strings.Contains(sys, "You are a custom SRE assistant.") || !strings.Contains(sys, "You read files carefully.") {
		t.Fatalf("a custom system_prompt replaces the default base and keeps the agent prompt:\n%s", sys)
	}
}

func TestSystemPromptIsByteEqualWhileTheClockAndTheTaskMove(t *testing.T) {
	f := newFixture(t,
		classifyStep("tu_1", "medium", true, "two files"),
		toolStep("tu_2", "task_open", map[string]string{"goal": "ship", "scope": "space", "done_when": "tests pass"}),
		text("opened"),
		toolStep("tu_3", "write_file", map[string]string{"path": "a.txt", "content": "x"}),
		toolStep("tu_4", "bash", map[string]string{"command": "echo hi"}),
		toolStep("tu_5", "task_advance", map[string]string{"phase": "verify"}),
		text("verifying"),
		text("third"),
	)
	flowAgent(t, f)
	now := t0
	f.ws.opts.Now = func() time.Time { return now }
	conv := f.ws.Chat()

	if _, err := conv.Send(context.Background(), "build it"); err != nil {
		t.Fatal(err)
	}
	now = now.Add(time.Minute)
	if _, err := conv.Send(context.Background(), "continue"); err != nil {
		t.Fatal(err)
	}
	now = now.Add(time.Minute)
	if _, err := conv.Send(context.Background(), "what next"); err != nil {
		t.Fatal(err)
	}

	first, last := f.mock.Calls[0].System, f.mock.Calls[7].System
	if first != last {
		t.Fatalf("the system prompt must not change across turns:\nfirst:\n%s\nlast:\n%s", first, last)
	}
	for _, banned := range []string{"Current time:", "[task #", "[classified"} {
		if strings.Contains(last, banned) {
			t.Errorf("the system prompt carries a per-turn line %q", banned)
		}
	}

	// The third turn's user message opens with the time of that turn and the
	// task block as it stands then: the phase advanced in the second turn.
	turn3 := lastUserText(f.mock.Calls[7])
	wantTime := "Current time: " + strings.TrimPrefix(clock.PromptLine(t0.Add(2*time.Minute), time.Local), "Current time: ")
	if !strings.HasPrefix(turn3, wantTime) {
		t.Fatalf("the user turn must open with the time line %q, got:\n%s", wantTime, turn3)
	}
	if !strings.Contains(turn3, "[task #1 · medium · verify]") {
		t.Fatalf("the user turn must carry the task block of its turn, got:\n%s", turn3)
	}
	if turn1 := lastUserText(f.mock.Calls[0]); !strings.HasPrefix(turn1, "Current time: ") || strings.Contains(turn1, "[task #") {
		t.Fatalf("the first turn opens with the time and has no task yet, got:\n%s", turn1)
	}
}
