package workspace

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"
	"sync"

	"github.com/tinchoram/groot-cli/internal/flow"
	"github.com/tinchoram/groot-cli/internal/tool"
)

// fileLog keeps the paths written by write_file during the current task. It is
// used for the review when the space is not a git repository.
type fileLog struct {
	mu    sync.Mutex
	paths []string
}

func (l *fileLog) add(path string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	for _, p := range l.paths {
		if p == path {
			return
		}
	}
	l.paths = append(l.paths, path)
}

func (l *fileLog) reset() {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.paths = nil
}

func (l *fileLog) list() []string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return append([]string(nil), l.paths...)
}

// trackedTool records the path of every successful write_file.
type trackedTool struct {
	tool.Tool
	log *fileLog
}

func (t trackedTool) Execute(ctx context.Context, input string) (string, bool) {
	out, isErr := t.Tool.Execute(ctx, input)
	if !isErr {
		var in struct {
			Path string `json:"path"`
		}
		if json.Unmarshal([]byte(input), &in) == nil && in.Path != "" {
			t.log.add(in.Path)
		}
	}
	return out, isErr
}

// reviewBox holds the review text of the last delivery until the UI shows it.
type reviewBox struct {
	mu   sync.Mutex
	text string
}

func (b *reviewBox) set(text string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.text = text
}

func (b *reviewBox) take() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	text := b.text
	b.text = ""
	return text
}

// gitDiffStat returns the diff stat of a git work tree. ok is false when the
// directory is not inside a git repository, or git is not installed.
func gitDiffStat(ctx context.Context, space string) (stat string, ok bool) {
	if err := exec.CommandContext(ctx, "git", "-C", space, "rev-parse", "--is-inside-work-tree").Run(); err != nil {
		return "", false
	}
	out, err := exec.CommandContext(ctx, "git", "-C", space, "diff", "--stat").Output()
	if err != nil {
		return "", false
	}
	return strings.TrimRight(string(out), "\n"), true
}

// reviewText is the block shown when work mode delivers a task: what changed,
// the evidence and the agent's note, and how to close the task.
func reviewText(ctx context.Context, t flow.Task, space string, files []string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "review · task #%d · %s · agent %s in %s\n", t.ID, t.Size, t.Agent, space)
	b.WriteString("changes:\n")
	if stat, ok := gitDiffStat(ctx, space); ok {
		if stat == "" {
			b.WriteString("  no uncommitted changes in the repository\n")
		} else {
			b.WriteString(stat + "\n")
		}
	} else if len(files) > 0 {
		for _, f := range files {
			b.WriteString("  written: " + f + "\n")
		}
	} else {
		b.WriteString("  no files were written during this task\n")
	}
	fmt.Fprintf(&b, "evidence: %d records\n", len(t.Evidence))
	if cmd, ok := lastCommand(t); ok {
		b.WriteString("last command: " + cmd + "\n")
	}
	if note := lastDeliverNote(t); note != "" {
		b.WriteString("deliver note: " + note + "\n")
	}
	if t.Size == flow.SizeLarge && len(t.Specs) > 0 {
		b.WriteString("Specs:\n")
		for _, s := range t.Specs {
			b.WriteString("  " + s.Line() + "\n")
		}
	}
	b.WriteString(flow.FormatTestRuns(flow.TestRuns(t.Evidence, flow.CycleStart(t.Evidence))) + "\n")
	b.WriteString("work mode is off. /task accept closes the task; /task reject <reason> sends it back to implement.")
	return b.String()
}

// lastCommand is the first line and the exit status of the newest command evidence.
func lastCommand(t flow.Task) (string, bool) {
	for i := len(t.Evidence) - 1; i >= 0; i-- {
		e := t.Evidence[i]
		if e.Kind != flow.KindCommand {
			continue
		}
		lines := strings.Split(e.Text, "\n")
		head := lines[0]
		status := lines[len(lines)-1]
		return head + " (" + status + ")", true
	}
	return "", false
}

// lastDeliverNote is the newest note recorded in the deliver phase.
func lastDeliverNote(t flow.Task) string {
	for i := len(t.Evidence) - 1; i >= 0; i-- {
		e := t.Evidence[i]
		if e.Kind == flow.KindNote && e.Phase == flow.PhaseDeliver {
			return e.Text
		}
	}
	return ""
}
