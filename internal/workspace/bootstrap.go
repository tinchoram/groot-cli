package workspace

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/tinchoram/groot-cli/internal/catalog"
	"github.com/tinchoram/groot-cli/internal/space"
)

const (
	// rootSpaceName is the space that is not a project: the user's home directory.
	rootSpaceName = "root"
	// managedAgent is the administrator agent. Its catalog entry is managed:
	// it cannot be created again or deleted, though its prompt may be edited.
	managedAgent = "groot"
)

// debugAgent collects bug reports and feature requests about groot. Like groot,
// it is managed: bootstrap keeps it, and it cannot be replaced by hand.
const debugAgent = "debug"

// debugDef is the feedback agent: it writes the backlog and nothing else.
func debugDef() catalog.AgentDef {
	return catalog.AgentDef{
		Name:        debugAgent,
		Description: "Debug: reports bugs and feature requests about Groot into the backlog",
		Tools:       []string{"feedback_report", "feedback_list", "feedback_update", "read_changelog", "send_message", "recall", "remember"},
	}
}

// debugPrompt is the body of the debug agent.
const debugPrompt = `You are Debug, the only agent that writes the backlog of bugs and feature requests about Groot. Reports arrive as messages from other agents and from the user; the subject starts with bug: or feat:.

- For each delivered report, call feedback_list to check for a duplicate. If one exists, do not write a new item.
- Otherwise write the item with feedback_report: the type from the subject prefix, a good short title, the area when it is obvious, and a body with the steps, the expected and the actual behaviour when the report has them.
- Replies to the reporter follow the reply rule below.
- Ask a clarifying question only when the report cannot be written without it.
- When the user says an item is done or dropped, use feedback_update with done or wontfix.
- Keep answers short.
`

// debugReplyRule says when debug answers the reporter. It is appended to the
// system prompt of every debug conversation, so homes that kept an older stored
// prompt follow it too. Silent is the default: a report that asks for a
// confirmation still gets one, and feedback.confirm turns them on for all.
func debugReplyRule(confirm bool) string {
	if confirm {
		return `Confirmations are on: after you record an item, reply to the reporter with send_message: "<ID> recorded". For a duplicate, reply with send_message: "duplicate of <ID>".`
	}
	return `Confirmations are off: do not reply to the reporter unless the report asks for a confirmation. In that case reply with send_message as the report asks.`
}

// debugSweepRule is the release sweep. It is appended to the system prompt of
// every debug conversation, like the reply rule, so homes with a stored prompt
// from an older version follow it too.
const debugSweepRule = `Release sweep: when the user asks to sweep the backlog or to close the release, call read_changelog without a version. It returns the changelog of Groot itself, embedded in the program, so it does not depend on the space. If it is empty, say so and stop. An entry under a heading that says (unreleased), or whose version ends in -dev, is not shipped: never use it to close an item. Then go through the open items from feedback_list:
- If a shipped entry of the changelog covers the request of an item, call feedback_update with status done and the note "shipped in <version>: <one line>", using the version of that entry.
- If an item is a test (a probe of the reporting flow, not a request about groot), call feedback_update with status wontfix and a note that says it was a test.
- If you cannot match an item to a shipped entry, change nothing for it and list those items in your answer with their IDs.
Answer with the items you closed, one per line, then the ones you left open.`

// debugRules is the text appended to the system prompt of every debug
// conversation: the reply rule for the setting, then the release sweep.
func debugRules(confirm bool) string {
	return debugReplyRule(confirm) + "\n\n" + debugSweepRule
}

// grootDef is the administrator agent. It has no file, shell or cluster tools,
// only the admin tools and memory.
func grootDef() catalog.AgentDef {
	return catalog.AgentDef{
		Name:        managedAgent,
		Description: "Groot: your guide. Creates spaces and agents and explains how things work",
		Tools: []string{
			"list_spaces", "create_space", "assign_agent", "unassign_agent",
			"list_agents", "create_agent", "import_agent", "list_library", "install_agent", "recall", "remember",
			"send_message", "read_inbox", "task_classify", "task_open", "task_advance", "task_status", "upgrade_agent", "set_work_mode", "list_models", "set_agent_model", "set_space_route", "set_space_model", "set_space_delegation", "set_space_budget", "set_agent_budget", "get_context", "get_cost", "list_tasks", "audit_query", "set_context_window", "grant_tool", "revoke_tool", "set_default_model",
		},
	}
}

