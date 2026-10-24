package ui

import (
	"fmt"
	"strings"

	"github.com/tinchoram/groot-cli/internal/api"
)

// Command is a parsed slash command such as "/model claude-haiku-5-5".
type Command struct {
	Name string
	Arg  string
}

var knownCommands = map[string]bool{
	"help": true, "model": true, "compact": true, "tokens": true, "clear": true, "exit": true,
	"space": true, "agent": true, "inbox": true, "task": true, "tasks": true, "diff": true, "work": true, "allow": true, "models": true, "budget": true, "context": true, "cost": true, "bug": true, "feat": true, "backlog": true,
}

// ParseCommand reports whether line is a slash command and splits it into name and argument.
func ParseCommand(line string) (Command, bool) {
	s := strings.TrimSpace(line)
	if !strings.HasPrefix(s, "/") {
		return Command{}, false
	}
	parts := strings.SplitN(strings.TrimPrefix(s, "/"), " ", 2)
	cmd := Command{Name: parts[0]}
	if len(parts) == 2 {
		cmd.Arg = strings.TrimSpace(parts[1])
	}
	return cmd, true
}

// Known reports whether the command is one the UI implements.
func (c Command) Known() bool { return knownCommands[c.Name] }

// helpEntry is one line of the /help listing.
type helpEntry struct {
	usage       string
	description string
}

var helpEntries = []helpEntry{
	{"/help", "show this list"},
	{"/space list", "list spaces and their agents"},
	{"/space new <name> [path]", "create a space (path defaults to the current directory)"},
	{"/space use <name>", "switch to a space and select its first agent"},
	{"/space assign <agent>", "add a catalog agent to the active space"},
	{"/space unassign <agent>", "remove an agent from the active space"},
	{"/space routes [name]", "show the mode and routes of a space, and where each value comes from"},
	{"/agent list", "list agents in the catalog"},
	{"/agent new <name> [desc]", "create agents/<name>.md with a starter prompt"},
	{"/agent show [name]", "show an agent's settings and prompt"},
	{"/agent import <path-or-url> [--force]", "import a Claude Code or OpenCode style agent file"},
	{"/agent library", "list the curated agent library"},
	{"/agent install <name> [space] [--force]", "install a library agent, optionally into a space"},
	{"/inbox", "show the last 10 messages of this agent"},
	{"/inbox send <to> <subject> -- <body>", "send a message by hand"},
	{"/model [name]", "show or change the model"},
	{"/budget", "show the spend and budgets of the space and its agents"},
	{"/budget space <usd>", "set the budget of the active space (0 clears it)"},
	{"/budget agent <name> <usd>", "set the budget of an agent (0 clears it)"},
	{"/context", "context fill of each agent in the active space"},
	{"/context all", "context fill of the agents of every space"},
	{"/context window <model> <tokens>", "context window of a model, saved in groot.yaml"},
	{"/tasks [space] [all]", "open tasks of every space, or of one space; all adds the done and failed tasks of the last 7 days"},
	{"/compact", "summarize older history now"},
	{"/tokens", "token usage and estimated cost"},
	{"/clear", "clear the conversation (memory keeps its records)"},
	{"/exit", "end the session and quit"},
}

// helpBlock renders the /help listing in the reference layout: a dim header,
// then one usage column and one dim description column per command.
func helpBlock() string {
	var sb strings.Builder
	sb.WriteString(styleDim.Render("available commands:") + "\n")
	for _, e := range helpEntries {
		fmt.Fprintf(&sb, "  %-18s %s\n", e.usage, styleDim.Render(e.description))
	}
	sb.WriteString(styleDim.Render("writes and commands ask for approval: y allows, n denies"))
	return sb.String()
}

// usageBlock renders session token counts and the estimated cost. Values are
// in the accent style; labels are plain.
func usageBlock(u api.Usage, costUSD float64) string {
	rows := []struct {
		label string
		value string
	}{
		{"input", formatThousands(int(u.InputTokens))},
		{"output", formatThousands(int(u.OutputTokens))},
		{"cache write", formatThousands(int(u.CacheCreationTokens))},
		{"cache read", formatThousands(int(u.CacheReadTokens))},
		{"est. cost", fmt.Sprintf("$%.4f", costUSD)},
	}
	var sb strings.Builder
	sb.WriteString(styleDim.Render("session usage:") + "\n")
	for _, r := range rows {
		fmt.Fprintf(&sb, "  %-15s %s\n", r.label, styleAccentBold.Render(r.value))
	}
	return strings.TrimRight(sb.String(), "\n")
}

// compactionNotice reports how many messages a compaction removed.
func compactionNotice(before, after int) string {
	return styleDim.Render(fmt.Sprintf("compacted: %d → %d messages", before, after))
}

// statusUsage formats the idle footer's usage part. It returns "" before the
// first model call.
func statusUsage(u api.Usage, costUSD float64, showCost bool) string {
	if u.InputTokens == 0 && u.OutputTokens == 0 {
		return ""
	}
	parts := []string{
		formatThousands(int(u.InputTokens)) + " in",
		formatThousands(int(u.OutputTokens)) + " out",
	}
	if u.CacheCreationTokens > 0 || u.CacheReadTokens > 0 {
		parts = append(parts, fmt.Sprintf("cache %s/%s",
			formatThousands(int(u.CacheCreationTokens)), formatThousands(int(u.CacheReadTokens))))
	}
	if showCost {
		parts = append(parts, fmt.Sprintf("~$%.4f", costUSD))
	}
	return strings.Join(parts, " · ")
}

// formatThousands inserts thousands separators into n.
func formatThousands(n int) string {
	if n < 0 {
		return "-" + formatThousands(-n)
	}
	if n < 1000 {
		return fmt.Sprintf("%d", n)
	}
	return formatThousands(n/1000) + fmt.Sprintf(",%03d", n%1000)
}
