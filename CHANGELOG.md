# Changelog

All notable changes to Groot CLI. Versions follow semver; the first three were
built in one day of live testing, so they are grouped by what shipped.

## 0.6.0-dev (unreleased)

### Flow
- Classification uses one test: could another agent resume the work from the
  request plus the git diff? `task_classify` takes `resumable_from_diff`, and the
  size must agree with it: large is the work that cannot be resumed; small and
  medium are the work that can. Any other answer is refused.
- Large work writes a feature document with `## Specs` (`S1.`), `## Tasks`
  (`T1. (S1)`, each naming the specs it serves) and `## Log` (`L1.` the original
  request). Moving to `planned` parses it and stores the specs; a plain plan is
  refused with the template.
- New tool `task_verify {spec, result, evidence}`: records a pass or fail for one
  spec in verify. Deliver is blocked until every spec passes; a fail goes back to
  implement with the reason, and the verdicts reset.
- The plan file stays writable through verify, so the Tasks and Log can be kept
  current.
- The flow tables gain `task_specs`, added on open for older databases; copy and
  rename of agents carry the specs with their tasks.

## 0.5.5-dev (unreleased)

### Time
- Every turn starts with a `Current time:` line in local time, with the zone
  (IANA name when the machine has one). It is rebuilt each turn; the cached base
  prompt is unchanged. (FEAT-015)
- New tool `current_time {timezone?}`: the current date, time, zone and unix
  time, in the given IANA zone or in local time; with a zone it also shows the
  local time for comparison. Read-only, no approval. An unknown zone is an error.
  (FEAT-015)

### Messaging
- `send_message` takes `ack: true` for a short confirmation with no answer
  (body up to 200 characters). Acks are kept in the inbox with kind `ack`: they
  never enter a turn, never start one in work mode, and are not counted in the
  unread badges. The recipient sees each one once as a note. `/inbox` tags them.
  Older inbox databases are migrated on open; their rows stay messages.
  (FEAT-016)
- Debug sends its `<ID> recorded` and duplicate confirmations as acks. (FEAT-016)

### Memory
- Scopes are `agent` (only its writer), `space` (every agent of the space, the
  default) and `global` (kept in the root database; groot writes it, and other
  agents read it only when enabled with `grant_global_memory`).
  **Migration:** entries saved as `project` now read `agent`; entries saved as
  `global` now read `space`. Existing databases migrate once on open. A new
  `global` entry needs the reader to be enabled.
- `remember` upserts by `topic_key` and counts repeated content instead of
  storing it twice. It accepts `title` and `topic_key`, and reports
  `remembered #id (created)`, `updated #id (revision n)` or `duplicate of #id`.
- New tools: `memory_get`, `forget`, `update_memory`, `grant_global_memory`,
  `revoke_global_memory`. `recall` now returns one line per entry with its id.
- Entries saved after reading untrusted output are marked tainted: they are
  shown inside the untrusted-content envelope in the prompt.
- The preamble is budgeted (`memory.preamble_tokens`, default 1500;
  `memory.preamble_summaries`, default 3) and rebuilt after every memory change.
- Session summaries follow one shape (goal, done, decisions, next, files) and are
  titled `Session <time> <agent>`. `/clear` records the cleared session as the
  parent of the new one.
- `/memory` shows flags and a footer with pinned and review counts, and gains
  `show`, `pin`, `unpin`, `rm`, `reviewed` and `review`.
- `list_agents` shows `global memory: yes` for enabled agents.

### Sessions
- A `/clear` whose model summary fails or times out keeps a local summary of
  the session and reports it as a note (`conversation cleared · summary for
  groot: the model did not answer in time; a local summary was kept`), not as
  an error. Before, the error was shown and no summary was stored.
- Closing groot gives the model summaries fifteen seconds instead of five;
  `esc` still skips the wait and keeps the local summaries.

