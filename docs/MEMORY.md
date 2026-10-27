# Memory

groot keeps long-term memory: short entries that outlive a session and are
shown to the agent in its conversation. This page is the model. The commands
are in the README under "Memory and sessions".

The hygiene model: upsert by topic, dedupe by content, pin, review
dates, and taint.

## Scopes

Every entry has one scope. It decides who can read and who can change it.

| Scope | Lives in | Read by | Written by |
|---|---|---|---|
| `agent` | the space database | only the agent that wrote it | that agent |
| `space` | the space database | every agent of the space | any agent of the space (default) |
| `global` | the root database (the user's home space) | groot always; other agents only when enabled | groot only |

Other agents cannot write `global`. A `remember` with `scope: global` from them
returns a refusal, and so does `update_memory` or `forget` on a global entry.

Migration from the earlier scopes: `project` became `agent`, and `global`
(which meant "everyone in this database") became `space`. The migration runs
once per database. A new `global` entry needs the reader to be enabled.

## Kinds

- `fact`: something true about the environment.
- `decision`: a choice and its reason.
- `runbook`: steps to do something again.
- `session_summary`: written when a session ends. The model cannot save one
  with `remember`.

## Entry fields

`id`, `kind`, `scope`, `agent` (the writer), `title` (optional), `text`,
`topic_key` (optional), `source` (for example `tool:remember` or
`session:<id>`), `pinned`, `tainted`, `review_after`, `revision_count`,
`duplicate_count`, `created_at`, `updated_at`.

## What is captured, and when

- The agent calls `remember` when it decides something is worth keeping.
- Each session ends with a summary: the model summarizes it in a fixed shape
  (`Goal`, `Done`, `Decisions`, `Next`, `Files`), at most about twelve lines.
  The title is `Session <YYYY-MM-DD HH:MM> <agent>`. When there is no model
  call (a fast close, or the model is slow), a local summary with the same
  headings is stored: `Goal` is the last request, `Done` is the last answer, and
  `Next` says `unknown (fast close)`.
- `/clear` ends the session the same way. The new session records the cleared
  one as its parent.

## Upsert and dedupe

- A `remember` with a `topic_key` that already exists revises that entry. The
  result reads `updated #12 (revision 3)`.
- A `remember` whose normalized content matches an existing entry is not stored
  twice. The result reads `duplicate of #12`, and the duplicate count grows.
- Otherwise the result reads `remembered #12 (created)`.

## The preamble: what the model gets

Memory is not part of the system prompt. The system prompt is composed once
when the conversation opens and does not change for the session, so the cached
prefix holds. Memory enters the conversation as a block at the top of a user
turn, built from the same budgeted preamble:

- **The first turn** of a conversation (at open, and again after `/clear`)
  carries the whole preamble in a `<memory>` block with one line saying it is
  the agent's stored memory for this space.
- **Every later turn** carries only what changed since the previous block, in a
  `<memory-update>` block: the entries added or updated since then, with the same
  lines, and the removed ones named by topic key (or `#id [kind]` when they have
  none). An entry that gained or lost its review mark, or was pinned, counts as
  changed. A turn with no change carries no block.

A change made by `remember`, `update_memory`, `forget`, a pin or a review is
therefore in the next turn, not in the turn that made it. Changes are found by
comparing the content of the entries, so a tainted entry, whose envelope differs
on every render, does not look changed.

The budget is the same as before: the selection walks the entries in the order
below and stops at the first one that would exceed `memory.preamble_tokens`.

Selection order: pinned entries first, then decisions and runbooks, then facts,
then the newest session summaries. Only the newest `memory.preamble_summaries`
summaries are considered. Each entry text is cut to 400 runes. The walk stops at
the first entry that would exceed `memory.preamble_tokens`, using an estimate of
one token per four characters. One entry is always kept when any exist.

Each line is `- [kind] text`. Three marks show more:

- a global entry reads `- [kind, global] text`;
- an entry that needs review ends with ` (review)`;
- tainted text is wrapped in the same untrusted-content envelope the injection
  guard uses, so the model reads it as data.

## Review state

Each kind has a review interval. When an entry's `review_after` date passes, it
needs review. It stays usable, but it is marked. `/memory review` lists these,
and `/memory reviewed <id>` moves the date one interval ahead. Pinned entries
are placed first in the preamble, but a tight budget can still leave them out.

## Taint and provenance

An entry saved in a turn that read untrusted tool output is stored tainted.
Untrusted tools are the ones that mark their output as such: `recall`,
`memory_get`, `fetch_url`, `read_inbox`, `bash`, `kubectl`, `read_file`, and
others. The agent loop keeps a per-turn flag (`Agent.TurnTainted`), set when
such a tool runs and cleared at the start of the next turn. The `remember` tool
reads that flag. A tainted entry shows `!` in `/memory` and is wrapped in the
envelope in the prompt.

## Global enablement

Reading global memory is decided per agent:

- groot always reads it. groot's conversations live in the root space, which is
  the same database.
- Any other agent reads it when its definition has `global_memory: true`.
- groot turns it on or off with `grant_global_memory {agent}` and
  `revoke_global_memory {agent}`. The change is saved in the agent file and
  applies to its open conversations at once. The `list_agents` output shows
  `global memory: yes` for enabled agents.
- A space that is not the root space keeps its sessions and its space and agent
  memory in its own database, and reads and writes global memory in the root
  database through a layered store.

## Tools

- `remember {kind, text, title?, topic_key?, scope?: agent|space|global}`: the
  default scope is `space`.
- `recall {query, limit?}`: one line per entry, `#<id> [kind|scope] title — text`,
  the text cut to 160 runes. It ends with a hint to use `memory_get`.
- `memory_get {id}`: the full entry.
- `update_memory {id, title?, text?, topic_key?, pinned?}`: change fields.
- `forget {id}`: soft delete. The entry leaves every read path.
- `grant_global_memory {agent}`, `revoke_global_memory {agent}`: groot only,
  through the admin tools.

## `/memory`

- `/memory`: the entries in the prompt. Columns: `#id`, kind, scope, flags, age,
  title or text. Flags: `★` pinned, `?` needs review, `!` tainted, `G` global.
  A footer counts visible entries, summaries, pinned and entries that need review.
- `/memory <query>`: search, the same lines as the list.
- `/memory show <id>`, `/memory pin <id>`, `/memory unpin <id>`,
  `/memory rm <id>` (forget, no prompt), `/memory reviewed <id>`.
- `/memory review`: the entries whose review date has passed.

An unknown word is a search query. A known verb with bad arguments prints the
usage. Ids must be positive whole numbers.

## Configuration

| Key | Default | Meaning |
|---|---|---|
| `memory.path` | `.groot/groot.db` | the space database |
| `memory.preamble_tokens` | `1500` | estimated token budget of the preamble |
| `memory.preamble_summaries` | `3` | session summaries kept in the preamble |

## Deliberately out

- Conflict judgment between entries (no automatic supersede, no judge model).
- Embeddings and vector recall. Search is full-text (FTS5). Vector recall through
  Ollama is on the roadmap, under "Later".
