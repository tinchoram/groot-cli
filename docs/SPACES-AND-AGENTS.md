# Spaces and agents

Spaces, agent files, tools, identity, the agent library, and upgrades.

## Spaces and agents

Since 0.2.0 groot works in **spaces**, and inside a space you talk to **agents**.

- **Space**: a named working directory (`name`, absolute `path`) with the list of
  agents assigned to it. Tools run inside the space path: file tools are confined
  to it and `bash` starts there. By default each space keeps its memory and audit
  log in `<path>/.groot/` (see [Memory](MEMORY.md)).
- **Agent**: a reusable definition in the global catalog: a name, a description,
  an optional model, a tool subset, an optional turn limit, and a system prompt.
  Each (space, agent) pair runs its own conversation with its own history, and
  each conversation writes its own session row.
- **Memory**: each agent's long-term memory, with scopes that decide who sees
  what. See [Memory](MEMORY.md).
- **Summary on exit**: when groot quits, every conversation that exchanged
  messages writes a session summary (goal, done, decisions, next, files) to its space's memory.

### Deleting, renaming, moving and copying

Groot does these with six admin tools, each a write that you confirm inline.
The same operations are shortcuts in the UI.

- `delete_space {name, confirm?, purge_data?}`, `/space delete <name>`: removes
  the space from `spaces.yaml`. The first call, without `confirm: true`, changes
  nothing and reports the agents assigned and the open tasks per agent; call
  again with `confirm: true` to delete. The space's directory stays on disk, and
  its `.groot` data too unless `purge_data: true` (honoured only with `confirm`):
  that deletes `<space dir>/.groot`, the memory database and the audit log, and
  nothing else. A purge refuses the home directory and a directory another space
  uses. `root` cannot be deleted. Deleting the active space switches to `root`.
- `edit_space {name, new_name?, path?}`, `/space rename <old> <new>`: renames a
  space or points it at another directory. `root` cannot be renamed. A rename
  keeps the selection and the unread messages; a new path does not move the
  `.groot` data under the old path.
- `delete_agent {name, confirm?}`, `/agent delete <name>`: removes the agent from
  the catalog and from every space. The first call, without `confirm: true`,
  changes nothing and reports the spaces it is assigned to with the open tasks
  per space. Its memory and sessions stay in each space's database. `groot` cannot
  be deleted. An installed `debug` can be; its backlog files stay.
- `edit_agent {name, description?, prompt?, new_name?}`, `/agent rename <old> <new>`:
  changes the description or the prompt, or renames the agent everywhere it is
  assigned (assignments, routes, inbox, tasks, memory and sessions). Managed
  agents keep their name.
- `move_agent {agent, from, to, with_memory?}`: moves an agent between two spaces
  in one save. With `with_memory: true` it also copies the agent's memory,
  sessions (with their messages) and tasks (with their evidence) into the target
  space's database, under new task ids. The source keeps its copies, and the
  result says so. Sessions the target already has are skipped.
- `copy_agent {agent, new_name, space?}`: copies the description, prompt, model,
  tools and `max_turns` under a new name, optionally assigned to a space.

Every result is the state read back from `spaces.yaml` and the catalog.

### Files under `~/.groot`

```
~/.groot/
  groot.yaml               global configuration (optional)
  user.yaml                your name, written by the first-start prompt and /me
  spaces.yaml              spaces, the active space, assigned agents
  agents/<name>.md         one agent: frontmatter for the settings, body for the prompt
<space path>/.groot/
  groot.db                 memory and sessions for this space
  audit.jsonl              one line per tool call in this space
```

`GROOT_HOME` replaces `~/.groot`; tests use it. On the first run, groot creates
the space `root` at your home directory and the agent `groot`, and assigns
`groot` to `root`. Existing files are never overwritten.

A space can override two global settings in `spaces.yaml`. Both are optional,
and a file without them works as before:

- `model`: the default model of the space's agents that set no model of their own.
- `flow`: `mode` (`strict` or `advisory`) and `routes` for `small`, `medium` and
  `large`, each with `model` and `agent`. Each field falls back to the global
  value in `groot.yaml`, then to the built-in default.

```yaml
spaces:
  - name: payments
    path: /work/payments
    agents: [reviewer]
    model: claude-sonnet-5-5
    flow:
      mode: advisory
      routes:
        large:
          model: claude-opus-5-5
```

