# Background jobs: bash_background, monitor, job_list, job_stop

Backlog item 5, size M. Opened 2026-10-27. The agent can start a command, keep
working, and be woken when it exits or when it prints a matching line, without
polling. Jobs run under the same sandbox profile as `bash`.

## Specs

S1. "`bash_background` returns a job id and wakes the agent once when the
  command exits; `monitor` turns each output line into an event. Plus
  `job_list` and `job_stop`."
S2. "Jobs run under the same profile as `bash`. Network stays off unless
  `bash.sandbox.network` allows it."
S3. "Start is Risk exec. Events are not tool calls, so they carry no gate, but
  each event turn is a normal turn under budgets and `max_turns`."
S4. "Jobs die with the process in this version; detachable terminals stay in
  Later."
S5. "Watch logs and rollouts without polling."
S6. A job keeps the last 64 KiB of combined output in a ring buffer. `job_list`
  shows id, command, state (running, exited with code, stopped), start time and
  the last five lines; `job_output <id>` returns the buffered output, capped like
  a bash result.
S7. An event is delivered like an inbox message: it is injected into the next
  turn of the conversation that started the job. In work mode an event starts
  that turn by itself and counts against `work.max_auto_turns`; outside work
  mode the UI shows that events are waiting and the next user turn carries them.
S8. `monitor` takes an optional regular expression; only matching lines become
  events. A job emits at most 50 line events, then stops emitting and reports
  that the cap was reached. The exit event is always delivered.
S9. At most 4 jobs run at once per conversation; `bash_background` refuses a
  fifth with the list of running jobs. A job has the same `timeout_seconds`
  ceiling as `bash`, default 30 minutes, and is stopped at the ceiling.
S10. Start, stop and exit are audit records of the space, with the job id and
  the command. The exit record carries the exit status and the duration.
S11. `bash_background` is classified like `bash`: the destructive-command guard
  applies, and a read-only command is still Risk exec because it keeps running
  after the call returns.
S12. `job_stop` sends SIGTERM, then SIGKILL after 5 seconds. Closing the
  conversation or exiting groot stops every job the same way.

## Tasks

T1. (S1, S6, S8, S9, S12) New package `internal/jobs`: `Manager` with `Start`,
  `Stop`, `List`, `Output`; one goroutine per job that pumps stdout and stderr
  into the ring buffer and emits line and exit events on a channel; the regular
  expression filter and the 50-event cap; the concurrency limit; the SIGTERM
  then SIGKILL stop; a `StopAll` for shutdown. Tests with real processes
  (`sh -c`), including the cap, the limit, the stop sequence and the ceiling.
T2. (S2, S3, S11) Tools in `internal/tool`: `bash_background`, `monitor`,
  `job_list`, `job_output`, `job_stop`. The start tools build the same sandbox
  `Spec` as `Bash` (dir, env, network, extra writable) and wrap through the
  same `Runner`; `bash_background` implements `tool.Destructive`; `RiskFor`
  returns exec for start, monitor and stop, read for list and output. Tests
  for the input decoding, the risk table and the destructive pass-through.
T3. (S7, S3) Delivery in `internal/workspace`: a per-conversation event queue;
  `injectJobs` adds pending events to the prompt next to the inbox block; in
  work mode an event starts a turn through the same path as `afterDelivery`
  with the auto-turn limit; the conversation key and the job id travel with the
  event; shutdown calls `StopAll`. Tests with the real agent loop: an exit event
  reaches the next prompt, and work mode starts the turn.
T4. (S7) UI in `internal/ui`: the sidebar and status line show how many job
  events wait for a conversation, and `/jobs` lists the jobs of the active
  conversation. Tests for the indicator and the command.
T5. (S10) Audit records `job_start`, `job_stop` and `job_exit` in
  `internal/audit` and the agent wiring; `/audit` filters work on them. Tests.
T6. (S4, S5) Docs: `docs/JOBS.md` with the tools, the event rules and the
  limits; a bullet in the README "What it does"; a line in `docs/SAFETY.md`
  under the sandbox section; `CHANGELOG.md` Unreleased; `docs/ROADMAP.md` row 5.

## Log

L1. Validated backlog item 5 of 2026-10-26: "`bash_background` and `monitor`.
  Highest SRE value: watch logs and rollouts without polling." Scope fixed in
  the integration plan: two tools that wake the agent, plus `job_list` and
  `job_stop`, under the bash sandbox, with events as normal turns under budgets.
L2. 2026-10-27: feature document written. S6 to S12 are the decisions the plan
  left open: buffer size, delivery semantics, the monitor filter and event cap,
  the concurrency limit, audit records, classification and the stop sequence.
L3. implemented 2026-10-27.
