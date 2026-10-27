# Sessions and messaging

Session history, background conversations, and messages between agents.

## Sessions and history

Each conversation is one session stored in its space's database (`memory.path`,
by default `<space>/.groot/groot.db`): every message, tool call and the session summary
written when the session ends. Sessions are per agent in a space.

- Selecting an agent resumes its latest session. The session id stays the same,
  so new turns append to it. The model gets the history back as the compaction
  setting allows: `none` keeps all of it, `window` keeps the last `keep`
  messages, and `summarize` keeps the last 200. The cut always falls on a user
  message, so a tool call never loses its result.
- The transcript shows the last 200 stored messages above a dim line:
  `resumed session <id> · N messages · last activity <time>`. User turns are
  echoed with their divider, assistant text is rendered as Markdown, and each
  tool call is one `[tool] name` line. Tool result bodies are not drawn.
- `/clear` ends the session (the summary is written), marks it cleared and
  starts a fresh one. A cleared session is never resumed. Its messages stay in
  the database for audit and recall. The agent's memory is kept too, and the
  "Long-term memory" block of its prompt is refreshed, so the new session starts
  with what was remembered and the summary of the old one.
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
  is shown. An ack never enters a turn, never starts one in work mode, and is not
  counted in the sidebar badges. `/inbox` lists it with an `ack` tag.
- `read_inbox` returns the agent's unread messages and marks them read. Acks are
  not included.
- Unread messages are delivered into the next turn: up to five, oldest first, as
  `[inbox] from <space>/<agent> · <time>` blocks with their subject and body (8 KB
  at most). Delivered messages are marked read. The rest stay unread, and the turn
  is told how many.
- In work mode, a message that arrives while the agent is idle starts a turn that
  reads it (`⟶ triggered by message #id` appears in the transcript). At most
  `work.max_auto_turns` (default 10) such turns run per session; after that the
  message waits in the inbox. Without work mode nothing runs, and the badge shows.
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
