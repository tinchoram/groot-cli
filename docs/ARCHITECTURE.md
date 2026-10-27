# Architecture

A map of the code for contributors. Paths are relative to the repository root.
The import boundaries are enforced by `internal/arch/arch_test.go`.

## Layout

`cmd/groot/main.go` is wiring only: flags, config, adapters, UI. The rest lives
under `internal/`:

| Package | Role |
|---|---|
| `agent` | Conversation loop. Knows only the ports: calls the provider, runs tools through the gate, persists messages, writes the audit log. |
| `provider` | Model port (`provider.go`) and adapters: `anthropic.go`, `mock.go` for tests. Also pricing and the model list. |
| `sandbox` | Runner port for bash (`sandbox.go`): `Detect` picks the backend for the platform. Adapters: `seatbelt.go` (macOS `sandbox-exec`, SBPL profile) and `bwrap.go` (Linux bubblewrap). Without a backend, `none` runs commands directly and `strict` refuses. Imported by `tool` (bash) and wired in `cmd/groot`. |
| `tool` | Tool port and registry (`tool.go`), builtin tools: `files.go`, `exec.go`, `fetch.go`, `memory_tools.go`, `flow_tools.go`, `messaging_tools.go`, `feedback_tools.go`, `readonly.go`, `listdir.go` and `glob.go` (read-only, confined to the space root). |
| `gate` | Permission decisions: `Interactive`, `ReadOnly`, `AutoApprove` (`gate.go`), work mode (`work.go`), session grants (`grants.go`). |
| `flowgate` | Puts the ODD flow rules in front of the user's gate (`gate.go`). |
| `mcp` | MCP client (`manager.go`): one lazily connected session per configured server, the tool allowlist, call timeouts. `tools.go` adapts a remote tool to `tool.Tool` as `<server>__<tool>`: untrusted output, `External`, capped at 64 KiB. `mcptest` is an in-process fake server for tests. Config in `config/mcp.go`. |
| `flow` | ODD task engine: sizes, phases, evidence, transitions (`flow.go`) and the per-conversation `Controller`. |
| `memory` | Persistence port (`store.go`): sessions, messages, long-term memories, tasks. SQLite adapter in `sqlite.go`, `tasks.go`, `sessions.go`. `NoMemory` persists nothing. |
| `workspace` | Runtime for spaces and agents: builds conversations, opens stores, composes the gate chain, messaging, library, review. |
| `ui` | Bubble Tea terminal interface. Renders and forwards input; no agent logic. |
| `term` | Runs an interactive shell in a pseudo-terminal (`pty.go`) and renders its output (`render.go`). |
| `guard` | Untrusted-content guard (`guard.go`): wraps tool output and messages in a nonce envelope, scans for instruction-like text, strips invisible characters, and remembers the nonces so the gate can tag calls that copy them. The agent applies it to tool results, `read_inbox` wraps its own bodies, and the workspace uses it for the inbox notice. |
| `inbox` | Messages between agents and groot, in SQLite (`inbox.go`). |
| `library` | Lists the agents that can be installed: builtin, GitHub collections, local files (`sources.go`). |
| `space` | The list of spaces, the active space and the agents assigned to each (`space.go`). |
| `home` | Resolves the global state directory, `~/.groot` or `GROOT_HOME`. |
| `update` | Release check and self-update: `Latest` reads the GitHub release API (`update.go`), `Apply` verifies the archive's sha256 against `checksums.txt` and replaces the binary by rename. `check.go` keeps `<home>/update-check.json` so the API is asked at most once a day. Used by `groot upgrade`, the startup note and `groot doctor` (cache only). |
| `cmd/relnotes` | Prints the CHANGELOG.md section of one version, without its heading. The release workflow passes it to goreleaser as the release notes (`main.go`). |
| `auth` | Finds and stores the Anthropic key: environment, `.env`, the macOS keychain (through `security`), `<home>/credentials`. |
| `feedback` | Backlog of bugs and feature requests about groot. |
| `changelog` (repo root, `changelog.go`) | Embeds `CHANGELOG.md`; `go:embed` cannot reach a parent directory, so the file sits at the root. `Section` finds one version's entry. Used by `read_changelog` and by the version check in `cmd/groot`. |
| `config` | `groot.yaml` schema, defaults, layering (global file, then `user.yaml` with the operator's name, then the project file), `${VAR}` expansion in the global and user files only (a project file is checked against the allow-list in `project.go` and never expands env), system prompt composition (People block included), `AGENTS.md`. |

Supporting packages: `api` (provider-neutral messages and blocks), `audit` (JSONL
log of tool calls; `reader.go` filters it from the end of the file and `Summarize`
counts it), `catalog` (agent definitions on disk), `compact` (history
strategies), `messaging`, `admintool`, `agentimport`, `clipboard`.

## Ports and adapters

| Port | Defined in | Adapters |
|---|---|---|
| `provider.Provider` | `internal/provider/provider.go` | `anthropic.go`, `mock.go` |
| `tool.Tool` | `internal/tool/tool.go` | the builtin tools listed above |
| `sandbox.Runner` | `internal/sandbox/sandbox.go` | `seatbelt.go` (sandbox-exec), `bwrap.go` (bubblewrap), `noneRunner` and `offRunner` (direct) |
| `memory.Store` | `internal/memory/store.go` | `sqlite.go`, `NoMemory` |
| `gate.Gate` | `internal/gate/gate.go` | `Interactive`, `ReadOnly`, `AutoApprove`, wrapped by `Work` and `Session` |

## Per-turn wiring

1. The UI calls `Conversation.Send` (`internal/workspace/conversation.go`). A busy
   flag refuses a second turn on the same conversation.
2. `send` calls `flow.Controller.StartTurn`, then builds the task block from
   `ContextBlock` and prepends it to the base system prompt (`withBlock`,
   `internal/workspace/flow.go`).
3. Unread inbox messages are announced in the prompt (`injectInbox`).
4. `Agent.Send` (`internal/agent/agent.go`) runs: `ensureSession` opens the
   session once; `appendMessage` stores each message under the lock and
   persists it through `memory.Store`; the loop calls the provider, and for each
   `tool_use` it runs `runTool`: gate, then `Tool.Execute`, then the audit record.
   The loop ends when the model stops asking for tools or `max_turns` is reached; at the limit the agent is asked for a final answer without tools.

The memory preamble is rebuilt, not built once: when a conversation is opened,
on `/clear`, and after every change through the memory tools or `/memory`
(`refreshSystem`, `internal/workspace/conversation.go`). It is budgeted by
`memory.preamble_tokens` and `memory.preamble_summaries`, and composed into the
system prompt by `config.ComposeSystemPrompt`. See `docs/MEMORY.md`.

Mentions (`internal/ui/mention*.go`, `internal/ui/route.go`) are resolved in the UI, before a turn starts. A message that starts with `@address` is routed to that conversation (`routeTo` switches the space and selects the agent). Other `@` tokens resolve to a `[mentions: …]` line that is appended to the message, so the turn carries the full addresses. Nothing about mentions reaches the workspace except the People block in the system prompt.

## Gate chain

`workspace.go` wires every conversation as:

```
flowgate.Gate            ODD flow rules: refuses calls the flow does not allow
  -> gate.Work           work mode: approves what it covers, within its limits
    -> gate.Session      grants approved for this conversation
      -> user gate       confirmingGate(w.opts.Gate): interactive prompt or policy
```

A `flowgate` denial returns a `tool_result` with the reason and never asks the
user. Anything it allows goes down the chain.

Work mode never auto-approves a write of an external tool. An MCP tool sets
`gate.Request.External` through the `tool.External` marker, so `work.go` sends
its writes to the user. A read_only MCP tool is approved like any read.

## ODD flow

Strict mode denies writes that skip the flow. Advisory mode only warns.

```
medium:  implement -> verify -> deliver -> done          (opened in implement)
large:   classified -> planned -> implement -> verify -> deliver -> done
verify -> implement   on failure, the reason is kept as evidence; verdicts reset
any open phase -> failed, the reason is kept as evidence
```

Classification (`Controller.Classify`, `task_classify`) applies the resume test:
could another agent resume the work from the request plus the git diff? Large
must answer no, small and medium yes. Any other pair is refused.

Evidence rules, from `internal/flow/flow.go`:

- `planned` needs the plan file path (large tasks only). The plan must parse as
  a feature document (`internal/flow/featuredoc.go`): `## Specs` with `S<n>.`,
  `## Tasks` with `T<n>. (S<n>)`, each task naming existing specs, and `## Log`
  with `L1.` the original request. Its specs are stored in `task_specs`.
- `verify` needs a command run during the current implement cycle. A large task
  also records a verdict per spec with `task_verify` (`pass` or `fail`, with
  evidence). Evidence kind `verdict`.
- `deliver` needs a note with the changes and the risks. A large task delivers
  only when every spec passed: a pending spec is refused, a failed one blocks the
  delivery and names the spec.

Evidence kinds are `command`, `note`, `plan` and `verdict`. Risky calls (writes,
commands) need the `implement` phase, with two exceptions for a large task: a
`write_file` under `docs/tasks/` while it is `classified` or `planned`, and a
write to the recorded plan path in any open phase, so the Tasks and Log stay
current. Small tasks need no task at all (`Controller.Check` in
`internal/flow/controller.go`).

Delegation: `Controller.Open` decides a handover (`internal/flow/controller.go`). The
task messages `task:#<id> handover|delivered|returned|tracking` are the contract
between agents (`internal/flow/delegation.go`), applied by `messenger.Send` through
`internal/workspace/delegation.go`.

## Storage

Global state, under `~/.groot` (or `GROOT_HOME`):

- `spaces.yaml`: spaces, the active space, assigned agents (`internal/space`).
- `user.yaml`: the operator's name, a single `name:` key, written atomically with mode 0600 (`config.SaveUserName`). It loads as the identity layer between `groot.yaml` and the project file.
- Context size: measured after each model answer in `Agent.recordContext`
  (`internal/agent/agent.go`), stored in `sessions.context_tokens`; windows come
  from `provider.ContextWindow` (`internal/provider/context.go`).
- `agents/<name>.md`: agent definitions (`internal/catalog`).
- `library/`: local agent collections (`internal/library`).
- `history`: prompt history (`internal/ui`).
- `feedback`: feedback backlog (`internal/feedback`).
- `inbox.db`: SQLite table `messages`, the inter-agent mailbox (`internal/inbox`).

Per space, `<space>/.groot/`, at the paths set by `memory.path` and `audit.path`:

- `groot.db` (SQLite), tables:
  - `sessions`, `messages`: one row per conversation and per stored message.
  - `memories`: long-term entries (`kind`, `scope` agent|space|global, `agent`, `title`, `text`, `topic_key`, `normalized_hash`, `pinned`, `tainted`, `review_after`, `revision_count`, `duplicate_count`, `deleted_at` for soft delete).
  - `memories_fts`: FTS5 index over `memories.title` and `memories.text`, kept in sync by triggers.
  - Global entries live in the root space's `groot.db`. A space that is not the root reads and writes them through `memory.Layered` (`internal/memory/layered.go`), which keeps everything else in its own database.
  - `tasks`, `task_evidence`: ODD flow tasks and their evidence. A handed-over task keeps its delegation record (`delegate`, `manager`, `delegation_status`), and a task taken over keeps its origin (`parent_space`, `parent_agent`, `parent_task`); `migrate` adds these columns to older databases.
  - The spend ledger is `sessions.cost_usd`, written after every model answer;
    budgets sum it (`memory.SpendUSD`, `SpendByAgent`; checks in `internal/workspace/budget.go`).
- `audit.jsonl`: one JSON line per tool call. `audit.Fanout` (`sink.go`) copies each record
  to the sinks of `audit.sinks` (`sink_file.go`, `sink_http.go`, `sink_syslog.go`) through
  bounded queues; the workspace owns the fanout and flushes it on close (`internal/workspace/audit.go`).
- Open tasks across spaces (`/tasks`, `list_tasks`, sidebar `⚑ N`): `workspace.ListTasks` reads each space's `tasks` through `memory.SQLite.TaskSummaries`, with the directory guard of `internal/workspace/context.go` (`internal/workspace/tasks.go`).
- MCP: one `mcp.Manager` per space, built in `storesFor` from the enabled servers and closed with the space's other stores. Stdio servers start in the space directory. Connections are lazy: opening a space starts nothing. Conversations build their MCP tools in `rebuild` (`internal/workspace/mcp.go`), and warnings reach the transcript off the workspace lock.

Relative `memory.path` and `audit.path` values resolve against the space
directory (`memoryPathFor`, `auditPathFor` in `internal/workspace/workspace.go`).

## Tests

- `make test` runs `go test ./... -count=1`.
- `make lint` runs `go vet` and a gofmt check.
- `internal/arch` fails `go test` when a core package (`agent`, `api`, `gate`,
  `compact`, `tool`) imports a vendor SDK or UI library, `internal/ui`,
  `internal/provider/` or `cmd/`, or runs `os/exec` outside `internal/tool/exec.go`.
  `api` may import no groot package. `internal/ui` may not import the PTY or the
  terminal emulator directly; it reaches them through `internal/term`.
- Fakes: `provider.NewMock` for model turns, `memory.OpenSQLite` on a temp path
  for stores.

## Overview

```
cmd/groot/main.go             wiring only: flags, config, adapters, UI
        |
        v
internal/agent               conversation loop (knows only the ports)
   |        |        |        |         |          |
   v        v        v        v         v          v
provider   tool     gate    compact   memory     audit
(port)    (port)   (port)   (port)    (port)     (JSONL)
   |        |
   v        v
anthropic  tool families: files/search, commands, web, memory,
mock       messaging, flow, backlog, admin, MCP
                    internal/api: provider-neutral messages and blocks
                    internal/config: groot.yaml and AGENTS.md
                    internal/ui: Bubble Tea screens, no agent logic
                    internal/arch: test that enforces the import boundaries
```

A turn runs like this: compact the history, call the model with streaming, and
for each `tool_use` block ask the gate. If the call is allowed, run the tool.
Otherwise return a `tool_result` marked `denied by user`. Persist every message,
and audit every call. Repeat until the model stops asking for tools. When
`max_turns` is reached, the agent is asked for a final answer without tools.
