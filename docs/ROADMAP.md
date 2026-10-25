# Roadmap

## 0.1.0 (done)

Single-provider MVP: the Anthropic adapter, six builtin tools, the interactive
gate, SQLite memory, the JSONL audit log, and the terminal UI.

## 0.2.0 (done): spaces, agents and the terminal column

- Spaces: named working directories with their own memory and audit log.
- Agents: reusable definitions in `~/.groot/agents`, assigned to spaces, with
  one conversation per (space, agent) pair.
- Sidebar UI with keyboard navigation, space and agent slash commands.
- First-run bootstrap with the `root` space and the `groot` administrator agent.
- Embedded terminal column (`ctrl-t`): a real shell in the active space.

## 0.3.0 (done): flow, work mode, messaging, background turns

See CHANGELOG.md. Highlights: the ODD task flow with per-size model routes,
work mode with limits and end-of-task review, the inbox between agents with
automatic delivery and work-mode reactions, background conversations with
indicators, resumable sessions, the agent library with validated public
sources, model listing and validation, `fetch_url`, and the feedback backlog
handled by the `debug` agent.

## 0.4.0 (done): coordination

- Automatic delegation: a `large` task routed to an agent is handed over
  through the inbox, with the space's project-manager as the channel (done).
- Routes and models per space (today they are global) (done).
- Budgets per space and per agent (`budget_usd`), spend shown in the sidebar,
  stop before overrun (done).
- Context gauge per agent, token-based compaction, overflow recovery (done).
- `/tasks`: every open task across spaces (done).
- End-to-end tests against the real API, opt-in with a key (done).

## 0.5.0 (done): safety

- A real sandbox for `bash` (today the gate decides, but the command runs with
  the user's full permissions) (done).
- Prompt-injection guard for `fetch_url` and tool results: external content is
  data, not instructions (done).
- `/audit` in the UI and export of audit records to an external sink (done).

## 0.5.1 to 0.5.3 (done): fixes from live use

Three patch releases on 2026-10-25, each from a problem found while using
groot to run groot. The backlog items they close are in CHANGELOG.md.

- 0.5.1: a tool granted with `grant_tool` or `/agent tools add` never reached
  the running process. The catalog cache is reloaded after every edit and open
  conversations swap their tool registry in place, keeping their session.
- 0.5.2: `/clear` keeps the agent's memory in view by rebuilding the prompt
  from stored memory; `/memory` and `/memory <query>`; `/readinbox`.
- 0.5.3: the changelog is embedded in the binary and debug sweeps the backlog
  with `read_changelog` from any space.

## Status at 2026-10-25

What is verified where. "Live" means the user exercised it in a real session;
"tests" means unit, race and CI coverage only, including the Linux sandbox job.

| Area | Verified | Notes |
|---|---|---|
| Spaces, agents, terminal, memory, messaging, ODD flow, work mode | live | since 0.3.0 |
| Routes, budgets, context gauge, delegation, hidden cost | live | 0.4.0, e2e run against the API |
| Tool grants reaching open conversations | tests | 0.5.1, restart pending |
| `bash` sandbox (macOS `sandbox-exec`, Linux `bwrap`) | tests | network is off by default: `bash.sandbox.network`, `extra_writable` for the Go cache |
| Prompt-injection guard, tainted calls always prompt | tests | 0.5.0 |
| `/audit`, `audit_query`, sinks | tests | 0.5.0 |
| `/clear` memory refresh, `/memory`, `/readinbox` | tests | 0.5.2 |
| Backlog sweep with `read_changelog` | tests | 0.5.3 |
| Admin tools: delete, rename, move, copy; two-step confirm, `purge_data`, `with_memory` | live (first round) | 0.5.5-dev, the user exercised create/edit/delete/move |
| `groot auth`, layered config, `groot doctor` | live (`doctor` on the dev machine) | 0.5.5-dev |
| goreleaser pipeline, `groot upgrade`, `install.sh` | tests + local snapshot | 0.5.5-dev; the GitHub release workflow has not published yet |
| `debug` from the library, movable | tests | 0.5.5-dev |
| `/clear` summary fallback, 15 s close budget | tests | 0.5.5-dev |

Known limits carried forward:

- Delegation runs in the same space only; the manager needs `list_tasks`
  granted by hand.
- `/audit stats` streams the whole file; audit records carry no taint field.
- A message to an agent is read on its next turn; only work mode starts a
  turn by itself.
- The memory block of the prompt is rebuilt on open and on `/clear`, not on
  every `remember`.

Decisions waiting on the user before 0.6.0 is planned:

- An ACP client so an external agent engine (AWS Kiro: `kiro-cli acp`) can be
  used from groot. Kiro has no token API, so this is the only integration path;
  the Bedrock adapter stays the way to control cost.
- Skills as prompt fragments attached to agents.

## 0.5.5: first public release

Decisions and the order of work are in [DISTRIBUTION.md](DISTRIBUTION.md).
Done on main: `groot auth`, layered config, `groot doctor`, the goreleaser
pipeline, `groot upgrade`, the admin tools to delete, rename, move and copy
spaces and agents; `debug` moved from bootstrap to the built-in library (a fresh
install holds `root` and `groot` only); `install.sh`. Still to do before the
tag: the user's polish pass, then the repository goes public, the tag runs the
release workflow for the first time, and `groot upgrade --check` is verified
against a real release. After that, the Homebrew tap.

Memory hygiene slice (in progress on the working tree): scopes agent, space and
global with a root-database global store and per-agent enablement; upsert by
topic key and dedupe by content; pin, review dates and taint; a budgeted,
rebuilt preamble; fixed-shape session summaries; and the `/memory` subcommands.
The model is in [MEMORY.md](MEMORY.md). Vector recall stays under Later.

Session closed on 2026-10-25 at commit `74aef30` with everything above on
main, verified and pushed. Backlog: BUG-005 and FEAT-014 are implemented and
still marked open until debug is told or the 0.5.5 sweep runs.

## 0.6.0: connectivity

- MCP client with an explicit allowlist of servers and tools.
- Read-only log (LogQL) and metrics (PromQL) tools with namespace and
  time-range guards.
- Bedrock, Vertex, Ollama and OpenAI-compatible adapters, chosen by the model
  id prefix. Only when asked: Anthropic stays the single provider until then.
- Candidates, see the decisions above: ACP client, skills.
- A2A (Agent2Agent, spec 1.0, Apache-2.0, Linux Foundation) as a client for
  talking to remote agents outside groot, and later as an optional server behind
  groot's gates. Not for the internal inbox: agents of one groot process share a
  space and its gates, so a network protocol between opaque agents is the wrong
  layer there. Inbox rows should stay mappable to an A2A Message (message id,
  context id pointing at the flow task, parts), so a bridge is an adapter.

## Later

- Vector recall through Ollama behind the `memory.Store` port.
- Per-hunk approval for diffs.
- Detachable background terminals that survive a restart.
- Web search.