### Agents
- `delete_space` and `delete_agent` take `confirm`. Without `confirm: true` they
  change nothing and report what the delete would touch: the agents assigned to
  a space, the spaces an agent is assigned to, and the open tasks of each. Call
  again with `confirm: true` to delete. The confirmation prompt says whether the
  call is the report or the delete.
- `delete_space` takes `purge_data` (honoured only with `confirm`). It deletes
  `<space dir>/.groot`, the memory database and the audit log, and nothing else
  in the directory. It refuses the home directory, a relative path, and a
  directory that another space uses.
- `move_agent` takes `with_memory`. It copies the agent's memory, its sessions
  (with their messages) and its tasks (with their evidence) into the target
  space's database. Task ids are new there, and a task's parent link follows
  them; a session the target already has is skipped. The source keeps its
  copies, and the result lists the counts.

### Distribution
- `install.sh` at the repository root installs groot on macOS and Linux. It
  detects the platform, downloads the release archive, checks its sha256 against
  `checksums.txt`, installs `groot` into `~/.local/bin` (or `GROOT_INSTALL_DIR`),
  and runs `groot doctor`. `GROOT_VERSION` pins a release. While the repository is
  private it needs `GITHUB_TOKEN`. Each release carries the script.
- A fresh home holds `root` and `groot` only. Bootstrap no longer creates or
  assigns the `debug` agent, the one that keeps the groot backlog. It ships in the
  built-in library: install it with `install_agent debug` or `/agent install debug`,
  into the space you choose.
- `/bug`, `/feat` and messages to `debug` fail with a message that says how to
  install debug when it is assigned to no space. Nothing is written in that case.
- An installed `debug` can be deleted; its backlog files stay. It is still kept
  current at each start and cannot be renamed. `groot` keeps every protection.
- Existing homes keep their debug file, its assignments and their groot prompt.
  The new line about debug in groot's prompt reaches fresh homes only.

The entries below were first written under 0.5.4, a version that was tagged by
mistake and never published; its tag was removed and they ship here.

### UI
- The status line above the input starts with the active agent as a badge:
  dark text on the primary green. It stays while the agent thinks, while
  others work in the background and while groot closes, so the name of the
  agent you are writing to is always in view.
- The banner prints the version in deep green under the tagline and the key
  hint, left aligned with them. The tagline now reads "Terminal Agent for Devs and
  Platform engineering".
- `home` and `end` (`fn-←` and `fn-→` on a Mac) move the cursor to the start
  or the end of the line when the input has text. With an empty input they
  scroll the transcript, as before.
- When `/readinbox` finds nothing unread for the active agent, its note says
  where the unread messages are (`3 unread elsewhere: groot-cli/debug (1), …`),
  so a badge on another agent or space is explained, and the badges are
  re-read at the same time.

### Agents
- The debug agent can be moved. Bootstrap used to assign it to `root` on every
  start, so a move to another space was undone by the next restart. It is now
  placed in `root` only when it is assigned to no space at all. Bug and feature
  reports, `/bug`, `/feat` and the bare `debug` address follow it to the space
  it lives in.
- Groot can delete, rename, move and copy spaces and agents, with six admin
  tools: `delete_space`, `edit_space`, `delete_agent`, `edit_agent`,
  `move_agent` and `copy_agent`. Each is a write that the user confirms inline,
  and each result is the state read back from `spaces.yaml` and the catalog.
  Deleting never removes a directory or the `.groot` data of a space, nor the
  memory and sessions of an agent. `root`, `groot` and `debug` cannot be deleted,
  and `groot` cannot be renamed.
- A rename moves the assignments, the selection, the inbox (unread messages
  included), and the tasks, memory and sessions of each space that holds the
  agent. Open conversations of the old name end and resume under the new one.
- The same operations are shortcuts in the UI: `/space delete <name>`,
  `/space rename <old> <new>`, `/agent delete <name>` and `/agent rename <old> <new>`.
  Work mode still asks for them.
