# ODD flow

How groot sizes a task, moves it through phases, and records evidence.

## ODD flow

Every conversation classifies its work before the first write. Medium and large
work then follows a short loop that leaves evidence behind.

```
implement -> verify -> deliver -> done    (a medium task opens in implement)
     |                        |
     | large only             +-- failed verify --> implement
     v
  planned -> implement

any open phase -> failed (with a reason)
```

Sizes come from one test: could another agent resume the work from the request
plus the git diff alone?

- small: trivial, one or two reversible edits you fully understand, nothing to
  run. No task. Writes pass through the gate as before. (Resumable: yes.)
- medium: resumable. A task with command evidence is enough. A task moves through
  implement, verify, deliver and done. (Resumable: yes.)
- large: not resumable. It needs a feature document under `docs/tasks/`, with
  numbered specs. (Resumable: no.)

`task_classify` takes the size and the answer to the test (`resumable_from_diff`).
Large with a yes, or small and medium with a no, is refused.

The feature document of a large task has three sections, in any order:

```
## Specs
S1. "<what the user asked, verbatim>"
## Tasks
T1. (S1) <what you will change>
## Log
L1. <the original request, verbatim>
```

Items are numbered from 1 without gaps. A task names the specs it serves as `(S1)`
or `(S1, S3)`, and every named spec must exist. A line indented by two spaces
continues the item before it. `task_advance` to `planned` with the path reads the
document; a plan that does not parse is refused with this template.

Tools: `task_classify` records the size, `task_open` opens the task and applies the
route, `task_advance` moves it on, `task_verify` records a verdict per spec of a
large task, and `task_status` reports it. In the chat, the header shows
`task #12 medium/implement` while a task is open. `/task` prints the status and
`/task fail <reason>` closes the task as failed.

Evidence:

- verify needs a command run during implement. Every bash run in that phase is
  recorded automatically: the command, the first 300 characters of output and the
  exit status. The model does not write these records.
- A large task verifies each spec with `task_verify` (`pass` or `fail`, with the
  evidence). Deliver is blocked until every spec passes: a pending spec is named,
  and a failed one sends the task back to implement with the reason. Going back to
  implement clears the verdicts; the fail notes stay as evidence.
- deliver needs a note with the changes and the risks.
- going back to implement from a failed verify needs the failure as evidence.

Configuration (`flow` in `groot.yaml`):

- `mode: strict` (default). Writes and commands are denied while a medium or large
  task is not in implement. The reason goes back to the model as the tool result.
  Without a classification, the message asks for `task_classify` first.
- `mode: advisory`. The same rules, but the call runs and a warning is shown.
- `routes.small`, `routes.medium`, `routes.large`: `model` and `agent`. The defaults
  are haiku, sonnet and opus. The model is applied when the task opens and restored
  when it closes. A route's `agent` takes the task over when `delegation` is `auto`
  (the default), with the model not applied to the origin; see
  [Delegation](#delegation). With `hint` it is only named in the result.

Per space: `mode`, the routes and the default model can be overridden in a space's
entry in `spaces.yaml` (see [Spaces and agents](SPACES-AND-AGENTS.md#spaces-and-agents)). Each field is
resolved on its own, in this order: the space, then `groot.yaml`, then the built-in
default. The model of a conversation follows the same idea, but with the agent's
own model first: agent `model`, then the space `model`, then `provider.model`.

The override is read when a conversation is opened: the first time a space and agent
pair is used in a process. Conversations already open keep the controller and model
they started with, so restart groot to apply a change to a pair you have used. The
route model still applies while a task is open and is restored when the task closes.

`/tasks` lists the open tasks of every space: space, agent, task id, size, phase,
evidence count, opened and updated time, the goal (cut at 60 characters), and
`ACTIVE` when the agent's conversation is running a turn in this process.
`/tasks <space>` filters by space. `/tasks all` adds the done and failed tasks of
the last seven days, with the phase they finished in. The sidebar shows `⚑ N`
after a space with N open tasks. The admin agent reads the same table with
`list_tasks {space?, include_closed?}`.

What the rules leave alone: reads, the task tools, `send_message`, and the admin
tools that change the catalog and the spaces (`create_space`, `create_agent`,
`import_agent`, `install_agent` and the assign tools). A classification lasts until
a task opens or until a new user request starts without one.

The flow shapes the agent's work and records what it did. It is not a sandbox.
The permission gate still decides every call that the flow allows.

### Delegation

A route can name an agent (`routes.<size>.agent`). With `delegation: auto`, the
default, a task opened on that route is handed over to the agent through the
inbox. The space's manager gets a tracking copy.

- The origin keeps its task open and marks it as handed over. While the task is
  with the delegate the origin cannot write or run commands (the gate names the
  delegate and the report it waits for) and cannot advance the task: it reads,
  answers, waits for the report, or fails the task with a reason.
- The delegate gets `task:#N handover` with the goal, scope, done-when, plan path,
  the origin address (`<space>/<agent>`) and the instructions. It works the task
  through its own flow, and its task records the origin, so `/tasks` shows the chain
  (`reader → developer`, `developer ← payments/reader #12`).
- When the delegate delivers, its deliver note goes back as `task:#N delivered`.
  The origin records it as evidence and moves to `verify`, where it checks the work
  and delivers to you as usual.
- If the delegate cannot finish, it fails its task and sends `task:#N returned
  <reason>`. The origin records the reason and goes back to `implement`.
- The manager gets `task:#N tracking` when a task is handed over, and a copy of
  each `delivered` and `returned`. It does not implement handed-over tasks.

The subject is the contract: `task:#<id>`, a space, then the signal (`handover`,
`delivered`, `returned` or `tracking`). A leading `Re:` is ignored, and the text
after `returned` is the reason. The body is the deliver note or the reason
of a return that has no reason in the subject.

Configuration, in `flow` of `groot.yaml` or of a space's entry in `spaces.yaml`.
Each field falls back to the global value, then to the built-in default:

- `delegation: auto | hint | off`. `auto` hands over; `hint` only names the agent
  in the result, as before; `off` does neither.
- `manager: <agent>`. The default is `space-manager` when that agent is assigned
  to the space; `none` means no manager. The manager must be assigned to the space.

```yaml
spaces:
  - name: payments
    flow:
      delegation: auto
      manager: lead
```

The admin agent sets both with `set_space_delegation {space, mode?, manager?}`.
`/space routes` shows the values and their origins, and says when the manager lacks
`list_tasks`, which it needs to answer status questions. Groot does not grant it to
agents you own: add it to their `tools:` line.

Limits:

- A handover needs the delegate assigned to the space; otherwise the hint is shown.
- A task is not handed back to the agent it came from. Longer cycles (A to B to C
  to A) are not detected.
- A report is applied when it arrives. An origin whose conversation is not open is
  updated in the store, and it reads the new state when it opens.
- In work mode the delegate's report is sent at deliver, before you accept the
  review of its work.
- The return path depends on the delegate following its rule. A delivery is sent by
  groot when the delegate delivers.
- Delegation runs in the same space only.
