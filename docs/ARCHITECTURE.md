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
| `flow` | ODD task engine: sizes, phases, evidence, transitions (`flow.go`) and the per-conversation `Controller`. |
| `memory` | Persistence port (`store.go`): sessions, messages, long-term memories, tasks. SQLite adapter in `sqlite.go`, `tasks.go`, `sessions.go`. `NoMemory` persists nothing. |
| `workspace` | Runtime for spaces and agents: builds conversations, opens stores, composes the gate chain, messaging, library, review. |
| `ui` | Bubble Tea terminal interface. Renders and forwards input; no agent logic. |
| `term` | Runs an interactive shell in a pseudo-terminal (`pty.go`) and renders its output (`render.go`). |
| `inbox` | Messages between agents and groot, in SQLite (`inbox.go`). |
| `library` | Lists the agents that can be installed: builtin, GitHub collections, local files (`sources.go`). |
| `space` | The list of spaces, the active space and the agents assigned to each (`space.go`). |
| `home` | Resolves the global state directory, `~/.groot` or `GROOT_HOME`. |
| `feedback` | Backlog of bugs and feature requests about groot. |
| `config` | `groot.yaml` schema, defaults, `${VAR}` expansion, system prompt composition, `AGENTS.md`. |

Supporting packages: `api` (provider-neutral messages and blocks), `audit` (JSONL
log of tool calls), `catalog` (agent definitions on disk), `compact` (history
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
   The loop ends when the model stops asking for tools or `max_turns` is reached.

The memory preamble is built once, when a conversation is created
(`internal/workspace/workspace.go`, `Preamble(ctx, agent, 10)`), and composed into
the system prompt by `config.ComposeSystemPrompt`.

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

## ODD flow

Strict mode denies writes that skip the flow. Advisory mode only warns.

```
medium:  implement -> verify -> deliver -> done          (opened in implement)
large:   classified -> planned -> implement -> verify -> deliver -> done
verify -> implement   on failure, the reason is kept as evidence
any open phase -> failed, the reason is kept as evidence
```

Evidence rules, from `internal/flow/flow.go`:

- `planned` needs the plan file path (large tasks only).
- `verify` needs a command run during the current implement cycle.
- `deliver` needs a note with the changes and the risks.

Evidence kinds are `command`, `note` and `plan`. Risky calls (writes, commands)
need the `implement` phase, with one exception: a large task may write a
`write_file` under `docs/tasks/` while it is `classified` or `planned`. Small
tasks need no task at all (`Controller.Check` in `internal/flow/controller.go`).

Delegation: `Controller.Open` decides a handover (`internal/flow/controller.go`). The
task messages `task:#<id> handover|delivered|returned|tracking` are the contract
between agents (`internal/flow/delegation.go`), applied by `messenger.Send` through
`internal/workspace/delegation.go`.

## Storage

Global state, under `~/.groot` (or `GROOT_HOME`):

- `spaces.yaml`: spaces, the active space, assigned agents (`internal/space`).
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
  - `memories`: long-term entries (`kind`, `scope`, `agent`, `text`).
  - `memories_fts`: FTS5 index over `memories.text`, kept in sync by triggers.
  - `tasks`, `task_evidence`: ODD flow tasks and their evidence. A handed-over task keeps its delegation record (`delegate`, `manager`, `delegation_status`), and a task taken over keeps its origin (`parent_space`, `parent_agent`, `parent_task`); `migrate` adds these columns to older databases.
  - The spend ledger is `sessions.cost_usd`, written after every model answer;
    budgets sum it (`memory.SpendUSD`, `SpendByAgent`; checks in `internal/workspace/budget.go`).
- `audit.jsonl`: one JSON line per tool call.
- Open tasks across spaces (`/tasks`, `list_tasks`, sidebar `⚑ N`): `workspace.ListTasks` reads each space's `tasks` through `memory.SQLite.TaskSummaries`, with the directory guard of `internal/workspace/context.go` (`internal/workspace/tasks.go`).

Relative `memory.path` and `audit.path` values resolve against the space
directory (`memoryPathFor`, `auditPathFor` in `internal/workspace/workspace.go`).

## Tests

- `make test` runs `go test ./... -count=1`.
- `make lint` runs `go vet` and a gofmt check.
- `internal/arch` fails the build when a core package imports an SDK, the UI, or
  `os/exec` outside `internal/tool/exec.go`.
- Fakes: `provider.NewMock` for model turns, `memory.OpenSQLite` on a temp path
  for stores.