Groot's admin agent can set these with `set_space_route`, `set_space_model` and
`set_space_delegation` (see [Delegation](FLOW.md#delegation)),
and `/space routes [name]` shows the effective values and where each one comes
from.

### Commands

```
/space list                      list spaces and their agents
/space new <name> [path]         create a space (path defaults to the current directory)
/space use <name>                switch to a space and select its first agent
/space assign <agent>            add a catalog agent to the active space
/space unassign <agent>          remove an agent from the active space
/space routes [name]             show the mode, default model and routes of a space, with where each value comes from
/space rename <old> <new>        rename a space; the selection and unread messages follow
/space delete <name>             delete a space from spaces.yaml; its directory and data stay
/agent rename <old> <new>        rename an agent everywhere it is assigned
/agent delete <name>             delete an agent and unassign it from every space; its memory stays
/budget [space|agent ...]        show or set spend budgets (see Budgets)
/cost                            session cost, spend and budgets (see Budgets)
/context [all|window <model> <tokens>]  context fill per agent (see Context)
/tasks [space] [all]             open tasks across spaces; all adds done and failed of the last 7 days (see ODD flow)
/mcp                             MCP servers of this space: state, and the tools each agent holds (see MCP servers)
/mcp connect <name>              connect one MCP server now and report the tools it admits
/audit [n|all|stats] [filters]   the audit records of the active space, or of all spaces (see Audit)
/agent list                      list the catalog (✓ = assigned to the active space)
/agent new <name> [description]  create agents/<name>.md with a starter prompt
/agent import <path-or-url> [--force]  import an agent file (see below)
/agent show [name]               show an agent's settings and the first prompt line
/me [name]                       show your name, or set it: agents mention you as @name
```

To make a new agent: `/agent new reviewer "reviews diffs"`, edit the
file at the path it prints, then `/space assign reviewer`.

### Agent files

Each agent is one Markdown file, `~/.groot/agents/<name>.md`. The frontmatter
holds the settings; the body is the system prompt:

```markdown
---
name: groot
description: "Groot: your guide. Creates spaces and agents"
model: sonnet                  # optional: a full id, or opus | sonnet | haiku | inherit
effort: high                   # optional: low | medium | high | xhigh | max; overrides provider.effort
tools: [list_spaces, recall]   # optional: a list or a comma-separated string
max_turns: 40                  # optional
---
You are Groot, the administrator of this CLI.
```

- `name` must equal the file name without `.md`. Other frontmatter keys are
  ignored. CRLF line endings are accepted.
- Model aliases: `opus` is `claude-opus-5-5`, `sonnet` is `claude-sonnet-5-5`,
  `haiku` is `claude-haiku-5-5`. `inherit` or an empty value uses the default
  model from `groot.yaml`.
- `effort` is optional and unset by default. When set, it is sent with every model
  call of the agent and overrides `provider.effort`; when unset, the provider's
  value applies (and none is sent if that is unset too). `/agent show` names it.
- Tools must be groot tool names: `read_file`, `list_dir`, `glob`, `write_file`,
  `bash`, `kubectl`, `recall`, `remember`, and the admin tools. Files that predate this format
  (`agents/<name>/agent.yaml` and `prompt.md`) are converted on the next start,
  and their folder is removed once the new file is written and read back.

### Granting tools

Agents get tools explicitly, one at a time. New agents get the default set.
`/agent upgrade` adds only the base set (`send_message`, `read_inbox`, the task
tools, `recall`, `remember`, and the read-only `list_dir` and `glob`). Every other tool, including `fetch_url`, is
granted on purpose:

- `/agent tools <name> add <tool>[,<tool>…]` adds tools, and
  `/agent tools <name> remove <tool>…` takes them away. Both accept commas, spaces
  or both. Unknown names are refused and nothing is saved.
- `/agent show [name]` lists an agent's tools.
- Groot does the same with `grant_tool {agent, tool}` and `revoke_tool {agent,
  tool}`. Both always ask for your approval, and the session approval `a` never
  covers them.
- The managed agents `groot` and `debug` (once installed) keep their required tools:
  removing one is refused. Groot can gain tools but never lose its admin tools.

Changes are saved in the agent's file and apply right away, including open conversations, which keep their session.

MCP tools are granted the same way, by their model-facing name `<server>__<tool>`,
or `<server>__*` for every allowlisted tool of a server. The server must be
configured (see [MCP servers](INTEGRATIONS.md#mcp-servers)). Groot never grants MCP tools by itself.

### Importing agents

`/agent import <path-or-url> [--force]` reads an agent file from disk or from an
http(s) URL, the Markdown-with-frontmatter style used by Claude Code and
OpenCode. The administrator agent can do the same with `import_agent`, after
showing you a preview.

| Source tool | groot tool |
|---|---|
| `Read` | `read_file` |
| `Write`, `Edit`, `MultiEdit` | `write_file` |
| `Bash` | `bash` |
| `Grep`, `Glob` | dropped, with a warning: no equivalent yet |
| groot tool names | kept |
| anything else | dropped, with a warning |

When a file declares no tools, or none of them has an equivalent, groot uses
`read_file`, `write_file`, `bash`, `recall`, `remember`. An existing agent with
the same name is replaced only with `--force` (or `overwrite: true` for the
tool). `groot` can never be replaced.

Limits: 256 KB per file, text only, and 10 seconds for a URL. Imported files
keep their own licence; check it before you share or redistribute them. The
import is written to your catalog as is, so read the prompt before you assign
the agent to a space.

## Identity and mentions

Agents address you and each other with `@`. Three names matter: yours, `@groot`
(the coordinator), and every agent as `name` or `space/name`.

- **Your name.** On the first start, with no name set, the input asks "How should
  agents call you? (one word, e.g. Tinchoram)". Enter saves it; esc skips it for
  this session, and the next start asks again. A name is one word of 2 to 32
  letters, digits, `_` or `-`, with or without the `@`. Change it any time with
  `/me <name>`, and `/me` alone shows it. It is stored in `~/.groot/user.yaml` and
  takes effect at once: the banner and the prompts of open conversations change
  without a restart. `user.name` in `~/.groot/groot.yaml` works too, and `user.yaml`
  overrides it. A project's `groot.yaml` cannot set it.
- **Banner.** The line under the wordmark shows `operated by: @<name>` next to the
  version. It is hidden when no name is set.
- **Autocomplete.** Type `@` and a list appears under the input: the agents of the
  current space first as `name`, then the agents of other spaces as `space/name`.
  An agent held by several spaces is always shown qualified. Typing filters by
  prefix, ignoring case. Up and down choose, `tab` or `enter` inserts the address
  and a space, `esc` closes the list. The list does not open in a `/` command.
- **Routing.** A message that starts with `@name` or `@space/name` goes to that
  agent: groot switches to its space when needed, opens its conversation and sends
  the rest. A bare `@name` only switches. An unknown name sends nothing and prints
  `no agent @x; agents: …`. A name held by several spaces needs the space when the
  current space does not hold it.
- **Mentions in text.** Other mentions stay in the text. The agent also gets a
  line with the agents it names, in full, so it can `send_message` to them:
  `[mentions: explore/search-specialist]`.
- **What you see.** `@<your name>` and `@groot` are highlighted in agent replies.
  When another conversation's reply mentions you, its agent in the sidebar and
  the `@n` in the status line show an `@` marker. Opening the conversation or
  writing to it clears the marker.
- **What agents know.** Every prompt has a People section. It names you, tells the
  agent to mention you only when it needs a decision, approval or information, and
  says that `@groot` is reached with `send_message`, not by a mention alone.

## Agent library

groot's library holds agents you can install into your catalog with one command.
It has three parts: your own collection, public catalogs that have been
validated, and extra sources you name in `groot.yaml`.

```
/agent library                                 list everything, grouped by collection
/agent library VoltAgent awesome-claude-code-subagents   list one source only
/agent show VoltAgent awesome-claude-code-subagents/devops-engineer
/agent install VoltAgent awesome-claude-code-subagents/devops-engineer devops
/agent install developer payments --force      replace an existing developer, assign to payments
```

Ask groot ("what agents can I install?") and it will list them and install on
request.

### Your collection: @tinchoram

Seven agents ship embedded in the binary, all in the collection `@tinchoram`:

| Agent | Purpose | Tools |
|---|---|---|
| `debug` | keeps the groot backlog: bug reports and feature requests about groot | feedback_report, feedback_list, feedback_update, read_changelog, send_message, recall, remember |
| `space-manager` | space manager: context, phases, backlog and docs/ structure, delegation to the space's agents, short status | send_message, read_inbox, read_file, write_file, list_dir, glob, recall, remember, memory_get, update_memory, forget, task_classify, task_open, task_advance, task_status, task_verify, feedback_list, fetch_url |
| `ux-designer` | flows, ASCII wireframes, components, accessibility checks | read_file, write_file, recall, remember |
| `developer` | test-first engineering with small diffs and explained trade-offs | read_file, write_file, bash, recall, remember |
| `security-tester` | authorized-scope security testing with reproducible findings | read_file, bash, recall, remember |
| `devsecops-platform` | cloud and cluster platform design, IaC, pipeline hardening | read_file, write_file, bash, kubectl, recall, remember |
| `observability` | SLOs, dashboards, alert quality, incident timelines, runbooks | read_file, kubectl, bash, recall, remember |

They leave the model unset, so they use the default from `groot.yaml`.
`security-tester` works only against targets you name in the conversation.

### Validated public sources

`library/sources.yaml` lists public catalogs that someone reviewed before they were
added. An entry says where the agents are, under what license, and who validated
it:

```yaml
sources:
  - name: "VoltAgent awesome-claude-code-subagents"
    kind: github
    repo: VoltAgent/awesome-claude-code-subagents
    ref: main
    path: categories
    license: MIT
    validated_by: "@tinchoram"
```

- `kind: github` lists the `.md` files under `path` in the repository, through the
  GitHub tree API. Each file's description is read only when you `show` or
  `install` it, and the category is the folder the file sits in.
- `kind: index` names an `index.yaml` in the format of `library/index.yaml`.
- "Validated" means that someone checked the license, opened several of the
  agents, confirmed that they import cleanly (tool names mapped as described
  in "Importing agents"), and checked that nothing in them is harmful.
  Validation records a review of that moment, not a guarantee for later
  changes in the source.

To add a source, open a pull request that appends an entry to
`library/sources.yaml` with the name, the kind, the license, and your name in
`validated_by`. The source name must not contain a slash.

A public agent is downloaded and checked like an import when you install it, and
it keeps the name of its file. Imported files keep their own license; check it
before you share or redistribute them. The gate preview of `install_agent` shows
the source URL, and the warnings of the import mapping, before you approve.

If a source cannot be read (network down, a rate limit, a broken index), the
listing shows a warning for it and the rest still works. Each source is read at
most once per process.

### Your extra sources

Name remote `index.yaml` sources in `groot.yaml`. They use the same format as the
`index` kind above and are listed with the collection and author the index gives:

```yaml
library:
  sources:
    - name: "@ana"
      url: https://example.com/ana/index.yaml
```

An index is at most 64 KB. Builtin and local agents of the same name take
precedence over index agents.

### Your own files

Put Markdown agent files in `~/.groot/library/` (the same format as the agent files
in `~/.groot/agents/`). Those files are listed with the collection `local`. A file
there with the same name as a builtin replaces that builtin in the library list.
Installing copies a file into the catalog and never changes the library.

### Curating the builtin set

The builtin files live in `library/agents/`, embedded at build time. To change one,
edit its file, keep the frontmatter name equal to the file name, keep the prompt
between 25 and 50 lines, and keep `library/index.yaml` in step. Then run
`make test`: the library tests check that every file loads and validates, and that
the index matches the embedded files.

### Existing homes

Groot answers in a fixed style: lead with the direct answer in one or two sentences, default to at most eight lines, no headings, no bold, no nested lists (a flat list only when listing things), and expand only when asked.

A fresh home gets groot's prompt and tools, including `list_library` and
`install_agent`. Bootstrap keeps an existing `groot.md`'s prompt and tools and adds the managed ones, so an older home
keeps its own prompt. To give an older groot the library tools, add them to the
`tools` list in `~/.groot/agents/groot.md`, and add the paragraph about the
library to its prompt. Nothing else changes.

## Upgrading agents

Agents carry a managed tool set: `send_message`, `read_inbox`, the four `task_*`
tools, `recall` and `remember`. Groot gets these tools on every start, even in an
existing home, and keeps its prompt and any other tools.

Other agents keep their files as they are. To add the managed tools that an agent
lacks, run `/agent upgrade <name>` or `/agent upgrade --all`. Upgrades never
remove a tool. The admin tool `upgrade_agent` does the same for groot to call.
Groot itself is not upgraded by hand.
