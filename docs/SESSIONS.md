# Sessions and messaging

Session history, background conversations, and messages between agents.

## Sessions and history

Each conversation is one session stored in its space's database (`memory.path`,
by default `<space>/.groot/groot.db`): every message, tool call and the session summary
written when the session ends. Sessions are per agent in a space.

- Selecting an agent resumes its latest session. The session id stays the same,
  so new turns append to it. The model gets the history back as the compaction
  setting allows: `none` keeps all of it, `window` keeps the last `keep`
  messages, and `summarize` (the default) keeps the last 200 stored messages. The
  cut always falls on a user message, so a tool call never loses its result.
- The older messages that the cut leaves out are summarized, once, on the first
  turn after the resume. The summary uses the compaction rules (user requests
  kept near-verbatim, the open task, decisions, resource names, commands and
  outcomes, open problems) and one model call, bounded by a 30-second timeout.
  It sits in front of the kept messages as `Summary of the N earlier messages of
  this session`. When the call fails or answers nothing, the model gets the note
  `N earlier messages are not in context.` instead, and a warning is shown. A
  long resume therefore runs the summary and then the usual compaction, which
  is the first model call of that turn. `window` drops messages by design and
  gets no summary; `none` keeps them all.
- The transcript shows the last 200 stored messages above a dim line:
  `resumed session <id> · N messages · last activity <time>`. User turns are
  echoed with their divider, assistant text is rendered as Markdown, and each
  tool call is one `[tool] name` line. Tool result bodies are not drawn.
- `/clear` ends the session (the summary is written), marks it cleared and
  starts a fresh one. A cleared session is never resumed. Its messages stay in
  the database for audit and recall. The agent's memory is kept too. The new
  session starts with the whole memory block on its first turn, which holds what
  was remembered and the summary of the old session.
- Databases from earlier versions are upgraded on open, with a `cleared_at`
  column added to `sessions`. Nothing is deleted.

## Background conversations

Every agent keeps its own conversation. Switching agents or spaces never stops
a running turn: it keeps running, and its output is kept for when you return.

- A turn that waits for an approval blocks until you open its conversation and
  answer. Nothing is approved on your behalf. Quitting denies every pending
  approval and cancels the running turns.
- The sidebar shows a spinner after an agent that works, and a tan `?` after one
  that waits for an approval. A space shows a green dot while its agents work
  (with a count when there are several), and a tan `?` when one of them waits.
- Sending to a conversation that is still working is refused with a note; sending
  to an idle one starts its turn even while others run.

## Messaging

Agents and groot can send each other messages. Messages live in one SQLite
database at `<GROOT_HOME>/inbox.db` (default `~/.groot/inbox.db`). Bodies are
capped at 16 KB and subjects at 200 characters.

- `send_message {to, subject, body}` delivers a message. `to` is `groot` (the
  groot agent in the root space), `<agent>` (an agent in the sender's space), or
  `<space>/<agent>`. The recipient must be assigned to that space; otherwise the
  tool fails with `unknown recipient; use <space>/<agent>`.
- `send_message {to, subject, body, ack}` with `ack: true` sends an
  acknowledgement: a short confirmation that needs no answer, such as
  `FEAT-016 recorded`. The body is limited to 200 characters. The recipient sees
  it once as a one-line dim note (`ack from <agent>: ...`) when its conversation
  is shown. An ack never enters a turn, never starts one, and is not counted in the
  sidebar badges. `/inbox` lists it with an `ack` tag.
- `read_inbox` returns the agent's unread messages and marks them read. Acks are
  not included.
- Unread messages are delivered into the next turn: up to five, oldest first, as
  `[inbox] from <space>/<agent> · <time>` blocks with their subject and body (8 KB
  at most). Delivered messages are marked read. The rest stay unread, and the turn
  is told how many.
- A message that arrives while the agent is idle starts a turn that reads it, in
  work mode or not, so you do not need `/readinbox` for it. The transcript shows
  `⟶ triggered by message #id`. Each such turn counts against
  `work.max_auto_turns` (default 50) per session. At the limit the message waits in
  the inbox, and one warning says so. `/clear` starts a new session and resets the
  count.
- A message that arrives while the agent is busy waits for the running turn. When
  that turn ends, one follow-up turn reads what is still unread, so a reply that
  lands mid-turn is not stuck. The sidebar badge shows the waiting messages in the
  meantime.
- The sidebar shows `✉ N` after each agent with unread messages, and after a
  space the total unread of its agents. The space mark sits after the `●` and `?`
  marks.
- `/inbox` lists the active agent's last 10 messages, read or not.
- `/readinbox` starts a turn in which the active agent reads its unread messages
  and says which ones need an answer from you, without acting on them yet. With
  no unread messages it spends no turn.
- `/inbox send <to> <subject> -- <body>` sends a message by hand. It is not gated,
  because you wrote it.

Every `send_message` call is in the audit log of the sending space. Messages do
not start turns by themselves. The task messages of delegation (`task:#<id>
handover`, `delivered`, `returned`, `tracking`) follow a subject contract; see
[Delegation](FLOW.md#delegation).

Agents created before this version keep their tool list. To give one messaging,
add `send_message` and `read_inbox` to its `tools:` line in `agents/<name>.md`.
The `@tinchoram` library agents already include both tools.
