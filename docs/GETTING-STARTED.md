# Getting started

What groot shows on first run, how to move around, and how its modes work.

Groot is a terminal agent for developers and platform engineering. It works with
Kubernetes, logs, files and the shell. Every tool call passes a permission gate,
memory lives in SQLite, and the model layer is a port with an Anthropic adapter.

- Binary: `groot`. Module: `github.com/tinchoram/groot-cli`. Go 1.26. Version 0.6.0.
- Terminal UI with a green theme, built on Bubble Tea.

## Getting started

On the first run groot prepares `~/.groot` and opens on two things:

- **root**: a space at your home directory. It is the place for work that is not
  tied to one project.
- **groot**: the administrator agent, assigned to root and selected. It has no
  file, shell or cluster tools. It creates spaces and agents, assigns them, and
  explains how groot works.

A fresh home holds exactly these two. Create more agents with `/agent new` or by
asking groot. groot greets you with a short welcome block that lists the keys.

Try it:

1. Ask groot to create a space: "create a space for ~/src/payments". groot
   confirms the name, path and purpose, then creates it after your approval.
2. The space appears in the SPACES section of the sidebar. Press `tab` to focus
   the sidebar, `s` to jump to SPACES, pick it with `↑`/`↓`, and press `enter`.
   The space switches and its first agent is selected.
3. Add an agent: ask groot for one, or run `/agent new reviewer "reviews diffs"`,
   edit the file it prints, and assign it with `/space assign reviewer`.
   Press `a` in the sidebar to see the agents, select one, and talk to it.

Older homes keep everything they have, including any `default` space and agents
such as `devjunior`. groot only adds root and groot next to them and keeps the
active space as it was. Nothing is ever deleted.

The `groot` catalog entry is managed: its name cannot be used by `/agent new`,
and nothing deletes it. You may edit its prompt (the body of its file).

`debug` is optional. It keeps groot's backlog of bug reports and feature requests,
and it ships in the library: ask groot to install it ("install debug"), or run
`/agent install debug`. Until it is installed, `/bug` and `/feat` say so.

## Layout and keys

The sidebar on the left lists **SPACES** (active marked ▸) and **AGENTS**
(assigned to the active space, selected marked ▸, with a description under each).
The chat on the right shows `space ▸ agent · model`. Narrow terminals (under 90
columns) start with the sidebar hidden.

| Key | Action |
|---|---|
| `tab` | move focus forward: sidebar, input, terminal |
| `shift-tab` | move focus backward, and leave the terminal |
| `↑` / `↓` | move the cursor in the focused section |
| `enter` | select: a space switches space and focuses AGENTS; an agent switches chat and focuses the input |
| `enter` in the input | send the message |
| `shift-enter`, `alt-enter`, `ctrl-j` | insert a newline in the input; the box grows to 8 lines, then scrolls |
| `↑` / `↓` in the input | recall history from the first line (↑) or the last line (↓); elsewhere they move the cursor |
| `home` / `end` (`fn-←` / `fn-→` on a Mac), `ctrl-a` | start or end of the line when the input has text; with an empty input, `home` / `end` scroll the transcript |
| `s` / `a` | jump to SPACES / AGENTS |
| `ctrl-b` | show or hide the sidebar |
| `ctrl-e` | show or hide the chat; the sidebar then shows the space path and each agent's model and tools |

Switching is refused while an answer is in flight or an approval is pending.

## Prompt history

When the input has focus, `↑` recalls the previous prompt or slash command and
`↓` moves forward again. Past the newest entry, `↓` restores the draft you were
typing. `esc` also returns to the draft. Editing a recalled line changes only
the copy in the input, never the stored entry. Blank lines and consecutive
duplicates are not stored, and the newest 1000 entries are kept. The history is
per user, in `~/.groot/history` (mode 0600, one entry per line, with newlines
and backslashes escaped).

## Completion

