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
	"space": true, "agent": true, "inbox": true, "task": true, "tasks": true, "diff": true, "work": true, "allow": true, "models": true, "budget": true, "context": true, "cost": true, "bug": true, "feat": true, "backlog": true, "audit": true, "memory": true, "readinbox": true, "mcp": true, "me": true, "jobs": true,
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
	{"/space", "spaces and the agents assigned to them"},
	{"/space list", "list spaces and their agents"},
	{"/space new <name> [path]", "create a space (path defaults to the current directory)"},
	{"/space use <name>", "switch to a space and select its first agent"},
	{"/space assign <agent>", "add a catalog agent to the active space"},
	{"/space unassign <agent>", "remove an agent from the active space"},
	{"/space routes [name]", "show the mode and routes of a space, and where each value comes from"},
	{"/space network [on|off]", "show or set whether the commands of the active space may use the network"},
	{"/space rename <old> <new>", "rename a space; the selection and unread messages follow"},
	{"/space delete <name>", "delete a space from spaces.yaml; its directory and data stay"},
	{"/agent", "the catalog of agents"},
	{"/agent list", "list agents in the catalog"},
	{"/agent new <name> [desc]", "create agents/<name>.md with a starter prompt"},
	{"/agent show [name]", "show an agent's settings and prompt"},
	{"/agent import <path-or-url> [--force]", "import a Claude Code or OpenCode style agent file"},
	{"/agent library", "list the curated agent library"},
	{"/agent install <name> [space] [--force]", "install a library agent, optionally into a space"},
	{"/agent tools <name> add|remove <tool>[,<tool>…]", "grant or revoke the tools of an agent"},
	{"/agent upgrade <name>|--all", "add the tools that built-in agents gained since they were installed"},
	{"/agent rename <old> <new>", "rename an agent everywhere it is assigned; managed agents keep their name"},
	{"/agent delete <name>", "delete an agent and unassign it from every space; its memory stays"},
	{"/inbox", "show the last 10 messages of this agent"},
	{"/inbox send <to> <subject> -- <body>", "send a message by hand"},
	{"/readinbox", "have this agent read its unread messages in one turn"},
	{"/memory", "the memory entries in this agent's prompt, and their counts"},
	{"/memory <query>", "search the memory this agent can read"},
	{"/memory show|pin|unpin|rm|reviewed <id>", "show one entry in full, pin or unpin it, forget it, or mark it reviewed"},
	{"/memory review", "list the entries whose review date has passed"},
	{"/model [name]", "show or change the model"},
	{"/model --all <id> [--force]", "set every agent of the active space to a model, and the space default"},
	{"/models", "list the models the provider offers, with their prices"},
	{"/cost", "session cost, spend and budgets"},
	{"/budget", "show the spend and budgets of the space and its agents"},
	{"/budget space <usd>", "set the budget of the active space (0 clears it)"},
	{"/budget agent <name> <usd>", "set the budget of an agent (0 clears it)"},
	{"/context", "context fill of each agent in the active space"},
	{"/context all", "context fill of the agents of every space"},
	{"/context window <model> <tokens>", "context window of a model, saved in groot.yaml"},
	{"/task", "show the status of the open task"},
	{"/task accept", "accept the task's result"},
	{"/task reject <reason>", "reject the task's result, with the reason"},
	{"/task fail <reason>", "fail the task, with the reason"},
	{"/task assign #N <agent>", "hand the open task to an agent of the space; the owner keeps it"},
	{"/tasks [space]", "open tasks of every space, or of one space"},
	{"/tasks all", "also the done and failed tasks of the last 7 days"},
	{"/jobs", "background jobs of this agent: state, exit code, start and command"},
	{"/jobs stop <id>", "stop one background job of this agent"},
	{"/mcp", "MCP servers of this space: state, and the tools each agent holds"},
	{"/mcp connect <name>", "connect an MCP server now and report the tools it admits"},
	{"/audit [n]", "last audit records of the active space (20 by default)"},
	{"/audit denied|guard|errors", "only denied calls, calls with guard findings, or failed calls"},
	{"/audit tool <name> | agent <name>", "only one tool, or the calls of one agent"},
	{"/audit since <2h|1d|time>", "only records from that time on (until <time> ends it)"},
	{"/audit find <text>", "records whose input preview contains the text"},
	{"/audit all [filters]", "the same across every space, with a SPACE column"},
	{"/audit stats [filters]", "counts by decision and risk, and the export sinks"},
	{"/work [on|off]", "show, turn on or turn off work mode"},
	{"/work on", "turn work mode on"},
	{"/work off", "turn work mode off"},
	{"/allow", "show the approvals kept for this session"},
	{"/allow clear", "drop the approvals kept for this session"},
	{"/diff", "show the full detail of the pending approval"},
	{"/bug <text>", "report a bug about groot into the backlog"},
	{"/feat <text>", "request a feature for groot into the backlog"},
	{"/backlog [open|done|all]", "list the backlog: open items by default"},
	{"/compact", "summarize older history now"},
	{"/tokens", "token usage and estimated cost"},
	{"/clear", "clear the conversation (memory keeps its records)"},
	{"/me [name]", "show your name, or set it: agents mention you as @name"},
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