- Bootstrap does not rewrite the stored prompt of an existing `groot`, so homes
  made before this release learn the new tools from the tool list, not from the
  prompt; edit `~/.groot/agents/groot.md` to mention them.

### Distribution
- `groot auth login` stores the Anthropic key: in the macOS keychain, or in
  `~/.groot/credentials` (mode 0600) when the keychain is unavailable and on
  Linux. It prompts with echo off, or reads the first line with `--stdin`.
  `groot auth status` shows the source and the last four characters;
  `groot auth logout` removes the stored key.
- The key is found in this order: `ANTHROPIC_API_KEY` in the environment, in
  `.env` in the working directory, the keychain, the credentials file. A
  startup without a key names `groot auth login` instead of failing on the
  variable alone.
- Global configuration: `~/.groot/groot.yaml` (or under `GROOT_HOME`) is read
  under the project `groot.yaml`. The project file wins; lists are replaced
  whole and maps merge by key. Writes go to the project file when it exists,
  otherwise to the global one.
- `groot doctor` prints the version, home, the config files loaded, the key
  source, the sandbox backend, kubectl and the data, and exits 1 when startup
  would be blocked.
- The sandbox warning on linux names the install command of the detected
  distribution (apt, dnf, pacman or zypper) instead of always apt.
- `groot upgrade` downloads the latest release for this platform, checks its
  sha256 against `checksums.txt`, and replaces the running binary (symlinks
  resolved). `--check` only reports. Nothing is written before the checksum
  matches. A permission error names the path and suggests `sudo` or
  `~/.local/bin`. While the repository is private, it needs `GITHUB_TOKEN`
  (or `GH_TOKEN`).
- Startup checks GitHub for a newer release at most once a day, in the
  background with a 3 second budget. A newer version shows as a dim note in
  the status line for 10 seconds: `v0.6.0 available · run: groot upgrade`. It
  never blocks startup and never fails it. Turn it off with `updates.check: false`.
- `groot doctor` prints `update:` from the cached check, with no network call:
  available, up to date, or unknown with the reason.
- Release pipeline: `.goreleaser.yaml` builds darwin and linux on amd64 and
  arm64 with `CGO_ENABLED=0`, and publishes `groot_<version>_<os>_<arch>.tar.gz`
  archives and `checksums.txt`. A pushed `v*` tag runs `.github/workflows/release.yml`,
  which tests the code and publishes the release, with the notes from the
  CHANGELOG section of that version (`cmd/relnotes`). `make release-check` and
  `make snapshot` need goreleaser; the other targets do not.

## 0.5.3 — 2026-10-25

### Backlog sweep
- The changelog is embedded in the binary and debug reads it with the new
  `read_changelog` tool, so the release sweep works from any space. Before,
  the rule looked for `CHANGELOG.md` at the root of the space, and debug lives
  in the root space, the home directory, where there is none. Entries marked
  unreleased or `-dev` never close an item. Debug no longer needs `read_file`.
- `read_changelog <version>` returns one entry; without a version, the whole
  changelog. A test ties the version string to a changelog heading.

## 0.5.2 — 2026-10-25

### Memory
- `/clear` keeps the agent's memory in view. The "Long-term memory" block of
  the system prompt was composed once when the conversation opened, so a
  cleared session did not see the summary just written nor anything remembered
  since. The prompt is now rebuilt on `/clear`, once right away and again after
  the summary of the cleared session is stored. Closes FEAT-013.
- `/memory` lists the entries the active agent carries in its prompt (id, kind,
  scope, age, text) and how many entries it can read in all. `/memory <query>`
  searches them.

### Messaging
- `/readinbox` makes the active agent read its unread messages in one turn and
  report sender, subject, a one-line summary and which ones need an answer.
  With nothing unread it prints a note and spends no turn. Closes BUG-004.

## 0.5.1 — 2026-10-25

