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
  through the inbox, with the space's space-manager as the channel (done).
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
"tests" means unit and CI coverage only. Since 2026-10-26 CI runs the whole
suite under the race detector on Linux and macOS, with the bwrap and seatbelt
sandbox tests required on each.

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
| Admin tools: delete, rename, move, copy; two-step confirm, `purge_data`, `with_memory` | live (first round) | 0.6.0, the user exercised create/edit/delete/move |
| `groot auth`, layered config, `groot doctor` | live (`doctor` on the dev machine) | 0.6.0 |
| goreleaser pipeline, `groot upgrade`, `install.sh` | tests, published release | 0.6.0; the release workflow published v0.6.0 on 2026-10-26 (four archives, `checksums.txt`, `install.sh`) |
| `debug` from the library, movable | tests | 0.6.0 |
| `/clear` summary fallback, 15 s close budget | tests | 0.6.0 |

Known limits carried forward:

- Delegation runs in the same space only; the manager needs `list_tasks`
  granted by hand.
- `/audit stats` streams the whole file; audit records carry no taint field.
- A message to an agent is read on its next turn; only work mode starts a
  turn by itself.
- The memory block of the prompt is rebuilt on open, on `/clear`, and after each
  `remember`, `forget` or `update_memory` call, so a new entry is in view on the
  next model call.

Decisions waiting on the user before 0.7.0 is planned:

- An ACP client so an external agent engine (AWS Kiro: `kiro-cli acp`) can be
  used from groot. Kiro has no token API, so this is the only integration path;
  the Bedrock adapter stays the way to control cost.
- Skills as prompt fragments attached to agents.

## 0.5.5: folded into 0.6.0

0.5.5 was never published. Its work shipped in 0.6.0, the first public release.
Decisions and the release steps are in [DISTRIBUTION.md](DISTRIBUTION.md).

## 0.6.0 (done): first public release

Cut from main on 2026-10-25. The CHANGELOG.md section `0.6.0` has the details.

- Flow quality: the resume test classifies the work, large work writes a feature
  document with numbered specs, `task_verify` checks each spec before delivery,
  and the delivery review lists the test runs.
- MCP client: servers from the global config, allowlisted tools, `/mcp`, and an
  `mcp` section in `groot doctor`.
- Identity and mentions: `@` mentions between you and the agents, the operator name.
- Memory hygiene: scopes agent, space and global; upsert by topic key; pin,
  review dates and taint; a budgeted preamble; the `/memory` subcommands.
- Distribution: `groot auth`, layered config, `groot doctor`, the goreleaser
  pipeline, `groot upgrade`, `install.sh`, the admin tools, and `debug` in the library.
- Live-use fixes: turn budget answers, `read_file` windows, bash timeouts,
  global memory ids, and `/mcp connect` off the UI loop.

Backlog: BUG-005 and FEAT-014 are implemented and still marked open until debug
is told or the sweep runs.

## Next, in order (validated 2026-10-26)

The rule behind the order: first prove what is already promised, then close
the cheapest safety gap, then features.

| # | Item | Size | State |
|---|---|---|---|
| 1 | `-race` in CI, macOS CI job for seatbelt | S | done 2026-10-26 |
| 2 | Doc drift fixes, unsandboxed paths noted in SAFETY | S | done 2026-10-26 |
| 3 | Destructive-command guard under the sandbox | S | done 2026-10-26 |
| 4 | Task phase inferred from tool activity | S | done 2026-10-26 |
| 5 | `bash_background` and `monitor` | M | done 2026-10-27 |
| 6 | Context backstop | M | open |
| 7 | Provider adapters (0.7.0 below) | L | on request |
| 8 | ACP client, skills | L | owner decision |
| 9 | Disjoint edit surfaces for parallel writers | L | after parallel writers |
| 10 | PromQL and LogQL tools | M | on demand |

Repo hygiene landed on 2026-10-28 in one commit: `.idea/` ignored, `dist/`
removed, `AGENTS.md` filled, `groot.yaml.example` comments corrected (no field
was dead), `make notices` plus an arch test for `THIRD_PARTY_NOTICES.md`, and a
confirmation on `make cleanall`.

## 0.7.0: connectivity

Decision (2026-10-25): 0.6.0 shipped the base structure only, and 0.7.0 keeps that rule. Domain tools are
built when a real task needs them, not ahead of it. The first of those is in
the backlog below.

- Bedrock, Vertex, Ollama and OpenAI-compatible adapters, chosen by the model
  id prefix. Only when asked: Anthropic stays the single provider until then.
- Candidates, see the decisions above: ACP client, skills.
- A2A (Agent2Agent, spec 1.0, Apache-2.0, Linux Foundation) as a client for
  talking to remote agents outside groot, and later as an optional server behind
  groot's gates. Not for the internal inbox: agents of one groot process share a
  space and its gates, so a network protocol between opaque agents is the wrong
  layer there. Inbox rows should stay mappable to an A2A Message (message id,
  context id pointing at the flow task, parts), so a bridge is an adapter.

## Backlog: domain tools, built on demand

- Read-only log (LogQL) and metrics (PromQL) tools with namespace and
  time-range guards. Design kept: four tools (`promql_query`, `promql_range`,
  `logql_query`, `logql_labels`), `observability.prometheus|loki` config with
  url, headers via `${VAR}`, `max_range`, `require_namespace` and a namespace
  allowlist checked over the matcher blocks, compact output, a doctor section.
  Postponed on 2026-10-25 until a task needs them; until then agents can reach
  Prometheus and Loki through an MCP server.

## Later

- Vector recall through Ollama behind the `memory.Store` port.
- Per-hunk approval for diffs.
- Detachable background terminals that survive a restart.
- Web search.
