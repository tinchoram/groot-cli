# Changelog

All notable changes to Groot CLI. Versions follow semver; the first three were
built in one day of live testing, so they are grouped by what shipped.

## 0.4.0-dev (unreleased)

### Spaces, routes and models
- Routes and models per space. A space in `spaces.yaml` can set `flow.mode`,
  `flow.routes.{small,medium,large}` (`model`, `agent`) and a default `model`.
  Each field falls back to `groot.yaml`, then to the built-in default.
- Model of a conversation: the agent's own model, then the space's, then
  `provider.model`.
- `set_space_route` and `set_space_model` admin tools, granted to `groot` and
  listed with the other admin changes that work mode still asks about.
- `/space routes [name]` shows the effective mode, default model and routes of a
  space, and where each value comes from (`space`, `global` or `default`).
- The override is read when a conversation opens. Running conversations keep the
  settings they started with; restart groot to apply a change to them.

### Budgets
- `budget_usd` on a space (`spaces.yaml`) and on an agent (frontmatter). Zero or
  a missing value means no budget.
- The spend of a session is stored after every model answer, so a crash keeps it.
  Budgets sum the stored spend per agent and per space. `sessions.cost_usd` holds
  the session's own spend: a session ended by `/clear` used to store the
  conversation's running total.
- A turn is refused, with no model call, when a budget is spent. A turn that may
  pass a limit warns once and runs. The check runs again before each model call
  in a turn.
- The sidebar shows spend after each space and agent, amber from 80% and red at
  the budget.
- `/budget`, `/budget space <usd>` and `/budget agent <name> <usd>`. Admin tools
  `set_space_budget` and `set_agent_budget`, granted to `groot`.
- Not yet: a global default in `groot.yaml`.

## 0.3.0 — 2026-10-24

Coordination, control and day-to-day ergonomics.

### Flow and control
- ODD task flow (`internal/flow`): sizes `small`, `medium`, `large`; tasks with
  phases, evidence and a state machine enforced in Go; `strict` (writes denied
  without a task) and `advisory` modes; per-size model routes (Haiku, Sonnet,
  Opus) applied when a task opens and restored when it closes; bash output
  recorded as evidence automatically; `[task …]` block injected every turn;
  `/task`, `/task fail`, `/task accept`, `/task reject`.
- Work mode (`/work on|off`, `set_work_mode`): auto-approval of every write
  except `work.always_ask` commands, admin/catalog tools and writes outside the
  space; turn and USD limits; review block on deliver; unmistakable UI (tan
  input border, `[auto]` lines, `⚙`).
- Session grants: approvals are `y / n / a`; `a` allows that tool for the rest
  of the session (`/allow`, `/allow clear`).
- Inline approvals only; the diff modal is gone (`/diff`, `d`).
- Read-only shell classifier: pipelines made of read-only commands run without
  a prompt; redirections, `sudo`, `rm`, `kubectl delete`… still ask.

### Agents and messaging
- Inbox between agents (`send_message`, `read_inbox`, `/inbox`): unread
  messages are injected into the next turn; in work mode a message starts a
  background turn (capped by `work.max_auto_turns`); sidebar badges per agent
  and per space.
- Background conversations: switching space or agent is always allowed; each
  conversation streams into its own transcript; approvals wait in the
  background (`?`), working agents show a spinner, spaces show `●`.
- Managed agents `groot` (administrator) and `debug` (backlog writer); their
  tool sets are kept current at every start; `/agent upgrade` adds the base set
  to installed agents; `/agent tools <name> add|remove`, `grant_tool`,
  `revoke_tool` for least-privilege grants.
- Agent library: the `@tinchoram` collection embedded in the binary, user files
  under `~/.groot/library`, validated public sources (`library/sources.yaml`,
  GitHub tree listing), `/agent library|show|install`, `/agent import` for
  Claude Code / OpenCode style files.
- Models: `/models` (live list with prices), `/model <id>` with validation and
  suggestions, `--agent` and `--default`, automatic revert on a 404.
- `fetch_url`: readable text from a URL with an SSRF guard and domain lists;
  granted on demand.

### Sessions and UI
- Resumable sessions: a conversation reopens with its history and transcript
  (message timestamps, safe tool_use/tool_result split); `/clear` starts fresh.
- Tool-use repair: unanswered `tool_use` blocks get a `cancelled` result at
  send and resume time; cancelled turns persist consistent history.
- Markdown rendering of answers (glamour, green/brown theme).
- Prompt history (`↑/↓`, `~/.groot/history`), multi-line input
  (`shift-enter`), long pastes collapsed to `[Pasted text #N +L lines]`.
- Mouse: click to focus, drag to select and copy (OSC 52 + `pbcopy`), drag the
  dividers to resize (`~/.groot/layout.yaml`); native selection with option/alt.
- Sidebar focus sections (SPACES, AGENTS) with a persistent selection band;
  `enter` jumps to the next section.
- Fast exit: `ctrl-c` cancels turns and writes local summaries; `ctrl-d` and
  `/exit` ask the model for summaries within 5 s (`esc` skips).
- Feedback backlog (`~/.groot/feedback`): `/bug`, `/feat`, `/backlog`; reports
  travel through `debug`, the only writer.

## 0.2.0 — 2026-10-24

- Spaces (named working directories) with their own memory and audit log.
- Agent catalog in `~/.groot/agents/<name>.md` (YAML frontmatter + prompt),
  assigned per space, one conversation per (space, agent).
- Sidebar UI, slash commands for spaces and agents, first-run bootstrap.
- Embedded terminal column (`ctrl-t`): a real shell (pty + vt emulator) in the
  active space, with `tab` forwarded to the shell.

## 0.1.0 — 2026-10-24

- Agent loop with the Anthropic adapter (streaming, prompt caching, adaptive
  thinking), six builtin tools, interactive gate with diff previews, SQLite
  memory with FTS5, JSONL audit, `groot.yaml`, `AGENTS.md`, Bubble Tea UI with
  the pixel-art banner, architecture test and CI.