### Fixed
- `grant_tool`, `revoke_tool` and `/agent tools <name> add|remove` saved the
  agent file but the running process kept the old definition: `list_agents`
  and the sidebar still showed the previous tools, and a conversation opened
  afterwards was built from the stale list. The catalog cache now reloads after
  every edit.
- A tool granted to an agent reached it only after restarting groot, because a
  conversation built its tool registry once and `/clear` kept it. Open
  conversations of the edited agent now rebuild their registry in place, so the
  tool is callable on the next turn and the session and its context are kept.
  Closes FEAT-012.

## 0.5.0 — 2026-10-25

### Audit
- `/audit` shows the last records of the active space as a table: time, agent,
  tool, risk, decision, duration and flags (`⚠` guard, `⛔` denied, `✗` error,
  `∅` no sandbox). Filters combine: a count, `denied`, `guard`, `errors`, `tool`,
  `agent`, `risk`, `decision`, `since`/`until` (`2h`, `1d` or a time), `find`, and
  `all` for every space. `/audit stats` prints the counts and the export sinks.
- The audit reader walks the file from the end, so a small limit on a large log
  stays cheap, and it skips malformed lines.
- The `audit_query` admin tool returns the same table.
- Records carry `space` and `agent`. The local file keeps its format; the fields
  are added and omitted when empty.
- `audit.sinks` exports a copy of each record to a `file`, an `http` collector
  (JSON Lines batches, 3 retries) or `syslog`. The local file is written first;
  sinks run asynchronously with a queue of 1000 and never block or fail a tool call.
  Sink failures warn once in the console and are counted in `/audit stats`.
  Header values come from the environment and are never logged.

### Prompt-injection guard
- Output from `fetch_url`, `read_file`, `list_dir`, `glob`, `bash`, `kubectl` and
  `recall`, and message bodies from other agents, are wrapped in an envelope with
  a random id per call. The system prompt tells the model that this content is
  data, not instructions.
- The scanner reports phrases that address the model, and invisible or bidirectional
  characters, which it removes. Findings appear in the result, in the transcript
  and in the audit entry (`guard`).
- `guard.mode`: `warn` (default), `strict` (withholds `fetch_url` results with
  findings) or `off` (unsafe).
- A tool call that copies the id of a recent envelope is tagged in its approval
  preview, and a read-only command asks instead of running unasked.

### Sandbox
- Every `bash` command runs inside an OS sandbox: `sandbox-exec` on macOS, `bwrap`
  on Linux. It can write only the space directory, the temp directories and
  `bash.sandbox.extra_writable`; it cannot read or write the credential paths
  `~/.ssh`, `~/.aws`, `~/.gnupg`, `~/.groot`, `~/.kube`, `~/.netrc`, `~/.docker`,
  `~/.config/gh`, `~/.config/gcloud` and `~/.azure`, nor write `<space>/.groot`; and it has no network
  unless `bash.sandbox.network: true`.
- `bash.sandbox.mode`: `auto` (default; without a backend, commands run unrestricted
  with a notice and `sandbox: none` in the header), `strict` (refuse without a
  backend, naming what to install) or `off`. The scalar form `sandbox: strict` works too.
- The audit record of a bash call carries `sandbox: <backend>`.
- The kubectl tool and the terminal column run outside the sandbox; see the README.

## 0.4.0 — 2026-10-24

### Delegation
- A route's `agent` takes the task over. With `delegation: auto` (the default) the
  task is handed to the agent through the inbox, as `task:#<id> handover`, and the
  origin keeps it open as handed over. The delegate's deliver note comes back as
  `task:#<id> delivered`: the origin records it and moves to `verify`. `returned`
  moves it back to `implement`, with the reason as evidence.
- A manager (`project-manager` when assigned, by default) gets `tracking` copies and
  every delivery and return. It does not implement handed-over tasks.
