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

## 0.4.0: coordination

- Automatic delegation: a `large` task routed to an agent is handed over
  through the inbox, with the space's project-manager as the channel.
- Routes and models per space (today they are global) (done).
- Budgets per space and per agent (`budget_usd`), spend shown in the sidebar,
  stop before overrun (done).
- Context gauge per agent, token-based compaction, overflow recovery (done).
- `/tasks`: every open task across spaces.
- End-to-end tests against the real API, opt-in with a key.

## 0.5.0: safety

- A real sandbox for `bash` (today the gate decides, but the command runs with
  the user's full permissions).
- Prompt-injection guard for `fetch_url` and tool results: external content is
  data, not instructions.
- `/audit` in the UI and export of audit records to an external sink.

## 0.6.0: connectivity

- MCP client with an explicit allowlist of servers and tools.
- Read-only log (LogQL) and metrics (PromQL) tools with namespace and
  time-range guards.
- Bedrock, Vertex, Ollama and OpenAI-compatible adapters, chosen by the model
  id prefix. Only when asked: Anthropic stays the single provider until then.

## Later

- Vector recall through Ollama behind the `memory.Store` port.
- Per-hunk approval for diffs.
- Detachable background terminals that survive a restart.
- Web search.