Type `/` at the start of the input and a list of up to six commands shows above
it, each with its one-line help; the list narrows as you type, and a command
word followed by a space lists its sub-commands. `tab` completes the highlighted
row and adds a trailing space. `↓` and `↑` move the highlight while the list is
open, so they do not recall history then. `esc` closes the list for the current
line without clearing the input; typing again reopens it. `enter` completes
instead of sending when one row remains and the word is not yet a whole command.
The list stays hidden for text that does not start with `/`, during an approval,
while a long paste is collapsed, and when the sidebar or the terminal has focus.

Type `@` in a message that does not start with `/` and the list shows the agents
you can mention (see [Identity and mentions](SPACES-AND-AGENTS.md#identity-and-mentions)).

## Mouse, copy and paste

groot captures the mouse so that clicks and drags can work on the columns.
Native terminal selection is therefore replaced by selection inside groot.

- **Select**: drag with the left button over the chat or the terminal. The text
  is copied to the clipboard on release, and a note shows how many characters
  were copied. Double-click selects a word, triple-click a line. A click or any
  key clears the selection.
- **Clipboard**: groot writes OSC 52, which iTerm2, kitty, WezTerm, Ghostty and
  Windows Terminal accept, and also runs `pbcopy` (macOS) or `wl-copy` /
  `xclip` (Linux). Helper errors are ignored.
- **Paste**: in the chat input, a short paste (up to three lines and 200
  characters) is inserted as it is, with newlines turned into spaces. A longer
  paste shows as `[Pasted text #N +L lines]` in tan. The full text is kept and put
  back when you send, so the model gets all of it, and the transcript shows the
  placeholder with the first line of the paste in dim. Backspace right after a
  placeholder removes the whole paste. `/bug` and `/feat` capture the placeholder,
  not the paste. In the terminal, pasted text goes to the shell, wrapped in
  bracketed paste markers when the program asked for them.
- **Native selection**: hold `option` (iTerm2 and Terminal.app) or `alt` to make
  the terminal select text itself. That bypasses groot's mouse capture.
- **Click to focus**: a click focuses the column under the pointer. In the
  sidebar, a click also selects that space or agent.
- **Resize columns**: drag the divider between the sidebar and the chat, or
  between the chat and the terminal. The divider turns green while you hover or
  drag it. Columns keep a minimum width of 20 cells; the chat keeps at least 40
  while the other columns are open.
- **Mouse in programs**: when a program in the terminal asks for mouse events
  (vim, htop and similar), clicks and wheel events go to that program instead of
  selecting text.
- **Layout**: the columns are saved to `<GROOT_HOME>/layout.yaml` (sidebar and
  terminal widths and visibility) and restored on the next start.

## Terminal column

`ctrl-t` opens a real shell on the right of the screen, in the active space's
directory. It runs `$SHELL` (or `/bin/sh`) on a pseudo-terminal, and the screen
is drawn with a VT emulator, so colours, cursor movement and full-screen tools
work as they do in a normal terminal.

This is your own shell, not a groot tool: the [sandbox](SAFETY.md#sandbox) does not apply
to it, and nothing it runs is audited.

| Key | Action |
|---|---|
| `ctrl-t` | show or hide the terminal; the first press starts the shell |
| `tab` | move focus forward: sidebar, input, terminal. Inside the terminal, `tab` goes to the shell (completion works) |
| `shift-tab` | move focus backward; from the terminal, this is how you leave it |

- The column takes about 45% of the width. The sidebar keeps its size, and the
  chat shrinks. The column shows only beside the chat (`ctrl-e` hides both).
- While the terminal has focus, every key goes to the shell, including `ctrl-c`
  (it interrupts the program, it does not quit groot) and `ctrl-d`. `ctrl-b` and
  `ctrl-e` stay groot's. Use `shift-tab` to leave the terminal.
- The shell starts lazily, in the directory of the space that was active at the
  first `ctrl-t`. It keeps running when you hide the column or change space;
  it does not follow later space changes.
- Typing `exit` ends the shell and closes the column. The next `ctrl-t` starts a
  new one.

Limitations: no scrollback beyond the visible screen, and on narrow windows the
chat hint line is cut short. Mouse input reaches a program that asks for it (see
below); otherwise the mouse selects text.

## Work mode

`/work on` lets the agents of the active space work without asking for each step,
and `/work off` takes that back. It applies to every agent assigned to the space:
conversations already open switch at once, and conversations opened later in the
space inherit the setting. Groot can do the same with `set_work_mode`, for one
agent or, with `space_wide: true`, for the whole space. While it is on:

Work mode decides by tool:

| Runs without asking | Still asks |
| --- | --- |
| `write_file` inside the space | `write_file` outside the space |
| `bash`, unless it matches `work.always_ask` | `bash` matching `work.always_ask` (defaults: `rm -rf` outside the space, `sudo`, `git push --force`, `git reset --hard`, `git clean`, `kubectl apply/delete/patch/edit/scale/rollout`, `docker rm/rmi/system prune`, `terraform apply/destroy`, `curl`/`wget` writing a file) |
| `send_message`, `read_inbox`, `remember`, `recall` | `create_space`, `assign_agent`, `unassign_agent`, `delete_space`, `edit_space`, `delete_agent`, `edit_agent`, `move_agent`, `copy_agent` |
| `feedback_report`, `feedback_update`, `feedback_list` | `create_agent`, `import_agent`, `install_agent`, `upgrade_agent`, `set_agent_model`, `set_space_route`, `set_space_model`, `set_space_delegation`, `set_space_budget`, `set_agent_budget`, `set_context_window`, `set_work_mode` |
| `get_context`, `get_cost`, `list_tasks`, `read_changelog`, `current_time` (read-only, not asked) | none; they answer as text |
| MCP tools listed in their server's `read_only` | every other MCP tool, read or write, since groot cannot tell what it does |
| `task_classify`, `task_open`, `task_advance`, `task_verify`, `task_status` | `set_work_mode` |

Auto-approved calls are shown as `[auto] <tool> <preview>` in the transcript.
- the ODD flow still applies: a medium or large task still needs its steps.
- `work.max_turns` (default 50) limits the calls of one task, and `work.max_usd`
  (default 5.0) limits the session spend. At either limit work mode turns off and
  a warning appears that ends with `/work on turns it back on`.
- when the task reaches deliver, work mode turns off for that agent only and a
  review appears: the changes (`git diff --stat`, or the files written during the
  task), the evidence, and the agent's deliver note. `/task accept` closes the task,
  and `/task reject <reason>` sends it back to implement with the reason. The
  delivery review lists the test runs of the cycle with their exit codes. The
  space setting stays, so the next conversation opened in the space is still on.
- a message between agents starts a turn in the recipient even outside work mode.
  Those automatic turns count against `work.max_auto_turns` (default 50) per session.
  See [Sessions](SESSIONS.md).

Approvals are one line in the box above the input, for example
`allow write_file: notes/a.md (+3 −1)? (y/n/a)`. Press `d` or type `/diff` to see the
whole diff. Press `a` to approve this tool for the rest of the session in this
conversation. A session grant covers the same calls work mode would approve: writes
inside the space and bash outside `work.always_ask`. Admin tools never get `a`.
`/allow` lists the grants of the conversation and `/allow clear` drops them. Calls
covered by a grant print as `[auto]`. The sidebar marks agents in work mode with `⚙`, and a waiting approval
with a tan `?`.

## Keys and exit

- `ctrl-c` ends the session at once: turns are cancelled, pending approvals are
  denied, each conversation keeps a local summary (goal = the last request, done = the start
  of the last answer) with no model call, and the shell is hung up.
- `ctrl-d` and `/exit` write model summaries for the conversations that ran, in
  parallel, within fifteen seconds. The status line shows `closing… (esc to skip
  summaries)`. Esc, or the end of the fifteen seconds, keeps the local summary.
- When the model summary of a `/clear` fails or times out, the session keeps a
  local summary and the note says so (`conversation cleared · summary for
  groot: the model did not answer in time; a local summary was kept`). It is
  not an error: the session is ended and remembered either way.
- Summaries are stored as session summaries in each space's database.