- `delegation` (`auto`, `hint` or `off`) and `manager` in `flow`, per space with the
  usual fallbacks. `/space routes` shows both with their origins and warns when the
  manager lacks `list_tasks`.
- `set_space_delegation {space, mode?, manager?}` admin tool; work mode asks before it.
- Tasks keep their delegation and origin. `/task` shows the delegation, `/tasks`
  shows `reader → developer` and `developer ← space/reader #12`. Existing databases
  gain the columns on open.
- The built-in rules tell the delegate how to answer a handover, and the manager how
  to track one.

### Command completion
- Typing `/` at the start of the input shows up to six matching commands above it,
  with their help; a command word followed by a space lists its sub-commands. `tab`
  completes the highlighted row, `↑`/`↓` move the highlight, `esc` closes the list,
  and `enter` completes when one row remains. History keys are unchanged while the
  list is closed. The help table is the single source for `/help` and completion.

### Backlog sweep
- `/backlog [open|done|all]` lists the backlog filtered by status.
- The debug agent has a release sweep: on "sweep the backlog" or "close the release"
  it reads the space's `CHANGELOG.md` and closes the items a shipped entry covers,
  with a `shipped in <version>` note. Unmatched items stay open.

### Tasks across spaces
- `/tasks` prints the open ODD tasks (phase not done or failed) of every space:
  space, agent, task id, size, phase, evidence count, opened and updated time,
  goal (60 characters at most) and whether the conversation is running a turn.
  `/tasks <space>` filters by space; `/tasks all` adds the done and failed tasks
  of the last seven days with their final phase.
- The sidebar shows `⚑ N` after a space with N open tasks, refreshed with the
  other badges.
- `list_tasks {space?, include_closed?}` admin tool (read-only), granted to `groot`.

### End-to-end tests
- `e2e/` holds tests against the real Anthropic API, behind the `e2e` build tag
  and skipped unless `ANTHROPIC_API_KEY` is set. `make e2e` runs them. They use
  the cheapest priced model with tiny prompts and print their total cost.
  CI does not run them.

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

### Context
- Context gauge per agent: the size of the last model answer (input plus cache
  tokens) is stored per session (`sessions.context_tokens`, added on open) and
  shown as `ctx 84k/200k 42%` in the header and `42%` in the sidebar, dim, then
  warning from 70% and red from 90%.
- `compaction.max_context_percent` (default 80, 0 disables): the history is
  compacted before a call once the context fills that share of the window. With
  strategy `none` groot warns once at 90%.
- Overflow recovery: a "prompt is too long" refusal compacts once and retries
  once, with a transcript line.
- `/clear` resets the context gauge: the header and the sidebar show no context
  for the agent until its next answer.
- `models.context_window` in `groot.yaml` overrides the window per model or
  prefix; `/context`, `/context all`, `/context window <model> <tokens>`.
- Admin tools `get_context` (read-only) and `set_context_window`, granted to `groot`.

### Tools
- `list_dir {path?, recursive?, max_depth?, include_hidden?}` lists a directory:
  `d <path>` and `f <path> <bytes>`, directories first. One level by default; with
  `recursive`, `max_depth` defaults to 3 and is capped at 8. Read-only, confined to
  the space root, never follows symlinked directories out of it.
- `glob {pattern, path?, max_results?}` finds files with `**` (any number of
  directories) and `*` (within one name). Results are relative to the space root,
  at most 500. Dotfiles only when the pattern starts with a dot.
- Both skip `.git`, `node_modules`, `vendor`, `.groot`, `target`, `dist` and
  `build`, have a 10-second budget and are in the base set that `/agent upgrade` adds.

### Cost visibility
- `ui.show_cost` (default false) controls the cost in the status line and the
  spend in the sidebar. Sidebar spend shows only next to a budget unless it is on.
- `/cost` prints the session cost with the spend and budgets of the space.
- Admin tool `get_cost` (read-only) returns the same as text.

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
