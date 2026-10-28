# Background jobs

A background job is a shell command that keeps running after the tool call
returns. The agent starts it, keeps working, and gets the result back in a later
turn: when the command exits, or for each matching line of a monitored command.
Nothing polls.

## Tools

| Tool | Input | Result |
|---|---|---|
| `bash_background` | `command`, `timeout_seconds` | a job number; the exit comes back as an event |
| `monitor` | `command`, `pattern` (Go regular expression), `timeout_seconds` | a job number; each matching line, and the exit, come back as events |
| `job_list` | none | every job of the conversation: id, command, state, start time and the last five lines |
| `job_output` | `id` | the last 64 KiB the job printed, capped like a bash result (long output is saved under `.groot/outputs`) |
| `job_stop` | `id` | sends SIGTERM to the job's process group, then SIGKILL after 5 seconds |

Jobs run under the same sandbox profile as `bash`: the same working directory,
the same credential paths denied, and network off unless `bash.sandbox.network`
is `true`. The destructive-command guard applies to `bash_background` and
`monitor` too. Starting a job is a risk-`exec` call even when the command only
reads, because the command keeps running after the call returns.

## Events

An event is delivered like an inbox message.

- **Next turn.** Events wait in the conversation that started the job. The next
  user turn carries them in one block, after the inbox notices and before the
  prompt:

  ```
  Background job events
  job #1 exited with code 3 after 2.4s: sh -c 'exit 3'
  job #2: ERROR one
  job #2 reached the 50-line event cap
  ```

  Each event line is redacted and cut at 500 characters. The block is cleared once
  it is sent.
- **Work mode.** An event starts a turn by itself when the conversation is in work
  mode and idle. That turn counts against `work.max_auto_turns` like a turn
  started by a message. At the limit, the turn does not start and groot warns
  once that the events wait for the next turn. Line events do not warn; the exit
  and the cap do.
- **Outside work mode.** Nothing starts. The agent shows a `◆ N` badge with the
  number of waiting events, and the status line shows `jobs: N running` while a
  job runs.
- **Busy conversation.** An event that arrives while a turn is running waits for
  the next turn. In work mode that means it waits for the next user message, not
  for the running turn to end.

## Monitor pattern and cap

- Without a pattern every output line is an event. With a pattern only the
  matching lines are.
- A job sends at most 50 line events. The 51st matching line becomes one cap
  event (`job #N reached the 50-line event cap`), and no line events follow. The
  exit event is always delivered.
- Output is split on newlines. A line longer than 8 KiB without a newline is sent
  as one line.

## Limits

- At most 4 jobs run at once per conversation. A fifth start is refused, and the
  refusal lists the running jobs.
- Each job keeps the last 64 KiB of combined stdout and stderr.
- A job without `timeout_seconds` is stopped at 30 minutes. With it, the value
  is capped at the larger of `bash.max_timeout` and 30 minutes; a larger value is
  clamped and the result says so. Stopping at the limit is the same as `job_stop`.
- Events pass through queues of 256 per conversation. When a queue is full, a
  line event is dropped at once. An exit or cap event waits up to 5 seconds for
  room and is dropped after that; the drop is counted, not shown.

## Operator commands

- `/jobs` lists the jobs of the active conversation: id, state, exit code,
  start time and command. The command is redacted.
- `/jobs stop <id>` stops one job, the same way `job_stop` does.

## Audit records

Jobs write three records to the space audit log, with the agent and session that
own them. They are found with `/audit tool job_start`, `/audit tool job_stop`,
`/audit tool job_exit`, or the `tool` filter of `audit_query`.

| Record | Written when | Fields |
|---|---|---|
| `job_start` | a job starts | `input_preview`: job number, starting tool and command; `sandbox` as `bash` records it |
| `job_stop` | a running job is stopped on request | `input_preview`: job number, `by tool`, `by user` (`/jobs stop`) or `by clear` (`/clear`), and command |
| `job_exit` | a job ends, however it ends | `input_preview`: job number, state (`exited` or `stopped`), exit code, command; `duration_ms`; an error when the code is not 0 or the job was stopped |

The agent's own record of the `job_stop` call is kept as well, so one stop by the
agent writes two `job_stop`-named records: the tool call and the job record.

## What stops a job

- `job_stop`, or `/jobs stop <id>`.
- The timeout.
- `/clear`: the running jobs of the cleared session stop. The conversation keeps
  its job list, so later jobs still start.
- Closing the conversation or groot: `/exit`, `ctrl-d`, `ctrl-c`, and a space or
  agent that is deleted, renamed or moved while it has an open conversation.
  Every job of the conversation is stopped and its exit is recorded before the
  session ends.

## Not covered

- Jobs die with groot. They are not detached: closing groot stops them, and a
  restarted groot does not know about them.
- No stdin for a job, and no terminal. A job that waits for input waits until its
  timeout.
- Events are not kept across restarts. Output stays only in the job's buffer
  until the job is gone.