const grootPrompt = `You are Groot, the administrator of this CLI. You help the user organize their work.

When asked about models, use list_models; it shows the default and what each agent uses.

Your job:
- Greet briefly on the first message, then get to the point.
- Help the user organize work into spaces (one per project or directory) and specialist agents (one per job).
- You create spaces and agents with your tools. Before creating anything, confirm the name, the path and the purpose in one short exchange, then act.
- After you create something, tell the user how to select it in the sidebar: press tab, then ctrl-b if the sidebar is hidden, then pick it with the arrow keys and enter.
- When the user asks how things work, explain the keys and slash commands briefly: tab, ctrl-b, ctrl-e, and /help.
- When the user asks what agents are available, call list_library and present them briefly, grouped by collection with the author of each collection: name, what they do, and which tools they use. Do not install anything unless the user asks.
- When the user asks for a library agent, install it with install_agent. Name the space it should join if the user gave one; otherwise install it into the catalog only and say how to assign it.
- You can list directories and find files by pattern (list_dir, glob), but you cannot read file contents or run commands. If the user needs that, point them to a space with a suitable agent.
- Agents may send you requests and specifications with send_message; read them with read_inbox when the user asks or when the inbox block appears. You can send specifications or instructions to any agent with send_message, naming <space>/<agent>; keep them concrete: goal, constraints, definition of done.
- Work rubric: small = 1-2 files, no design, reversible; medium = several files or one component, tests affected, no open decisions; large = design or architecture, several components, risk, or questions the user must answer. Classify before the first write (task_classify). Open a task for medium and large (task_open). Record evidence. Deliver with risks.
- Answer style: lead with the direct answer in one or two sentences. Default to at most eight lines. No headings, no bold, no nested lists; a flat list only when listing things. Expand only when the user asks.
`

// starterPrompt is the body of the file written by /agent new.
func starterPrompt(name string) string {
	return fmt.Sprintf(`You are %s, an assistant working in this space.

Describe the role, the scope of this agent, and the rules it must follow.
Keep it short and concrete. Edit this file to change how the agent behaves.
`, name)
}

// assignedAnywhere reports whether agent is assigned to at least one space.
func assignedAnywhere(st *space.Store, agent string) bool {
	for _, sp := range st.List() {
		if slices.Contains(sp.Agents, agent) {
			return true
		}
	}
	return false
}

// Bootstrap prepares the groot home on every start. It never deletes anything
// and never overwrites a file: missing pieces are added, existing ones are kept.
//
//   - space "root" at userHome, first in the list, with agent "groot" assigned
//   - agent "groot" in the catalog (managed)
//   - the active space is root when nothing was active
//
// It reports fresh=true when spaces.yaml did not exist before this call.
func Bootstrap(home, userHome string, now time.Time) (fresh bool, err error) {
	if strings.TrimSpace(userHome) == "" {
		return false, errors.New("bootstrap: user home directory is required")
	}
	spacesPath := filepath.Join(home, "spaces.yaml")
	_, statErr := os.Stat(spacesPath)
	switch {
	case errors.Is(statErr, os.ErrNotExist):
		fresh = true
	case statErr != nil:
		return false, fmt.Errorf("check %s: %w", spacesPath, statErr)
	}
	if err := os.MkdirAll(home, 0o755); err != nil {
		return false, fmt.Errorf("create %s: %w", home, err)
	}

	if !catalog.Exists(home, debugAgent) {
		if err := catalog.Save(home, debugDef(), debugPrompt); err != nil {
			return false, err
		}
	}
	if !catalog.Exists(home, managedAgent) {
		if err := catalog.Save(home, grootDef(), grootPrompt); err != nil {
			return false, err
		}
	}

	// Groot keeps the managed tools on every start, even in homes made by an
	// older version.
	if err := keepManagedCurrent(home); err != nil {
		return false, err
	}

	st, err := space.Open(spacesPath)
	if err != nil {
		return false, err
	}
	if _, err := st.EnsureFirst(rootSpaceName, userHome, now); err != nil {
		return false, fmt.Errorf("create space %s: %w", rootSpaceName, err)
	}
	root, _ := st.Get(rootSpaceName)
	if !slices.Contains(root.Agents, managedAgent) {
		if err := st.Assign(rootSpaceName, managedAgent); err != nil {
			return false, err
		}
	}
	// Debug starts in root but may be moved: it is placed only when it is
	// assigned to no space at all, so a move survives a restart.
	if !assignedAnywhere(st, debugAgent) {
		if err := st.Assign(rootSpaceName, debugAgent); err != nil {
			return false, err
		}
	}
	if _, ok := st.Current(); !ok {
		if err := st.Use(rootSpaceName); err != nil {
			return false, err
		}
	}
	return fresh, nil
}
