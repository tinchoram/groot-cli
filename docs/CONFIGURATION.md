# Configuration

The groot.yaml fields, the context window, and spend budgets.

## Configuration

Copy `groot.yaml.example` to `groot.yaml`. Every field is optional. In the global
file, string values accept `${VAR}` and `${VAR:-default}`; the project file never
expands them (see [Environment expansion](#environment-expansion)). If `AGENTS.md` exists in the working
directory, Groot adds it to the system prompt under "Project context".

| Field | Default | Meaning |
|---|---|---|
| `provider.name` | `anthropic` | Only `anthropic` in this release |
| `provider.model` | `claude-sonnet-5-5` | Model id |
| `provider.effort` | unset | `low`, `medium`, `high`, `xhigh` or `max`, sent as `output_config.effort`. Unset sends nothing, so the model's own default applies. An agent's `effort` overrides it |
| `max_turns` | `40` | Model calls allowed per request; at the limit the agent answers once more without tools |
| `compaction.strategy` | `summarize` | `none` (keep everything), `window` (keep N) or `summarize` (the older half becomes a summary) |
| `compaction.max_context_percent` | `80` | Compact before a call once the last context fills this share of the window; `0` turns it off. See Context |
| `models.context_window` | empty | Map of model id or prefix to context window in tokens. Overrides the built-in table (200000) |
| `ui.show_cost` | `false` | Show the estimated session cost in the status line and the spend of each agent in the sidebar. `/cost` and `/tokens` show it either way |
| `updates.check` | `true` | Check GitHub once a day, in the background at startup, for a newer release and show a note in the status line. `groot upgrade --check` checks at any time |
| `memory.path` | `.groot/groot.db` | SQLite database. A relative path is under each space's directory; an absolute path is shared by all spaces |
| `memory.preamble_tokens` | `1500` | Estimated token budget of the memory block, sent in the conversation (not the system prompt) |
| `memory.preamble_summaries` | `3` | Session summaries kept in the memory, newest first |
| `audit.path` | `.groot/audit.jsonl` | Audit log. Same rules as `memory.path` |
| `audit.sinks` | empty | Export sinks: `file`, `http` or `syslog`. See Audit |
| `gate.policy` | `interactive` | `interactive`, `readonly` or `auto` |
| `kubectl.namespaces` | empty (any) | Namespace allowlist |
| `system_prompt` | built in | Replaces the base prompt only: the default groot prompt (identity, working rules, tool rules) when unset. The agent prompt, the rules of the space, the environment block, the People block and `AGENTS.md` are still added after it |

Never put API keys in `groot.yaml`. Keys come from `groot auth login`, from
`ANTHROPIC_API_KEY` in the environment, or from a `.env` file in the working
directory (the environment wins). `.env` is git-ignored.

Three files are loaded, in this order, and each one overrides the one before:

1. `<home>/groot.yaml`, that is `~/.groot/groot.yaml` (or under `GROOT_HOME`), when it exists.
2. `<home>/user.yaml`, which holds only the operator's `name`.
3. The project file: `--config`, default `./groot.yaml`, when it exists. An explicit
   `--config` that does not exist is an error.

Scalars in the project file override the global ones, lists are replaced whole,
and only the keys listed under [Safety model](SAFETY.md#safety-model) may be set in it. Settings made from
inside groot (`/model --default`) are written to the project
file when it exists, and to the global file otherwise. `set_context_window` always writes the global file. `groot doctor` shows which
files were loaded.

### Environment expansion

| File | `${VAR}` | Keys it may set |
|---|---|---|
| `~/.groot/groot.yaml` | expanded at load, in every string, except the `mcp` section | all |
| `~/.groot/user.yaml` | expanded at load | `name` only |
| `./groot.yaml` (project) | never: a `${` there is an error | the allow-list under [Safety model](SAFETY.md#safety-model) |

The `mcp` section of the global file keeps `${VAR}` literal at load. Its values
expand when a server connects, and an unset variable is an error that names it.
A `.env` in the working directory is read into the environment at startup, so its
values count for every layer above; a variable already exported in the shell wins.

## Context

Each model call sends the whole history, so the context of a conversation is
the size of its last model answer: the input tokens it read, plus the cache
write and cache read tokens. Groot stores that size per session
(`sessions.context_tokens`), so a resumed session shows it before its next turn.

- **Where it shows.** The header of the active conversation shows
  `ctx 84k/200k 42%`, or `ctx 42%` when the line is too narrow. The sidebar
  shows `42%` after each agent of the active space, only once a size is known.
- **Colours.** Dim below 70%, the warning colour from 70%, red from 90%.
- **Window.** The window of the model the next call uses: the built-in table
  (200000 tokens for the Claude models), or `models.context_window` in
  `groot.yaml`. A model that is in neither uses 200000.
- **Compaction.** With `compaction.max_context_percent` (default 80), a model call
  first runs the configured strategy once the last context reaches that share of
  the window, even when the message count has not. `summarize` then halves the
  history. With strategy `none` nothing is dropped; groot warns once when the
  context reaches 90% and suggests `window` or `summarize`.
- **Overflow.** If the provider refuses a prompt as too long, groot compacts once
  (the configured strategy, or the last `compaction.keep` messages when the
  strategy is `none`), prints `context overflow: compacted history and retried`
  and retries the call once. A second refusal is shown as an error.

```
/context                          context of each agent in the active space
/context all                      the same for every space
/context window <model> <tokens>  window of a model or prefix, saved in groot.yaml
```

The table has the model, the window, the last context size, the percentage, the
compaction strategy and whether a compaction would run on the next call.

The admin agent reads it with `get_context {space?}` and sets windows with
`set_context_window {model, tokens}`. The window is written to `groot.yaml` under
`models.context_window`; running conversations pick it up on their next call.

## Budgets

A budget caps spend in dollars. Two budgets apply, and both are checked before
each turn:

- **Agent budget**: `budget_usd` in the agent file's frontmatter. It counts that
  agent's spend in each space.
- **Space budget**: `budget_usd` in the space's entry in `spaces.yaml`. It counts
  the spend of all agents in that space.

```yaml
# spaces.yaml, under the space
- name: payments
  budget_usd: 10
```

Zero or no budget means unlimited. Spend is the estimate from the price table
(`internal/provider/pricing.go`); a model without a price adds no spend.

- The spend of each session is stored in the space's `groot.db` after every model
  answer, so a restart or a crash keeps it.
- The sidebar shows the spend after each space and agent, for example `$0.90/$2.00`.
  It turns amber at 80% of the budget and red at the budget.
- When a budget is spent, the turn is refused with an error line and the model is
  not called. The same check runs before each model call inside a turn, so a turn
  stops between tool rounds.
- When the next turn may pass a limit, groot warns once in the transcript and runs
  the turn.

```
/budget                      spend and budgets of the active space and its agents
/budget space <usd>          budget of the active space (0 clears it)
/budget agent <name> <usd>   budget of an agent (0 clears it)
/cost                        session cost, then the spend and budgets above
```

The admin agent sets budgets with `set_space_budget {space, usd}` and
`set_agent_budget {agent, usd}`. Work mode still asks before them. It reads the
spend with `get_cost {space?}`, which is read-only.

Spend is hidden by default: the sidebar shows an agent's spend only next to its
budget (`$0.90/$2.00`), and the status line shows no cost. Set `ui.show_cost:
true` in `groot.yaml` to show the session cost in the status line and the spend
of every agent and space in the sidebar. `/cost` and `/tokens` always show the
cost when asked.

There is no global default yet: a `budget` key in `groot.yaml` is not read.
